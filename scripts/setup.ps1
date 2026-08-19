# Medha API — Local Development Setup Script (Windows)
# Run this in PowerShell as Administrator
# Usage: powershell -ExecutionPolicy Bypass -File scripts\setup.ps1

$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Medha API — Windows Dev Setup" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# ── 1. Check for winget ──────────────────────────────────────────────────────
if (!(Get-Command winget -ErrorAction SilentlyContinue)) {
    Write-Host "❌ winget not found. Please ensure App Installer is updated from the Microsoft Store." -ForegroundColor Red
    exit 1
} else {
    Write-Host "[1/5] winget is available ✓" -ForegroundColor Green
}

# ── 2. Install Go ────────────────────────────────────────────────────────────
Write-Host "[2/5] Installing Go..." -ForegroundColor Cyan
winget install GoLang.Go --accept-source-agreements --accept-package-agreements
if (!(Get-Command go -ErrorAction SilentlyContinue)) {
    # Refresh PATH without restarting shell
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
}
Write-Host "   Go version: $(go version)" -ForegroundColor Green



# ── 3. Install OpenSSL (for JWT key generation) ──────────────────────────────
Write-Host "[3/5] Installing OpenSSL..." -ForegroundColor Cyan
winget install ShiningLight.OpenSSL --accept-source-agreements --accept-package-agreements
# Refresh PATH
$env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")

# ── 4. Set up .env ───────────────────────────────────────────────────────────
Write-Host "[4/5] Configuring environment..." -ForegroundColor Cyan
if (!(Test-Path ".env")) {
    Copy-Item ".env.example" ".env"
    Write-Host "   Created .env from .env.example" -ForegroundColor Green
} else {
    Write-Host "   .env already exists — skipping." -ForegroundColor Green
}
Write-Host ""
Write-Host "   ⚠️  Open .env and fill in any <PLACEHOLDER> values before starting the server." -ForegroundColor Yellow
Write-Host ""

# ── 5. Generate JWT RS256 Keys ───────────────────────────────────────────────
Write-Host "[5/5] Generating JWT RS256 keys..." -ForegroundColor Cyan
if (!(Test-Path "keys\jwt_private.pem")) {
    New-Item -ItemType Directory -Force -Path keys | Out-Null
    openssl genrsa -out keys\jwt_private.pem 2048
    openssl rsa -in keys\jwt_private.pem -pubout -out keys\jwt_public.pem
    Write-Host "   JWT keys generated in keys\" -ForegroundColor Green
} else {
    Write-Host "   JWT keys already exist — skipping." -ForegroundColor Green
}

# ── 6. Install Go Tools (goose + air + swag) ──────────────────────────────────
Write-Host "Installing Go tools (goose, air, swag)..." -ForegroundColor Cyan
go install github.com/pressly/goose/v3/cmd/goose@latest
go install github.com/air-verse/air@latest
go install github.com/swaggo/swag/cmd/swag@latest
Write-Host "   goose, air, and swag installed." -ForegroundColor Green

# ── Done ─────────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "========================================" -ForegroundColor Green
Write-Host "  ✅ Setup complete!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
Write-Host ""
Write-Host "Next steps:" -ForegroundColor Cyan
Write-Host "  1. Start the API server:" -ForegroundColor White
Write-Host "       go run cmd\medha-api\main.go" -ForegroundColor Yellow
Write-Host ""
Write-Host "  2. Open the API in your browser:" -ForegroundColor White
Write-Host "       http://localhost:8082/api/docs    (Swagger UI)" -ForegroundColor Yellow
Write-Host "       http://localhost:8082/health      (Health check)" -ForegroundColor Yellow
Write-Host ""
