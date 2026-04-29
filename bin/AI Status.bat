@echo off
REM ── Quick AI services status ──

echo === Container state ===
wsl -d NVIDIA-Workbench -u workbench -- podman ps -a --filter name=yolo --filter name=qwen --format "table {{.Names}}\t{{.Status}}"

echo.
echo === YOLO health ===
wsl -d NVIDIA-Workbench -u workbench -- podman exec ironsight-api wget -qO- --timeout=5 http://yolo:8501/health 2>nul
if %errorlevel% neq 0 echo   (unreachable — service not up or api container down)

echo.
echo === Qwen health ===
wsl -d NVIDIA-Workbench -u workbench -- podman exec ironsight-api wget -qO- --timeout=5 http://qwen:8502/health 2>nul
if %errorlevel% neq 0 echo   (unreachable — service not up, api container down, or Qwen still loading 5GB of weights)

echo.
echo === Recent AI activity in api log ===
wsl -d NVIDIA-Workbench -u workbench -- bash -c "podman logs --tail 200 ironsight-api 2>^&1 | grep -E '\[AI\]|AI Pipeline|AI_ENABLED' | tail -10"

echo.
pause
