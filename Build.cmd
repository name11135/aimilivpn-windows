@echo off
setlocal
cd /d "%~dp0"
set "GOEXE=E:\go125\go\bin\go.exe"
if not exist "%GOEXE%" set "GOEXE=go"
"%GOEXE%" fmt .
if errorlevel 1 exit /b 1
"%GOEXE%" test ./...
if errorlevel 1 exit /b 1
"%GOEXE%" build -trimpath -ldflags="-s -w" -o aimilivpn.exe .
if errorlevel 1 (pause & exit /b 1)
echo Build complete.

