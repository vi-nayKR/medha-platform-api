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
	// Load environment variables if .env exists
	_ = godotenv.Load("../../.env")
	_ = godotenv.Load(".env")

	endpoint := os.Getenv("S3_ENDPOINT")
	accessKey := os.Getenv("S3_ACCESS_KEY")
	secretKey := os.Getenv("S3_SECRET_KEY")

	if endpoint == "" || accessKey == "" || secretKey == "" {
		log.Fatal("S3 environment variables are not set (S3_ENDPOINT, S3_ACCESS_KEY, S3_SECRET_KEY)")
	}

	useSSL := os.Getenv("ENVIRONMENT") == "production"

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		log.Fatalf("Failed to create S3 client: %v", err)
	}

	ctx := context.Background()

	// 1. List and delete all existing buckets
	buckets, err := client.ListBuckets(ctx)
	if err != nil {
		log.Fatalf("Failed to list buckets: %v", err)
	}

	for _, bucket := range buckets {
		log.Printf("Emptying and deleting bucket: %s", bucket.Name)

		// Empty the bucket
		objectsCh := client.ListObjects(ctx, bucket.Name, minio.ListObjectsOptions{Recursive: true})
		for object := range objectsCh {
			if object.Err != nil {
				log.Printf("Error listing object in bucket %s: %v", bucket.Name, object.Err)
				continue
			}
			err := client.RemoveObject(ctx, bucket.Name, object.Key, minio.RemoveObjectOptions{})
			if err != nil {
				log.Printf("Failed to remove object %s from bucket %s: %v", object.Key, bucket.Name, err)
			} else {
				log.Printf("  Deleted object: %s", object.Key)
			}
		}

		// Delete the bucket
		err = client.RemoveBucket(ctx, bucket.Name)
		if err != nil {
			log.Printf("Failed to remove bucket %s: %v", bucket.Name, err)
		} else {
			log.Printf("Bucket %s deleted.", bucket.Name)
		}
	}

	// 2. Create the new environment buckets and set public read policy
	newBuckets := []string{"medha-dev", "medha-main", "medha-prod", "medha-apk-release"}
	for _, bName := range newBuckets {
		log.Printf("Creating bucket: %s", bName)
		err := client.MakeBucket(ctx, bName, minio.MakeBucketOptions{})
		if err != nil {
			exists, errExists := client.BucketExists(ctx, bName)
			if errExists == nil && exists {
				log.Printf("Bucket %s already exists.", bName)
			} else {
				log.Printf("Failed to create bucket %s: %v", bName, err)
				continue
			}
		} else {
			log.Printf("Bucket %s created successfully.", bName)
		}

		// Set Public Read Policy
		policy := fmt.Sprintf(publicPolicy, bName)
		err = client.SetBucketPolicy(ctx, bName, policy)
		if err != nil {
			log.Printf("Failed to set public policy on bucket %s: %v", bName, err)
		} else {
			log.Printf("Public read policy applied to bucket: %s", bName)
		}
	}

	log.Println("S3 storage setup completed successfully.")
}
