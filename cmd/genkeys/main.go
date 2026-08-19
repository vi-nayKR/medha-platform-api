package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	keyDir := "keys"
	if err := os.MkdirAll(keyDir, 0755); err != nil {
		fmt.Printf("Failed to create keys directory: %v\n", err)
		os.Exit(1)
	}

	privateKeyPath := filepath.Join(keyDir, "jwt_private.pem")
	publicKeyPath := filepath.Join(keyDir, "jwt_public.pem")

	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		fmt.Printf("Failed to generate private key: %v\n", err)
		os.Exit(1)
	}

	// Marshal private key to PKCS#1 DER format
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	}

	privFile, err := os.Create(privateKeyPath)
	if err != nil {
		fmt.Printf("Failed to create private key file: %v\n", err)
		os.Exit(1)
	}
	defer privFile.Close()

	if err := pem.Encode(privFile, privateKeyBlock); err != nil {
		fmt.Printf("Failed to encode private key to PEM: %v\n", err)
		os.Exit(1)
	}

	// Generate public key
	publicKey := &privateKey.PublicKey
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		fmt.Printf("Failed to marshal public key: %v\n", err)
		os.Exit(1)
	}

	publicKeyBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	}

	pubFile, err := os.Create(publicKeyPath)
	if err != nil {
		fmt.Printf("Failed to create public key file: %v\n", err)
		os.Exit(1)
	}
	defer pubFile.Close()

	if err := pem.Encode(pubFile, publicKeyBlock); err != nil {
		fmt.Printf("Failed to encode public key to PEM: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Successfully generated RSA private and public keys in keys/ directory!")
}
