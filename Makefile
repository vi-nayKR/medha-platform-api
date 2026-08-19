.PHONY: build run dev test lint lint-fix migrate-up migrate-down migrate-create swagger clean setup keys \
        services-up services-down services-reset local-setup start check-env services-status run-remote tunnel deploy

# Load environment variables from .env if it exists
-include .env
export

GOPATH := $(shell go env GOPATH)
export PATH := /opt/homebrew/bin:$(GOPATH)/bin:$(shell echo $$PATH)

# Determine the executable extension based on the OS
ifeq ($(OS),Windows_NT)
    EXEC_EXT := .exe
else
    EXEC_EXT :=
endif

# Initial project setup (dependencies, database, config)
setup:
	@chmod +x scripts/setup.sh
	./scripts/setup.sh

build:
	go build -o bin/medha-api$(EXEC_EXT) ./cmd/medha-api

# Run the binary
run: build
	./bin/medha-api$(EXEC_EXT)

# Run with hot reload (air)
dev:
	$(GOPATH)/bin/air

# Run all tests with race detector
test:
	go test -race -v ./...

# Run linter
lint:
	golangci-lint run

# Run linter with auto-fix
lint-fix:
	golangci-lint run --fix

# Database migrations
migrate-up:
	@echo "Applying database migrations..."
	@$(GOPATH)/bin/goose -dir migrations postgres "$(DATABASE_URL)" up

migrate-down:
	@echo "Rolling back database migrations..."
	@$(GOPATH)/bin/goose -dir migrations postgres "$(DATABASE_URL)" down

migrate-create:
	@$(GOPATH)/bin/goose -dir migrations create $(name) sql

migrate-status:
	@echo "Checking migration status..."
	@$(GOPATH)/bin/goose -dir migrations postgres "$(DATABASE_URL)" status

# Generate swagger docs and sync Bruno collection
swagger:
	@echo "Generating Swagger documentation..."
	@$(GOPATH)/bin/swag init -g cmd/medha-api/main.go -d ./ --pd --useStructName --packageName docs -o api/openapi --ot yaml
	@$(GOPATH)/bin/swag init -g cmd/medha-api/main.go -d ./ --pd --useStructName --packageName docs -o api/openapi
	@go run cmd/sync-bruno/main.go
	@go run cmd/sync-postman/main.go

# Clean build artifacts
clean:
	go clean
	rm -rf tmp/

# Generate JWT RS256 keys for local development
keys:
	go run cmd/genkeys/main.go

# ── Local Docker services ──────────────────────────────────────────
COMPOSE_FILE := docker-compose.yml

# Start local Docker services (postgres + redis)
services-up:
	docker compose -f $(COMPOSE_FILE) up -d
	@echo "Waiting for postgres to be ready..."
	@docker compose -f $(COMPOSE_FILE) exec postgres pg_isready -U medha_user -d medha_dev || sleep 3
	@echo "Services ready"

# Stop local Docker services
services-down:
	docker compose -f $(COMPOSE_FILE) down

# Stop and wipe all local data (fresh start)
services-reset:
	docker compose -f $(COMPOSE_FILE) down -v
	docker compose -f $(COMPOSE_FILE) up -d

# ── Env management ─────────────────────────────────────────────────

# Scaffold the local API .env (development only)
local-setup:
	../medha-infra/scripts/sync-env.sh api

# Full local dev start: sync env + run
start:
	@../medha-infra/scripts/sync-env.sh api
	@air

# Validate all required env vars are set
check-env:
	../medha-infra/scripts/validate-env-contract.sh

# Show running local services
services-status:
	docker compose -f $(COMPOSE_FILE) ps

# ── Hybrid Development (Local API + Remote Data) ───────────────────

# Start SSH tunnels to dev server (requires 'medha-server' alias in ~/.ssh/config)
tunnel:
	@chmod +x scripts/run-remote.sh
	./scripts/run-remote.sh

# Run the API locally pointing to remote data (alias for tunnel)
run-remote: tunnel

# Run local compiler and SCP deploy tool
deploy:
	go run cmd/deploy/main.go -env $(or $(env),dev)

# Seal Kubernetes secrets using the public certificate
seal-dev:
	kubeseal --cert ../medha-infra/secrets/pub-cert.pem < deployments/secrets-dev.yaml > ../medha-infra/kubernetes/overlays/dev/backend-sealed-secret.yaml

seal-prod:
	kubeseal --cert ../medha-infra/secrets/pub-cert.pem < deployments/secrets-prod.yaml > ../medha-infra/kubernetes/overlays/prod/backend-sealed-secret.yaml
