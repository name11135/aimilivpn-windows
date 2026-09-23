@echo off
cd /d "%~dp0"
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Start.ps1"
if errorlevel 1 (
  echo.
  echo AimiliVPN failed to start. Check logs\service.log
  pause
)

