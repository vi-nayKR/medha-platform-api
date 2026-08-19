# Medha API — Remote Data / Local Backend Startup Script (Windows PowerShell)
# Usage: powershell -ExecutionPolicy Bypass -File run-remote.ps1

$ErrorActionPreference = "Stop"

$REMOTE_ALIAS = "medha-server"

Write-Host ""
Write-Host ">>> Starting Medha API with REMOTE DATA (SSH Tunnel)..." -ForegroundColor Cyan
Write-Host ""

# ── Step 1: Establish SSH Tunnel ─────────────────────────────────────────────
Write-Host "[1/2] Establishing SSH tunnel (5432 + 6379 + 9000 + 9001)..." -ForegroundColor Yellow
Write-Host "      Press Ctrl+C later to stop the tunnel." -ForegroundColor Gray

# Start SSH tunnel in the background using a separate process
$tunnelArgs = "-L 5432:localhost:5432 -L 6379:localhost:6379 -L 9000:localhost:9000 -L 9001:localhost:9001 $REMOTE_ALIAS -N"
Write-Host "      Running: ssh $tunnelArgs" -ForegroundColor Gray
Start-Process ssh -ArgumentList $tunnelArgs -NoNewWindow

Write-Host "      ✅ Tunnel established. Checking ports..." -ForegroundColor Green
Start-Sleep -Seconds 2

# Verify ports are open locally
$dbReady = Test-NetConnection -Port 5432 -ComputerName localhost -InformationLevel Quiet
$redisReady = Test-NetConnection -Port 6379 -ComputerName localhost -InformationLevel Quiet
$s3Ready = Test-NetConnection -Port 8333 -ComputerName localhost -InformationLevel Quiet

if (!$dbReady) {
    Write-Host "❌ Failed to open tunnel for Postgres (5432)." -ForegroundColor Red
    Write-Host "   Make sure no local Postgres is running on Port 5432." -ForegroundColor Yellow
    exit 1
}
if (!$redisReady) {
    Write-Host "❌ Failed to open tunnel for Redis (6379)." -ForegroundColor Red
    Write-Host "   Make sure no local Redis is running on Port 6379." -ForegroundColor Yellow
    exit 1
}
if (!$s3Ready) {
    Write-Host "❌ Failed to open tunnel for S3 storage (8333)." -ForegroundColor Red
    Write-Host "   Make sure no local S3-compatible server is running on Port 8333." -ForegroundColor Yellow
    exit 1
}

# ── Step 2: Load .env and Start API ───────────────────────────────────────────
if (!(Test-Path ".env")) {
    Write-Host "❌ .env file not found." -ForegroundColor Red
    exit 1
}

# Load the .env file into environment variables
Get-Content ".env" | Where-Object { $_ -notmatch "^\s*#" -and $_ -match "=" } | ForEach-Object {
    $parts = $_ -split "=", 2
    if ($parts.Length -eq 2) {
        $key = $parts[0].Trim()
        $value = $parts[1].Trim()
        [System.Environment]::SetEnvironmentVariable($key, $value, "Process")
    }
}

Write-Host "[2/2] Starting API server (Localhost)..." -ForegroundColor Yellow
Write-Host "      Pointing to SSH Tunnel -> Remote DB" -ForegroundColor Gray
Write-Host ""

$serverPort = if ($env:SERVER_PORT) { $env:SERVER_PORT } else { "8082" }
Write-Host "  >>> API: http://localhost:$serverPort" -ForegroundColor Green

go run cmd/medha-api/main.go
