package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const publicPolicy = `{
	"Version": "2012-10-17",
	"Statement": [
		{
			"Effect": "Allow",
			"Principal": {"AWS": ["*"]},
			"Action": ["s3:GetObject"],
			"Resource": ["arn:aws:s3:::%s/*"]
		}
	]
}`

func main() {
	_ = godotenv.Load("../../.env")

	endpoint := os.Getenv("S3_ENDPOINT")
	accessKey := os.Getenv("S3_ACCESS_KEY")
	secretKey := os.Getenv("S3_SECRET_KEY")
	bucket := os.Getenv("S3_BUCKET")

	if bucket == "" {
		bucket = "medha-dev"
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		log.Fatalf("Failed to create S3 client: %v", err)
	}

	policy := fmt.Sprintf(publicPolicy, bucket)
	err = client.SetBucketPolicy(context.Background(), bucket, policy)
	if err != nil {
		log.Fatalf("Failed to set public policy on bucket %s: %v", bucket, err)
	}

	log.Printf("Successfully set public policy for bucket: %s", bucket)
}
