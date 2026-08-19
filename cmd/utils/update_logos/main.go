package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func main() {
	_ = godotenv.Load("../../.env")
	_ = godotenv.Load(".env")

	endpoint := os.Getenv("S3_ENDPOINT")
	accessKey := os.Getenv("S3_ACCESS_KEY")
	secretKey := os.Getenv("S3_SECRET_KEY")
	dbURL := os.Getenv("DATABASE_URL")
	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		bucket = "medha-dev"
	}

	if endpoint == "" || strings.HasPrefix(endpoint, "<") {
		endpoint = "localhost:9000"
	}
	if accessKey == "" || strings.HasPrefix(accessKey, "<") {
		log.Fatal("S3_ACCESS_KEY is not set. Refusing to fall back to default credentials — set the variable explicitly before running this utility.")
	}
	if secretKey == "" || strings.HasPrefix(secretKey, "<") {
		log.Fatal("S3_SECRET_KEY is not set. Refusing to fall back to default credentials — set the variable explicitly before running this utility.")
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		log.Fatalf("S3 client: %v", err)
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("DB connect: %v", err)
	}
	defer pool.Close()

	objectsCh := client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    "public/festival-logos/",
		Recursive: true,
	})

	for object := range objectsCh {
		if object.Err != nil {
			fmt.Println("Error:", object.Err)
			continue
		}

		key := object.Key
		fmt.Println("Found object:", key)

		// public/festival-logos/akshaya-tritiya.svg
		// public/festival-logos/akshaya-tritiya_vec.svg
		filename := strings.TrimPrefix(key, "public/festival-logos/")

		slug := filename
		if idx := strings.LastIndex(slug, "."); idx != -1 {
			slug = slug[:idx] // strip extension
		}
		slug = strings.TrimSuffix(slug, "_vec")

		publicURL := fmt.Sprintf("http://localhost:9000/%s/%s", bucket, key)

		fmt.Printf("Updating %s -> %s\n", slug, publicURL)
		_, err := pool.Exec(ctx, "UPDATE festival_logos SET image_url_no_bg = $1 WHERE slug = $2 OR slug = $3", publicURL, slug, strings.ReplaceAll(slug, "_", "-"))
		if err != nil {
			fmt.Println("DB Update Error:", err)
		}
	}
	fmt.Println("Done")
}
