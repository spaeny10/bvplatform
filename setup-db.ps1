# ============================================================
# PostgreSQL + TimescaleDB Setup for ONVIF Tool
# Run as Administrator after installing PostgreSQL
# ============================================================

param(
    [string]$PgPassword = "onvif_dev_password",
    [string]$PgUser = "onvif",
    [string]$PgDatabase = "onvif_tool",
    [int]$PgPort = 5432
)

$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  PostgreSQL Setup for ONVIF Tool" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

# Find PostgreSQL installation
$pgDirs = @(
    "C:\Program Files\PostgreSQL\18",
    "C:\Program Files\PostgreSQL\17",
    "C:\Program Files\PostgreSQL\16",
    "C:\Program Files\PostgreSQL\15"
)

$pgHome = $null
foreach ($dir in $pgDirs) {
    if (Test-Path "$dir\bin\psql.exe") {
        $pgHome = $dir
        break
    }
}

if (-not $pgHome) {
    Write-Host "[ERROR] PostgreSQL not found!" -ForegroundColor Red
    Write-Host ""
    Write-Host "Please install PostgreSQL first:" -ForegroundColor Yellow
    Write-Host "  1. Download from: https://www.postgresql.org/download/windows/" -ForegroundColor White
    Write-Host "  2. Run the installer (use default options)" -ForegroundColor White
    Write-Host "  3. Remember the superuser password you set" -ForegroundColor White
    Write-Host "  4. Re-run this script" -ForegroundColor White
    Write-Host ""
    exit 1
}

$psql = "$pgHome\bin\psql.exe"
$pgBin = "$pgHome\bin"
Write-Host "  Found PostgreSQL at: $pgHome" -ForegroundColor Green

# Add pg bin to PATH for this session
$env:PATH = "$pgBin;$env:PATH"

# Get superuser password
$pgSuperPassword = Read-Host -Prompt "Enter PostgreSQL superuser (postgres) password"

# Set PGPASSWORD for non-interactive commands
$env:PGPASSWORD = $pgSuperPassword

Write-Host ""
Write-Host "[1/4] Creating database user '$PgUser'..." -ForegroundColor Yellow

# Create the user (ignore error if exists)
try {
    & $psql -U postgres -p $PgPort -h localhost -c "DO `$`$BEGIN IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = '$PgUser') THEN CREATE ROLE $PgUser WITH LOGIN PASSWORD '$PgPassword' CREATEDB; END IF; END`$`$;" 2>&1
    Write-Host "  User '$PgUser' ready" -ForegroundColor Green
}
catch {
    Write-Host "  Note: $($_.Exception.Message)" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "[2/4] Creating database '$PgDatabase'..." -ForegroundColor Yellow

# Create database (ignore error if exists)
try {
    & $psql -U postgres -p $PgPort -h localhost -c "SELECT 1 FROM pg_database WHERE datname = '$PgDatabase'" 2>&1 | Out-Null
    & $psql -U postgres -p $PgPort -h localhost -tc "SELECT 1 FROM pg_database WHERE datname = '$PgDatabase'" 2>&1 | ForEach-Object {
        if ($_.Trim() -ne "1") {
            & $psql -U postgres -p $PgPort -h localhost -c "CREATE DATABASE $PgDatabase OWNER $PgUser;" 2>&1
        }
    }
    Write-Host "  Database '$PgDatabase' ready" -ForegroundColor Green
}
catch {
    # Try creating anyway
    & $psql -U postgres -p $PgPort -h localhost -c "CREATE DATABASE $PgDatabase OWNER $PgUser;" 2>&1 | Out-Null
}

Write-Host ""
Write-Host "[3/4] Checking TimescaleDB extension..." -ForegroundColor Yellow

# Check if TimescaleDB is available
$tsdbAvailable = $false
try {
    $result = & $psql -U postgres -p $PgPort -h localhost -d $PgDatabase -tc "SELECT 1 FROM pg_available_extensions WHERE name = 'timescaledb'" 2>&1
    if ($result -match "1") {
        $tsdbAvailable = $true
        Write-Host "  TimescaleDB extension available" -ForegroundColor Green
    }
}
catch {}

if (-not $tsdbAvailable) {
    Write-Host "  TimescaleDB NOT installed" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "  TimescaleDB is optional but recommended." -ForegroundColor Yellow
    Write-Host "  To install it later:" -ForegroundColor Yellow
    Write-Host "    1. Download from: https://docs.timescale.com/self-hosted/latest/install/installation-windows/" -ForegroundColor White
    Write-Host "    2. Follow the installation instructions" -ForegroundColor White
    Write-Host "    3. Re-run this script" -ForegroundColor White
    Write-Host ""
    Write-Host "  Proceeding WITHOUT TimescaleDB (will use standard PostgreSQL tables)..." -ForegroundColor Yellow
}

Write-Host ""
Write-Host "[4/4] Initializing database schema..." -ForegroundColor Yellow

# Switch to the onvif user for schema creation
$env:PGPASSWORD = $PgPassword

$projectDir = "C:\Users\Shawn\Documents\Codebase\Onvif tool"

if ($tsdbAvailable) {
    # Use the full init.sql with TimescaleDB
    & $psql -U $PgUser -p $PgPort -h localhost -d $PgDatabase -f "$projectDir\init.sql" 2>&1
    Write-Host "  Schema initialized WITH TimescaleDB hypertables" -ForegroundColor Green
}
else {
    # Use a modified schema without TimescaleDB
    $schemaNoTimescale = "$projectDir\init_no_timescaledb.sql"
    if (Test-Path $schemaNoTimescale) {
        & $psql -U $PgUser -p $PgPort -h localhost -d $PgDatabase -f $schemaNoTimescale 2>&1
    }
    else {
        Write-Host "  Creating schema without TimescaleDB..." -ForegroundColor White
        # Run SQL inline without TimescaleDB-specific commands
        & $psql -U $PgUser -p $PgPort -h localhost -d $PgDatabase -c @"
-- Cameras table
CREATE TABLE IF NOT EXISTS cameras (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    onvif_address   TEXT NOT NULL,
    username        TEXT DEFAULT '',
    password        TEXT DEFAULT '',
    rtsp_uri        TEXT DEFAULT '',
    sub_stream_uri  TEXT DEFAULT '',
    retention_days  INT DEFAULT 30,
    recording       BOOLEAN DEFAULT true,
    status          TEXT DEFAULT 'offline',
    profile_token   TEXT DEFAULT '',
    manufacturer    TEXT DEFAULT '',
    model           TEXT DEFAULT '',
    firmware        TEXT DEFAULT '',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

-- Video segments table
CREATE TABLE IF NOT EXISTS segments (
    id          BIGSERIAL PRIMARY KEY,
    camera_id   UUID NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    start_time  TIMESTAMPTZ NOT NULL,
    end_time    TIMESTAMPTZ NOT NULL,
    file_path   TEXT NOT NULL,
    file_size   BIGINT DEFAULT 0,
    duration_ms INT DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_segments_camera_time ON segments (camera_id, start_time DESC);

-- Events / metadata table
CREATE TABLE IF NOT EXISTS events (
    id          BIGSERIAL PRIMARY KEY,
    camera_id   UUID NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    event_time  TIMESTAMPTZ NOT NULL,
    event_type  TEXT NOT NULL,
    details     JSONB DEFAULT '{}',
    thumbnail   TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_events_camera_type ON events (camera_id, event_type, event_time DESC);
CREATE INDEX IF NOT EXISTS idx_events_details ON events USING GIN (details);

-- Export jobs table
CREATE TABLE IF NOT EXISTS exports (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    camera_id   UUID NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    start_time  TIMESTAMPTZ NOT NULL,
    end_time    TIMESTAMPTZ NOT NULL,
    status      TEXT DEFAULT 'pending',
    file_path   TEXT DEFAULT '',
    file_size   BIGINT DEFAULT 0,
    error       TEXT DEFAULT '',
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);
"@ 2>&1
    }
    Write-Host "  Schema initialized (standard PostgreSQL, no hypertables)" -ForegroundColor Green
}

# Verify connection with the app user
$env:PGPASSWORD = $PgPassword
Write-Host ""
Write-Host "Verifying connection..." -ForegroundColor Yellow
$tables = & $psql -U $PgUser -p $PgPort -h localhost -d $PgDatabase -tc "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public'" 2>&1
Write-Host "  Tables created: $($tables.Trim())" -ForegroundColor Green

Write-Host ""
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  Setup Complete!" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Database URL:" -ForegroundColor White
Write-Host "  postgres://${PgUser}:${PgPassword}@localhost:${PgPort}/${PgDatabase}?sslmode=disable" -ForegroundColor Green
Write-Host ""
Write-Host "  Next steps:" -ForegroundColor Cyan
Write-Host "    1. Start the backend:  .\bin\onvif-tool.exe" -ForegroundColor White
Write-Host "    2. Start the frontend: cd frontend && npm run dev" -ForegroundColor White
Write-Host "    3. Open: http://localhost:3000" -ForegroundColor White
Write-Host ""
