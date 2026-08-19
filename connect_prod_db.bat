@echo off
REM ============================================================================
REM  BREAK-GLASS  --  PRODUCTION database / Redis / S3 storage tunnels
REM ----------------------------------------------------------------------------
REM  Opens SSH tunnels that map PRODUCTION Postgres/Redis/SeaweedFS onto localhost
REM  (5433 / 6379 / 8333). For deliberate, occasional prod inspection ONLY.
REM
REM  DANGER: do NOT run the API or any app in `development` while these tunnels
REM  are open. A dev process pointed at localhost:5433/6379/8333 will read and
REM  write PRODUCTION data, and the API's startup env-guard CANNOT see through a
REM  localhost port-forward. Close this window the moment you are done.
REM ============================================================================

if "%1"=="min" goto :run

echo(
echo   *** You are about to open tunnels to PRODUCTION infrastructure. ***
echo(
set /p "CONFIRM=Type PROD (exactly) to continue, anything else to abort: "
if not "%CONFIRM%"=="PROD" (
    echo Aborted -- no tunnels opened.
    exit /b 1
)
start /min "" "%~f0" min
exit /b

:run
echo Starting Cloudflare authentication and opening Medha Production Server tunnels...
echo.

echo [1/3] Querying Kubernetes for production database ClusterIP...
for /f "tokens=3" %%i in ('ssh medha-master "kubectl get svc postgres -n prod" ^| findstr postgres') do set DB_IP=%%i

echo [2/3] Querying Kubernetes for production Redis ClusterIP...
for /f "tokens=3" %%i in ('ssh medha-master "kubectl get svc redis -n prod" ^| findstr redis') do set REDIS_IP=%%i

echo [3/3] Querying Kubernetes for production SeaweedFS ClusterIP...
for /f "tokens=3" %%i in ('ssh medha-master "kubectl get svc seaweedfs -n prod" ^| findstr seaweedfs') do set S3_IP=%%i

echo.
echo Live Cluster IPs resolved:
echo   - Postgres:  %DB_IP%:5432
echo   - Redis:     %REDIS_IP%:6379
echo   - S3 storage: %S3_IP%:8333
echo.

echo Establishing secure tunnels via medha-worker...
ssh -L 5433:%DB_IP%:5432 -L 6379:%REDIS_IP%:6379 -L 8333:%S3_IP%:8333 medha-worker -N
