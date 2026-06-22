@echo off
REM mycelium — The Mycelium Network Access Point (Windows)
REM Usage: mycelium [api|node|full|status|probe|shell|stop|help]

setlocal

set "MYCELIUM_PORT=%MYCELIUM_PORT:~0,11435%"
if "%MYCELIUM_PORT%"=="" set MYCELIUM_PORT=11435
if "%MYCELIUM_HOST%"=="" set MYCELIUM_HOST=0.0.0.0
if "%MYCELIUM_RPC_PORT%"=="" set MYCELIUM_RPC_PORT=50052
if "%MYCELIUM_RPC_HOST%"=="" set MYCELIUM_RPC_HOST=0.0.0.0

REM Find the binary
if defined MYCELIUM_BIN (
    set "BIN=%MYCELIUM_BIN%"
) else (
    set "BIN=%~dp0mycelium-api.exe"
    if not exist "%BIN%" set "BIN=mycelium-api.exe"
)

if "%1"=="stop" (
    taskkill /F /IM mycelium-api.exe 2>nul
    echo Stopped Mycelium.
    exit /b 0
)

if "%1"=="status" (
    curl -s http://localhost:%MYCELIUM_PORT%/api/status
    exit /b 0
)

if "%1"=="probe" (
    curl -s http://localhost:%MYCELIUM_PORT%/api/rpc/probe
    exit /b 0
)

if "%1"=="" (
    REM Default: start full node
    echo.
echo   Mycelium Network - Access Point
echo   API: http://%MYCELIUM_HOST%:%MYCELIUM_PORT%
echo.
"%BIN%" -port %MYCELIUM_PORT% -host %MYCELIUM_HOST%
    exit /b 0
)

if "%1"=="api" (
    "%BIN%" -port %MYCELIUM_PORT% -host %MYCELIUM_HOST%
    exit /b 0
)

if "%1"=="help" goto :help
if "%1"=="--help" goto :help
if "%1"=="-h" goto :help

echo Unknown command: %1
:help
echo.
echo   mycelium - The Mycelium Network Access Point
echo.
echo   Usage: mycelium [api^|node^|full^|status^|probe^|shell^|stop^|help]
echo.
echo   Commands:
echo     (none)    Start the API gateway
echo     api        Start the API gateway only
echo     status     Check network status
echo     probe      Probe RPC nodes
echo     stop       Stop the network
echo     help       Show this help
echo.
exit /b 0
