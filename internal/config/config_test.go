package config

import (
	"strings"
	"testing"
)

func setRequiredEnv(t *testing.T, environment string) {
	t.Helper()
	t.Setenv("ENVIRONMENT", environment)
	database := "medha_dev"
	if environment == "production" {
		database = "medha_prod"
		t.Setenv("CREDENTIALS_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	}
	t.Setenv("DATABASE_URL", "postgres://user:test@postgres:5432/"+database+"?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://redis:6379/0")
	t.Setenv("S3_ENDPOINT", "seaweedfs:8333")
	t.Setenv("S3_PUBLIC_ENDPOINT", "https://s3.example.test")
	t.Setenv("S3_ACCESS_KEY", "test-access-key")
	t.Setenv("S3_SECRET_KEY", "test-secret-key")
	t.Setenv("JWT_PRIVATE_KEY_PATH", "/tmp/private.pem")
	t.Setenv("JWT_PUBLIC_KEY_PATH", "/tmp/public.pem")
	t.Setenv("ADMIN_PASSWORD", "test-admin-password")
	t.Setenv("ADMIN_API_TOKEN", "test-admin-token")
	t.Setenv("SHARE_BASE_URL", "https://share.example.test")
}

func TestLoadRequiresExplicitEnvironment(t *testing.T) {
	t.Setenv("ENVIRONMENT", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ENVIRONMENT must be explicitly set") {
		t.Fatalf("Load() error = %v, want explicit environment failure", err)
	}
}

func TestLoadMigrationRequiresOnlyDatabaseURL(t *testing.T) {
	t.Setenv("ENVIRONMENT", "")
	t.Setenv("DATABASE_URL", "postgres://user:test@postgres:5432/medha_dev?sslmode=disable")

	cfg, err := LoadMigration()
	if err != nil {
		t.Fatalf("LoadMigration() error = %v", err)
	}
	if cfg.DatabaseURL == "" {
		t.Fatal("LoadMigration() returned an empty database URL")
	}
}

func TestLoadMigrationRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := LoadMigration(); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("LoadMigration() error = %v, want missing DATABASE_URL failure", err)
	}
}

func TestLoadRequiresCoreConfiguration(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "required environment variables are missing") {
		t.Fatalf("Load() error = %v, want missing configuration failure", err)
	}
}

func TestLoadAcceptsCompleteDevelopmentConfiguration(t *testing.T) {
	setRequiredEnv(t, "development")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.IsDevelopment() {
		t.Fatal("expected development configuration")
	}
	if cfg.CredentialsEncryptionKey != "" {
		t.Fatal("development must not receive an implicit encryption key")
	}
}

func TestLoadRequiresProductionEncryptionKey(t *testing.T) {
	setRequiredEnv(t, "production")
	t.Setenv("CREDENTIALS_ENCRYPTION_KEY", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "CREDENTIALS_ENCRYPTION_KEY") {
		t.Fatalf("Load() error = %v, want encryption key failure", err)
	}
}

func TestCheckEnvInfraConsistency(t *testing.T) {
	tests := []struct {
		name    string
		isProd  bool
		targets []string
		wantErr bool
	}{
		{"dev on local", false, []string{"postgres://u:p@localhost:5432/medha_dev", "redis://localhost:6379/0", "localhost:8333"}, false},
		{"dev on dev services", false, []string{"postgres://u:p@postgres-dev-service:5432/medha_dev", "redis://redis-dev-service:6379", "seaweedfs-dev-service:8333"}, false},
		{"prod on prod services", true, []string{"postgres://u:p@postgres-prod-service:5432/medha_prod", "redis://redis-prod-service:6379", "seaweedfs-prod-service:8333"}, false},
		{"dev pointed at prod DB name", false, []string{"postgres://u:p@postgres:5432/medha_prod", "redis://redis:6379", "seaweedfs:8333"}, true},
		{"prod pointed at dev DB name", true, []string{"postgres://u:p@postgres:5432/medha_dev", "redis://redis:6379", "seaweedfs:8333"}, true},
		{"prod pointed at localhost", true, []string{"postgres://u:p@localhost:5432/medha_prod", "redis://redis:6379", "seaweedfs:8333"}, true},
		{"empty targets skipped", true, []string{"", "", ""}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkEnvInfraConsistency(tc.isProd, tc.targets...)
			if (err != nil) != tc.wantErr {
				t.Fatalf("checkEnvInfraConsistency() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestInfraConsistencyErrorRedactsCredentials(t *testing.T) {
	const credential = "never-include-this-credential"
	err := checkEnvInfraConsistency(true,
		"postgres://user:"+credential+"@localhost:5432/medha_prod",
		"redis://redis:6379/0",
		"seaweedfs:8333",
	)
	if err == nil {
		t.Fatal("expected environment mismatch")
	}
	if strings.Contains(err.Error(), credential) {
		t.Fatal("configuration error disclosed a credential")
	}
}
