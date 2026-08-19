package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/medha/backend/pkg/image/svg"
)

func main() {
	inputPath := flag.String("input", "", "Input SVG file or directory")
	outputPath := flag.String("output", "", "Output directory (defaults to input directory if not specified)")
	inplace := flag.Bool("inplace", false, "Overwrite input files")
	flag.Parse()

	if *inputPath == "" {
		fmt.Println("Usage: inline_svg -input <file|dir> [-output <dir>] [-inplace]")
		os.Exit(1)
	}

	inliner := svg.NewInliner()

	info, err := os.Stat(*inputPath)
	if err != nil {
		log.Fatalf("failed to stat input path: %v", err)
	}

	if info.IsDir() {
		processDir(*inputPath, *outputPath, *inplace, inliner)
	} else {
		processFile(*inputPath, *outputPath, *inplace, inliner)
	}
}

func processDir(inputDir, outputDir string, inplace bool, inliner *svg.Inliner) {
	files, err := os.ReadDir(inputDir)
	if err != nil {
		log.Fatalf("failed to read directory: %v", err)
	}

	if !inplace && outputDir != "" {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			log.Fatalf("failed to create output directory: %v", err)
		}
	}

	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(strings.ToLower(f.Name()), ".svg") {
			inputFilePath := filepath.Join(inputDir, f.Name())
			processFile(inputFilePath, outputDir, inplace, inliner)
		}
	}
}

func processFile(inputPath, outputDir string, inplace bool, inliner *svg.Inliner) {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		log.Printf("failed to read file %s: %v", inputPath, err)
		return
	}

	inlined := inliner.InlineStyles(string(data))

	var targetPath string
	if inplace {
		targetPath = inputPath
	} else if outputDir != "" {
		targetPath = filepath.Join(outputDir, filepath.Base(inputPath))
	} else {
		// Output to same directory with _inlined suffix if no outputDir and no inplace
		ext := filepath.Ext(inputPath)
		targetPath = strings.TrimSuffix(inputPath, ext) + "_inlined" + ext
	}

	if err := os.WriteFile(targetPath, []byte(inlined), 0644); err != nil {
		log.Printf("failed to write file %s: %v", targetPath, err)
		return
	}

	fmt.Printf("Processed: %s -> %s\n", inputPath, targetPath)
}
