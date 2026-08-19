@echo off
set PATH=%USERPROFILE%\go\bin;%PATH%

if "%1"=="setup" (
    echo Running developer environment setup...
    powershell -ExecutionPolicy Bypass -File scripts\setup.ps1
    exit /b
)

if "%1"=="run-remote" (
    echo Starting hybrid mode API pointing to remote dev server...
    powershell -ExecutionPolicy Bypass -File scripts\run-remote.ps1
    exit /b
)

if "%1"=="start" (
    echo Starting local Docker services...
    docker compose -f docker-compose.yml up -d
    echo Waiting for services to start...
    timeout /t 3 /nobreak > nul
    echo Applying database migrations...
    goose -dir migrations postgres "%DATABASE_URL%" up
    echo Starting air hot reload...
    where air >nul 2>nul && air || "%USERPROFILE%\go\bin\air.exe"
    exit /b
)

if "%1"=="dev" (
    echo Starting air hot reload...
    where air >nul 2>nul && air || "%USERPROFILE%\go\bin\air.exe"
    exit /b
)

if "%1"=="swagger" (
    echo Generating Swagger documentation...
    swag init -g cmd/medha-api/main.go -d ./ --pd --useStructName --packageName docs -o api/openapi --ot yaml
    swag init -g cmd/medha-api/main.go -d ./ --pd --useStructName --packageName docs -o api/openapi
    go run cmd/sync-bruno/main.go
    go run cmd/sync-postman/main.go
    exit /b
)

if "%1"=="build" (
    echo Building binary...
    go build -o bin/medha-api.exe ./cmd/medha-api
    exit /b
)

if "%1"=="run" (
    echo Building and running...
    go build -o bin/medha-api.exe ./cmd/medha-api
    bin\medha-api.exe
    exit /b
)

if "%1"=="test" (
    echo Running tests...
    go test -race -v ./...
    exit /b
)

if "%1"=="keys" (
    echo Generating RS256 keys...
    go run cmd/genkeys/main.go
    exit /b
)

if "%1"=="clean" (
    echo Cleaning...
    go clean
    if exist tmp rmdir /s /q tmp
    exit /b
)

if "%1"=="migrate-up" (
    echo Applying database migrations...
    goose -dir migrations postgres "%DATABASE_URL%" up
    exit /b
)

if "%1"=="migrate-down" (
    echo Rolling back database migrations...
    goose -dir migrations postgres "%DATABASE_URL%" down
    exit /b
)

if "%1"=="migrate-status" (
    echo Checking migration status...
    goose -dir migrations postgres "%DATABASE_URL%" status
    exit /b
)

if "%1"=="migrate-create" (
    if "%2"=="" (
        echo Error: Migration name is required. Usage: make migrate-create name_of_migration
        exit /b 1
    )
    echo Creating migration %2...
    goose -dir migrations create %2 sql
    exit /b
)

echo Unknown command: %1
echo Available commands: setup, run-remote, dev, swagger, build, run, test, keys, clean, migrate-up, migrate-down, migrate-status, migrate-create, start
