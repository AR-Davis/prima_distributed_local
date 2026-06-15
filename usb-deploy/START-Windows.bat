@echo off
:: Prima Distributed Local - Windows Launcher

cd /d "%~dp0"

echo 🔌 Starting Prima Distributed Local...
echo.

bin\prima-installer-windows.exe tui

if errorlevel 1 (
    echo.
    echo ❌ Error starting Prima. Press any key to exit...
    pause > nul
)
