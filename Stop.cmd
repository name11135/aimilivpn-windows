@echo off
cd /d "%~dp0"
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Stop.ps1"
if errorlevel 1 (
  echo.
  echo AimiliVPN failed to stop. Check logs\service.log
  pause
)

