# Medha API — Local Dev Startup Script (Windows PowerShell)
# Equivalent of run-local.sh for Windows users.
# Usage: powershell -ExecutionPolicy Bypass -File run-local.ps1

$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host ">>> Starting Medha API Local Dev Environment..." -ForegroundColor Cyan
Write-Host ""

# ── Step 1: Start Docker Services (PostgreSQL + Redis) ───────────────────────
Write-Host "[1/3] Starting Docker services (postgres + redis)..." -ForegroundColor Yellow
docker compose up -d

Write-Host "      Waiting for postgres to be ready..." -ForegroundColor Gray
$retries = 0
$maxRetries = 20
do {
    Start-Sleep -Seconds 3
    $retries++
    $result = docker compose exec postgres pg_isready -U medha_user -d medha_dev 2>&1
    $ready = $result -match "accepting connections"
} while (-not $ready -and $retries -lt $maxRetries)

if ($retries -ge $maxRetries) {
    Write-Host "" 
    Write-Host "❌ Postgres did not become ready in time." -ForegroundColor Red
    Write-Host "   Run: docker compose logs postgres" -ForegroundColor Yellow
    exit 1
}
Write-Host "      ✅ Services ready." -ForegroundColor Green
Write-Host ""

# ── Step 2: Verify .env exists ───────────────────────────────────────────────
if (!(Test-Path ".env")) {
    Write-Host "❌ .env file not found." -ForegroundColor Red
    Write-Host "   Copy .env.example to .env and fill in your values:" -ForegroundColor Yellow
    Write-Host "     copy .env.example .env" -ForegroundColor Yellow
    exit 1
}

# Load DATABASE_URL from .env for goose
$envContent = Get-Content ".env" | Where-Object { $_ -notmatch "^\s*#" -and $_ -match "=" }
foreach ($line in $envContent) {
    $parts = $line -split "=", 2
    if ($parts.Length -eq 2) {
        $key   = $parts[0].Trim()
        $value = $parts[1].Trim()
        [System.Environment]::SetEnvironmentVariable($key, $value, "Process")
    }
}

$dbUrl = $env:DATABASE_URL
if (!$dbUrl) {
    Write-Host "❌ DATABASE_URL not set in .env" -ForegroundColor Red
    exit 1
}

$serverPort = if ($env:SERVER_PORT) { $env:SERVER_PORT } else { "8082" }

# ── Step 3: Run Database Migrations ──────────────────────────────────────────
Write-Host "[2/3] Running database migrations..." -ForegroundColor Yellow
$goosePath = Join-Path (go env GOPATH) "bin\goose.exe"
if (Test-Path $goosePath) {
    & $goosePath -dir migrations postgres $dbUrl up
} elseif (Get-Command goose -ErrorAction SilentlyContinue) {
    goose -dir migrations postgres $dbUrl up
} else {
    Write-Host "⚠️  goose not found. Install it with:" -ForegroundColor Yellow
    Write-Host "     go install github.com/pressly/goose/v3/cmd/goose@latest" -ForegroundColor Yellow
    Write-Host "   Skipping migrations." -ForegroundColor Yellow
}
Write-Host ""

# ── Step 4: Start the API ─────────────────────────────────────────────────────
Write-Host "[3/3] Starting API server..." -ForegroundColor Yellow
Write-Host ""
Write-Host "  >>> API will be available at: http://localhost:$serverPort" -ForegroundColor Green
Write-Host "  >>> Swagger UI:               http://localhost:$serverPort/api/docs" -ForegroundColor Green
Write-Host "  >>> Health check:             http://localhost:$serverPort/health" -ForegroundColor Green
Write-Host ""

$airPath = Join-Path (go env GOPATH) "bin\air.exe"
if (Test-Path $airPath) {
    Write-Host "  Using air for hot-reload (Ctrl+C to stop)..." -ForegroundColor Gray
    & $airPath
} elseif (Get-Command air -ErrorAction SilentlyContinue) {
    air
} else {
    Write-Host "  air not found — starting with go run (no hot-reload)..." -ForegroundColor Yellow
    Write-Host "  Install air for hot-reload: go install github.com/air-verse/air@latest" -ForegroundColor Gray
    go run cmd\medha-api\main.go
}
