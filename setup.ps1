# ============================================================
# ONVIF Tool - Machine Setup Script
# Downloads and installs all prerequisites
# Run this script as Administrator!
# ============================================================

param(
    [switch]$SkipDocker,
    [switch]$SkipGo,
    [switch]$SkipFFmpeg,
    [switch]$SkipNodeFix
)

$ErrorActionPreference = "Continue"
$ProgressPreference = "SilentlyContinue" # Faster downloads

$ProjectDir = "C:\Users\Shawn\Documents\Codebase\Onvif tool"
$DownloadDir = "$ProjectDir\setup_downloads"
$NodePath = "C:\Users\Shawn\AppData\Local\Programs\nodejs"

Write-Host ""
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  ONVIF Tool - Machine Setup" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

# Check if running as admin
$isAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Write-Host "[WARNING] Not running as Administrator!" -ForegroundColor Yellow
    Write-Host "  Some installations may fail. Consider re-running as Admin." -ForegroundColor Yellow
    Write-Host ""
}

# Create download directory
New-Item -ItemType Directory -Force -Path $DownloadDir | Out-Null

# ============================================================
# STEP 1: Fix Node.js PATH
# ============================================================
if (-not $SkipNodeFix) {
    Write-Host "[1/5] Checking Node.js..." -ForegroundColor Yellow
    
    if (Test-Path "$NodePath\node.exe") {
        $nodeVer = & "$NodePath\node.exe" --version
        Write-Host "  Found Node.js $nodeVer at $NodePath" -ForegroundColor Green
        
        # Add to current session PATH
        if ($env:PATH -notlike "*$NodePath*") {
            $env:PATH = "$NodePath;$env:PATH"
            Write-Host "  Added to session PATH" -ForegroundColor Green
        }
        
        # Add to user PATH permanently
        $userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
        if ($userPath -notlike "*$NodePath*") {
            [Environment]::SetEnvironmentVariable("PATH", "$NodePath;$userPath", "User")
            Write-Host "  Added to user PATH permanently" -ForegroundColor Green
        }
        
        # Also add npm global path
        $npmGlobalPath = "$env:APPDATA\npm"
        if ($env:PATH -notlike "*$npmGlobalPath*") {
            $env:PATH = "$npmGlobalPath;$env:PATH"
        }
        $userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
        if ($userPath -notlike "*$npmGlobalPath*") {
            [Environment]::SetEnvironmentVariable("PATH", "$npmGlobalPath;$userPath", "User")
        }
    } else {
        Write-Host "  Node.js not found at expected location" -ForegroundColor Red
        Write-Host "  Will need manual install from https://nodejs.org" -ForegroundColor Yellow
    }
    Write-Host ""
}

# ============================================================
# STEP 2: Install Go
# ============================================================
if (-not $SkipGo) {
    Write-Host "[2/5] Installing Go..." -ForegroundColor Yellow
    
    $goInstalled = $false
    $goPath = "C:\Program Files\Go\bin\go.exe"
    
    if (Test-Path $goPath) {
        $goVer = & $goPath version
        Write-Host "  Go already installed: $goVer" -ForegroundColor Green
        $goInstalled = $true
    }
    
    if (-not $goInstalled) {
        $goVersion = "1.22.5"
        $goInstallerUrl = "https://go.dev/dl/go${goVersion}.windows-amd64.msi"
        $goInstallerPath = "$DownloadDir\go-installer.msi"
        
        Write-Host "  Downloading Go $goVersion..." -ForegroundColor White
        try {
            Invoke-WebRequest -Uri $goInstallerUrl -OutFile $goInstallerPath -UseBasicParsing
            Write-Host "  Downloaded. Installing..." -ForegroundColor White
            
            # Silent install
            Start-Process msiexec.exe -ArgumentList "/i", "`"$goInstallerPath`"", "/quiet", "/norestart" -Wait -NoNewWindow
            
            if (Test-Path $goPath) {
                Write-Host "  Go installed successfully!" -ForegroundColor Green
                # Add to current session
                $env:PATH = "C:\Program Files\Go\bin;$env:PATH"
                $env:GOPATH = "$env:USERPROFILE\go"
            } else {
                Write-Host "  Go MSI install may need manual completion" -ForegroundColor Yellow
            }
        } catch {
            Write-Host "  Download failed: $_" -ForegroundColor Red
            Write-Host "  Manual install: https://go.dev/dl/" -ForegroundColor Yellow
        }
    }
    Write-Host ""
}

# ============================================================
# STEP 3: Install FFmpeg
# ============================================================
if (-not $SkipFFmpeg) {
    Write-Host "[3/5] Installing FFmpeg..." -ForegroundColor Yellow
    
    $ffmpegDir = "C:\ffmpeg"
    $ffmpegExe = "$ffmpegDir\bin\ffmpeg.exe"
    
    if (Test-Path $ffmpegExe) {
        Write-Host "  FFmpeg already installed" -ForegroundColor Green
    } else {
        # Download FFmpeg release build
        $ffmpegUrl = "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip"
        $ffmpegZip = "$DownloadDir\ffmpeg.zip"
        
        Write-Host "  Downloading FFmpeg (this may take a minute)..." -ForegroundColor White
        try {
            Invoke-WebRequest -Uri $ffmpegUrl -OutFile $ffmpegZip -UseBasicParsing
            Write-Host "  Downloaded. Extracting..." -ForegroundColor White
            
            # Extract
            $extractDir = "$DownloadDir\ffmpeg_temp"
            Expand-Archive -Path $ffmpegZip -DestinationPath $extractDir -Force
            
            # Find the extracted folder (it has a version in the name)
            $ffmpegExtracted = Get-ChildItem $extractDir -Directory | Select-Object -First 1
            
            if ($ffmpegExtracted) {
                # Move to C:\ffmpeg
                if (Test-Path $ffmpegDir) { Remove-Item $ffmpegDir -Recurse -Force }
                Move-Item $ffmpegExtracted.FullName $ffmpegDir
                
                if (Test-Path $ffmpegExe) {
                    Write-Host "  FFmpeg installed to $ffmpegDir" -ForegroundColor Green
                    
                    # Add to PATH
                    $env:PATH = "$ffmpegDir\bin;$env:PATH"
                    
                    # Add to system PATH permanently
                    if ($isAdmin) {
                        $machinePath = [Environment]::GetEnvironmentVariable("PATH", "Machine")
                        if ($machinePath -notlike "*$ffmpegDir\bin*") {
                            [Environment]::SetEnvironmentVariable("PATH", "$ffmpegDir\bin;$machinePath", "Machine")
                            Write-Host "  Added to system PATH" -ForegroundColor Green
                        }
                    } else {
                        $userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
                        if ($userPath -notlike "*$ffmpegDir\bin*") {
                            [Environment]::SetEnvironmentVariable("PATH", "$ffmpegDir\bin;$userPath", "User")
                            Write-Host "  Added to user PATH" -ForegroundColor Green
                        }
                    }
                }
            }
            
            # Cleanup temp
            Remove-Item $extractDir -Recurse -Force -ErrorAction SilentlyContinue
        } catch {
            Write-Host "  Download failed: $_" -ForegroundColor Red
            Write-Host "  Manual install: https://www.gyan.dev/ffmpeg/builds/" -ForegroundColor Yellow
        }
    }
    Write-Host ""
}

# ============================================================
# STEP 4: Install Docker Desktop
# ============================================================
if (-not $SkipDocker) {
    Write-Host "[4/5] Checking Docker Desktop..." -ForegroundColor Yellow
    
    $dockerInstalled = $false
    $dockerPaths = @(
        "C:\Program Files\Docker\Docker\resources\bin\docker.exe",
        "$env:ProgramFiles\Docker\Docker\resources\bin\docker.exe"
    )
    
    foreach ($dp in $dockerPaths) {
        if (Test-Path $dp) {
            Write-Host "  Docker found at $dp" -ForegroundColor Green
            $dockerInstalled = $true
            $dockerBinDir = Split-Path $dp
            if ($env:PATH -notlike "*$dockerBinDir*") {
                $env:PATH = "$dockerBinDir;$env:PATH"
            }
            break
        }
    }
    
    if (-not $dockerInstalled) {
        $dockerUrl = "https://desktop.docker.com/win/main/amd64/Docker%20Desktop%20Installer.exe"
        $dockerInstaller = "$DownloadDir\DockerDesktopInstaller.exe"
        
        Write-Host "  Downloading Docker Desktop (large file, ~500MB)..." -ForegroundColor White
        Write-Host "  This will take several minutes..." -ForegroundColor White
        try {
            Invoke-WebRequest -Uri $dockerUrl -OutFile $dockerInstaller -UseBasicParsing
            Write-Host "  Downloaded. Starting installer..." -ForegroundColor White
            Write-Host "  NOTE: Docker installer will run interactively." -ForegroundColor Yellow
            Write-Host "  Accept defaults. A restart may be required." -ForegroundColor Yellow
            
            Start-Process $dockerInstaller -ArgumentList "install", "--accept-license", "--quiet" -Wait
            
            Write-Host "  Docker Desktop installed!" -ForegroundColor Green
            Write-Host "  You may need to restart your computer." -ForegroundColor Yellow
        } catch {
            Write-Host "  Download failed: $_" -ForegroundColor Red
            Write-Host "  Manual install: https://www.docker.com/products/docker-desktop/" -ForegroundColor Yellow
        }
    }
    Write-Host ""
}

# ============================================================
# STEP 5: Setup Project
# ============================================================
Write-Host "[5/5] Setting up project..." -ForegroundColor Yellow

# Create storage directories
$storageDirs = @(
    "$ProjectDir\storage\recordings",
    "$ProjectDir\storage\hls",
    "$ProjectDir\storage\exports",
    "$ProjectDir\storage\thumbnails"
)

foreach ($dir in $storageDirs) {
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
}
Write-Host "  Storage directories created" -ForegroundColor Green

# Install frontend dependencies if node is available
if (Get-Command node -ErrorAction SilentlyContinue) {
    Write-Host "  Installing frontend dependencies..." -ForegroundColor White
    Push-Location "$ProjectDir\frontend"
    & npm install --legacy-peer-deps 2>&1 | Out-Null
    Pop-Location
    Write-Host "  Frontend dependencies installed" -ForegroundColor Green
} else {
    Write-Host "  Skipping npm install (node not in PATH yet)" -ForegroundColor Yellow
}

Write-Host ""

# ============================================================
# SUMMARY
# ============================================================
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  Setup Summary" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan

# Re-check everything
$checks = @(
    @{ Name = "Node.js"; Test = { if (Test-Path "$NodePath\node.exe") { & "$NodePath\node.exe" --version } else { $null } } },
    @{ Name = "npm"; Test = { if (Test-Path "$NodePath\npm.cmd") { & "$NodePath\npm.cmd" --version 2>&1 } else { $null } } },
    @{ Name = "Go"; Test = { if (Test-Path "C:\Program Files\Go\bin\go.exe") { & "C:\Program Files\Go\bin\go.exe" version 2>&1 } else { $null } } },
    @{ Name = "FFmpeg"; Test = { if (Test-Path "C:\ffmpeg\bin\ffmpeg.exe") { "Installed at C:\ffmpeg" } else { $null } } },
    @{ Name = "Docker"; Test = { if (Test-Path "C:\Program Files\Docker\Docker\resources\bin\docker.exe") { "Installed" } else { $null } } }
)

foreach ($check in $checks) {
    $result = & $check.Test
    if ($result) {
        Write-Host "  [OK] $($check.Name): $result" -ForegroundColor Green
    } else {
        Write-Host "  [!!] $($check.Name): NOT INSTALLED" -ForegroundColor Red
    }
}

Write-Host ""
Write-Host "  IMPORTANT: Open a NEW terminal after installation" -ForegroundColor Yellow
Write-Host "  for PATH changes to take effect." -ForegroundColor Yellow
Write-Host ""
Write-Host "  Next steps:" -ForegroundColor Cyan
Write-Host "    1. Open a new terminal" -ForegroundColor White
Write-Host "    2. docker-compose up -d   (start database)" -ForegroundColor White  
Write-Host "    3. go mod tidy && go build -o bin\ironsight.exe ./cmd/server" -ForegroundColor White
Write-Host "    4. cd frontend && npm install && npm run dev" -ForegroundColor White
Write-Host "    5. Open http://localhost:3000" -ForegroundColor White
Write-Host ""

# Cleanup downloads
Write-Host "Cleanup download files? (y/n): " -ForegroundColor Yellow -NoNewline
$cleanup = Read-Host
if ($cleanup -eq "y") {
    Remove-Item $DownloadDir -Recurse -Force -ErrorAction SilentlyContinue
    Write-Host "  Downloads cleaned up" -ForegroundColor Green
}

Write-Host ""
Write-Host "Setup complete!" -ForegroundColor Green
Write-Host ""
