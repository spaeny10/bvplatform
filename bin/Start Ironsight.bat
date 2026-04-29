@echo off
REM -- Ironsight one-click start --
REM Brings up the WSL/Podman stack (db, mediamtx, api, worker, frontend, yolo, qwen).
REM
REM CDI cleanup: NVIDIA's WSL bootup hook re-creates /etc/cdi/nvidia.json
REM every time the VM starts, which conflicts with our generated nvidia.yaml
REM and causes "unresolvable CDI devices nvidia.com/gpu=all" on yolo/qwen.
REM We delete the .json before bringing up the stack. Done as the WSL root
REM user via "wsl -u root" so there's no sudo password prompt.

echo.
echo [1/3] Cleaning conflicting NVIDIA CDI spec...
REM /etc/wsl.conf [boot] runs nvidia-ctk on every WSL VM start and writes
REM /etc/cdi/nvidia.json -- which then conflicts with our nvidia.yaml and
REM yields "unresolvable CDI devices" on yolo/qwen. Delete the .json
REM (system + user scope) and sync the .yaml to the rootless user-scope
REM dir that podman actually reads from.
wsl -d NVIDIA-Workbench -u root -- bash -c "rm -f /etc/cdi/nvidia.json"
wsl -d NVIDIA-Workbench -u workbench -- bash -c "mkdir -p ~/.config/cdi && rm -f ~/.config/cdi/nvidia.json && cp -u /etc/cdi/nvidia.yaml ~/.config/cdi/nvidia.yaml && echo '   ok'"

echo.
echo [2/3] Starting Ironsight stack...
wsl -d NVIDIA-Workbench -u workbench -- bash -c ^
  "cd /mnt/c/Users/Shawn/Documents/Codebase/BV-Platform && export DOCKER_HOST=unix:///run/user/1000/podman/podman.sock && set -a && source .env && set +a && docker-compose up -d db mediamtx api worker frontend yolo qwen"

if %errorlevel% neq 0 (
    echo.
    echo Failed. Check WSL is running: wsl -l -v
    echo If yolo/qwen failed with "unresolvable CDI devices", regenerate the spec:
    echo    wsl -d NVIDIA-Workbench sudo nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml
    pause
    exit /b 1
)

echo.
echo [3/3] Stack is up. Status:
wsl -d NVIDIA-Workbench -u workbench -- bash -c ^
  "export DOCKER_HOST=unix:///run/user/1000/podman/podman.sock && cd /mnt/c/Users/Shawn/Documents/Codebase/BV-Platform && docker-compose ps --format 'table {{.Service}}\t{{.Status}}'"

echo.
echo Open http://localhost:3000  (login: admin / admin)
echo.
pause
