package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Env       string
	Host      string
	User      string
	Service   string
	EnvFile   string
	DryRun    bool
}

func main() {
	envFlag := flag.String("env", "dev", "Target environment: dev | main | prod")
	hostFlag := flag.String("host", "", "Override default SSH host alias")
	userFlag := flag.String("user", "", "Override default SSH username")
	dryRunFlag := flag.Bool("dry-run", false, "Print commands without executing them")
	flag.Parse()

	env := strings.ToLower(*envFlag)
	if env != "dev" && env != "main" && env != "prod" {
		fmt.Printf("ERROR: Invalid environment '%s'. Choose dev, main, or prod.\n", env)
		os.Exit(1)
	}

	cfg := Config{
		Env:    env,
		DryRun: *dryRunFlag,
	}

	// Map environments to service names and targets matching GHA setup
	switch env {
	case "dev":
		cfg.Service = "medha-api-dev"
		cfg.EnvFile = ".env.dev"
		cfg.Host = "medha-server"
		cfg.User = "medha"
	case "main":
		cfg.Service = "medha-api-main"
		cfg.EnvFile = ".env.main"
		cfg.Host = "medha-server"
		cfg.User = "medha"
	case "prod":
		cfg.Service = "medha-api-prod"
		cfg.EnvFile = ".env.prod"
		cfg.Host = "medha-production"
		cfg.User = "medha-admin"
	}

	// Flag overrides
	if *hostFlag != "" {
		cfg.Host = *hostFlag
	}
	if *userFlag != "" {
		cfg.User = *userFlag
	}

	fmt.Println("==================================================")
	fmt.Printf("  Medha Deployer Tool (Singular Flow Local CD)\n")
	fmt.Printf("  Target Env   : %s\n", cfg.Env)
	fmt.Printf("  Target Service: %s\n", cfg.Service)
	fmt.Printf("  Target Server : %s@%s\n", cfg.User, cfg.Host)
	fmt.Printf("  Config File   : %s\n", cfg.EnvFile)
	if cfg.DryRun {
		fmt.Println("  ⚠️ DRY RUN MODE ENABLED")
	}
	versionStr, err := incrementVersion(cfg.Env, cfg.DryRun)
	if err != nil {
		fmt.Printf("ERROR incrementing version: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  Build Version : %s\n", versionStr)
	fmt.Println("==================================================")

	// 1. Compile binaries locally
	fmt.Println("==> 1/5 Compiling Go application and tools...")
	if err := compileBinaries(cfg.DryRun, versionStr); err != nil {
		fmt.Printf("ERROR during compilation: %v\n", err)
		os.Exit(1)
	}

	// 2. Package assets
	fmt.Println("==> 2/5 Packaging assets into deploy archive...")
	archivePath := fmt.Sprintf("dist/deploy-%s.tar.gz", cfg.Service)
	if err := packageAssets(archivePath, cfg.DryRun); err != nil {
		fmt.Printf("ERROR during packaging: %v\n", err)
		os.Exit(1)
	}

	// 3. Upload archive
	fmt.Println("==> 3/5 Uploading archive to remote server...")
	remoteArchivePath := fmt.Sprintf("/tmp/deploy-%s.tar.gz", cfg.Service)
	if err := uploadArchive(archivePath, remoteArchivePath, cfg.Host, cfg.User, cfg.DryRun); err != nil {
		fmt.Printf("ERROR during upload: %v\n", err)
		os.Exit(1)
	}

	// 4. Remote compilation build and deploy execution
	fmt.Println("==> 4/5 Triggering remote Docker build and update...")
	if err := executeRemoteDeploy(cfg, remoteArchivePath); err != nil {
		fmt.Printf("ERROR during remote deployment: %v\n", err)
		os.Exit(1)
	}

	// 5. Cleanup local build artifact
	fmt.Println("==> 5/5 Cleaning up temporary local archives...")
	if !cfg.DryRun {
		_ = os.Remove(archivePath)
	}

	fmt.Println("\n==================================================")
	fmt.Println("  ✅ DEPLOYMENT INITIATION COMPLETED SUCCESSFULLY")
	fmt.Println("==================================================")
}

func runCmd(name string, args []string, dryRun bool) error {
	cmdStr := fmt.Sprintf("%s %s", name, strings.Join(args, " "))
	if dryRun {
		fmt.Printf("[Dry-Run] %s\n", cmdStr)
		return nil
	}

	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func compileBinaries(dryRun bool, versionStr string) error {
	absDist, err := filepath.Abs("dist")
	if err != nil {
		return err
	}

	// Clean up dist
	if !dryRun {
		_ = os.RemoveAll("dist")
		if err := os.MkdirAll("dist", 0755); err != nil {
			return err
		}
	}

	// Compile Go API binary for Linux AMD64
	fmt.Println("    Building medha-api binary...")
	ldFlags := fmt.Sprintf("-s -w -X 'github.com/medha/backend/internal/config.Version=%s'", versionStr)
	apiArgs := []string{"build", "-ldflags=" + ldFlags, "-o", "dist/medha-api", "./cmd/medha-api"}
	cmd := exec.Command("go", apiArgs...)
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	if dryRun {
		fmt.Printf("[Dry-Run] GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags=\"%s\" -o dist/medha-api ./cmd/medha-api\n", ldFlags)
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("compile medha-api: %w", err)
		}
	}

	// Compile/Download goose utility for migrations via go install without GOBIN
	fmt.Println("    Installing goose migration utility...")
	if dryRun {
		fmt.Printf("[Dry-Run] GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go install github.com/pressly/goose/v3/cmd/goose@latest\n")
	} else {
		// Determine GOPATH
		gopathCmd := exec.Command("go", "env", "GOPATH")
		gopathBytes, err := gopathCmd.Output()
		if err != nil {
			return fmt.Errorf("get GOPATH: %w", err)
		}
		gopath := strings.TrimSpace(string(gopathBytes))
		if gopath == "" {
			home, _ := os.UserHomeDir()
			gopath = filepath.Join(home, "go")
		}

		compileGoose := exec.Command("go", "install", "github.com/pressly/goose/v3/cmd/goose@latest")
		compileGoose.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
		// Clear GOBIN to allow cross-compiling
		for i, envVar := range compileGoose.Env {
			if strings.HasPrefix(envVar, "GOBIN=") {
				compileGoose.Env[i] = "GOBIN="
			}
		}
		compileGoose.Stdout = os.Stdout
		compileGoose.Stderr = os.Stderr
		if err := compileGoose.Run(); err != nil {
			return fmt.Errorf("compile goose: %w", err)
		}

		// Locate and copy compiled binary to dist/goose
		goosePaths := []string{
			filepath.Join(gopath, "bin", "linux_amd64", "goose"),
			filepath.Join(gopath, "bin", "goose"),
		}
		copied := false
		for _, goosePath := range goosePaths {
			if _, err := os.Stat(goosePath); err == nil {
				if err := copyFile(goosePath, filepath.Join(absDist, "goose")); err == nil {
					copied = true
					break
				}
			}
		}
		if !copied {
			return fmt.Errorf("could not find compiled goose binary in GOPATH/bin paths")
		}
	}

	return nil
}

func packageAssets(archivePath string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[Dry-Run] Creating compressed tar archive at %s containing dist/, migrations/, build/Dockerfile, scripts/cd-deploy.sh\n", archivePath)
		return nil
	}

	file, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gw := gzip.NewWriter(file)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	targets := []string{"dist", "migrations", "build/Dockerfile", "scripts/cd-deploy.sh"}
	for _, target := range targets {
		err := filepath.Walk(target, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			header, err := tar.FileInfoHeader(info, info.Name())
			if err != nil {
				return err
			}

			// Maintain relative path in the tar file
			relPath, err := filepath.Rel(".", path)
			if err != nil {
				return err
			}
			header.Name = filepath.ToSlash(relPath)

			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			if info.Mode().IsDir() {
				return nil
			}

			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()

			_, err = io.Copy(tw, f)
			return err
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func uploadArchive(localPath, remotePath, host, user string, dryRun bool) error {
	scpArgs := []string{"-o", "StrictHostKeyChecking=no", localPath, fmt.Sprintf("%s@%s:%s", user, host, remotePath)}
	return runCmd("scp", scpArgs, dryRun)
}

func executeRemoteDeploy(cfg Config, remoteArchivePath string) error {
	buildDir := fmt.Sprintf("/tmp/medha-build-%s", cfg.Service)

	// Commands to run sequentially on the server:
	// 1. Create a clean build directory
	// 2. Unpack the tarball into the build directory
	// 3. Build target deploy tag: medha-api-{env}:local
	// 4. Run compose deployment script using the build image locally
	// 5. Clean up temporary files
	remoteCmd := fmt.Sprintf(
		"mkdir -p %[1]s && "+
			"tar -xzf %[2]s -C %[1]s && "+
			"docker build --target deploy -t %[3]s:local -f %[1]s/build/Dockerfile %[1]s && "+
			"chmod +x %[1]s/scripts/cd-deploy.sh && "+
			"%[1]s/scripts/cd-deploy.sh --skip-pull --image %[3]s:local --service %[3]s --env-file .env.%[4]s && "+
			"rm -rf %[1]s %[2]s",
		buildDir, remoteArchivePath, cfg.Service, cfg.Env,
	)

	sshArgs := []string{
		"-o", "StrictHostKeyChecking=no",
		fmt.Sprintf("%s@%s", cfg.User, cfg.Host),
		remoteCmd,
	}

	start := time.Now()
	err := runCmd("ssh", sshArgs, cfg.DryRun)
	if err == nil {
		fmt.Printf("Remote deployment finished in %v\n", time.Since(start))
	}
	return err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Sync()
}

func incrementVersion(env string, dryRun bool) (string, error) {
	versionFile := "version.json"
	data, err := os.ReadFile(versionFile)
	if err != nil {
		return fmt.Sprintf("%s-v1.0.1", env), nil
	}

	var versions map[string]int
	if err := json.Unmarshal(data, &versions); err != nil {
		return "", err
	}

	buildNum := versions[env] + 1
	if !dryRun {
		versions[env] = buildNum
		newData, err := json.MarshalIndent(versions, "", "  ")
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(versionFile, newData, 0644); err != nil {
			return "", err
		}
	}

	return fmt.Sprintf("%s-v1.0.%d", env, buildNum), nil
}


