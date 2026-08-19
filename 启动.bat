@echo off
chcp 65001 >nul
setlocal
cd /d "%~dp0"

echo ================================
echo   Agent Loop Demo - Start
echo ================================
echo.

rem ---------- 1. Node: system node or bundled .tools/node; auto-download if missing ----------
set "NODE_CMD="
for /f "delims=" %%i in ('where node 2^>nul') do set "NODE_CMD=%%i"
if defined NODE_CMD goto :have_node
if exist ".tools\node\node.exe" set "NODE_CMD=%~dp0.tools\node\node.exe"
if defined NODE_CMD goto :have_node
echo [..] node not found. Downloading Node LTS (v22.14.0 win-x64)...
powershell -NoProfile -ExecutionPolicy Bypass -Command "$ErrorActionPreference='Stop';$ProgressPreference='SilentlyContinue';$td=Join-Path (Get-Location) '.tools';$v='v22.14.0';$en='node-'+$v+'-win-x64';$ed=Join-Path $td $en;$nd=Join-Path $td 'node';$z=Join-Path $env:TEMP ($en+'.zip');if(Test-Path $ed){Remove-Item $ed -Recurse -Force -ErrorAction SilentlyContinue};Invoke-WebRequest -Uri ('https://nodejs.org/dist/'+$v+'/'+$en+'.zip') -OutFile $z;Expand-Archive -Path $z -DestinationPath $td -Force;Remove-Item $z -Force -ErrorAction SilentlyContinue;if(Test-Path $nd){Remove-Item $nd -Recurse -Force -ErrorAction SilentlyContinue};Move-Item $ed $nd -Force -ErrorAction SilentlyContinue"
if exist ".tools\node\node.exe" set "NODE_CMD=%~dp0.tools\node\node.exe"
if not defined NODE_CMD if exist ".tools\node-v22.14.0-win-x64\node.exe" set "NODE_CMD=%~dp0.tools\node-v22.14.0-win-x64\node.exe"
if not defined NODE_CMD goto :fail_node
:have_node
echo [OK] node: %NODE_CMD%

rem ---------- 2. pnpm: bundled pnpm.mjs preferred; fall back to npm (bundled) ----------
set "NODE_DIR="
for /f "delims=" %%i in ("%NODE_CMD%") do set "NODE_DIR=%%~dpi"
rem %%~dpi ends with a backslash; strip it so quoted args never see a
rem trailing backslash (which would escape the closing quote, e.g. "node\" pnpm)
if defined NODE_DIR if "%NODE_DIR:~-1%"=="\" set "NODE_DIR=%NODE_DIR:~0,-1%"
set "PNPM_CMD="
for /f "delims=" %%i in ('where pnpm 2^>nul') do if not defined PNPM_CMD if /i "%%~xi"==".cmd" set "PNPM_CMD=%%i"
if defined PNPM_CMD goto :have_pnpm
set "PNPM_MJS=%NODE_DIR%\node_modules\pnpm\bin\pnpm.mjs"
set "NPM_CLI=%NODE_DIR%\node_modules\npm\bin\npm-cli.js"
if exist "%PNPM_MJS%" goto :have_pnpm
echo [..] pnpm missing; installing via bundled npm...
if exist "%NPM_CLI%" (
    "%NODE_CMD%" "%NPM_CLI%" install --prefix "%NODE_DIR%" pnpm
)
if exist "%PNPM_MJS%" goto :have_pnpm
echo [WARN] pnpm install failed (network?); dev.mjs requires pnpm
:have_pnpm
if defined PNPM_CMD (echo [OK] pnpm: %PNPM_CMD%) else if exist "%PNPM_MJS%" (echo [OK] pnpm: %PNPM_MJS%) else (echo [WARN] pnpm unavailable)

rem ---------- 3. PATH: add node dir (scripts call bare "node") ----------
set "PATH=%NODE_DIR%;%PATH%"

rem ---------- 4. Python venv: create if missing ----------
if exist ".tools\venv\Scripts\python.exe" goto :have_python
echo [..] Python venv missing, provisioning with setup:python...
if exist "%PNPM_MJS%" goto :setup_py_pnpm
if exist "%NPM_CLI%" goto :setup_py_npm
goto :have_python
:setup_py_pnpm
if defined PNPM_CMD ("%PNPM_CMD%" setup:python) else ("%NODE_CMD%" "%PNPM_MJS%" setup:python)
goto :have_python
:setup_py_npm
"%NODE_CMD%" "%NPM_CLI%" run setup:python
:have_python
if exist ".tools\venv\Scripts\python.exe" echo [OK] python: %~dp0.tools\venv\Scripts\python.exe

rem ---------- 5. Start dev: agent auto-builds, frontend deps auto-install, API+vite ----------
echo.
echo ===================================
echo  Services starting!
echo    API:      http://localhost:8081
echo    Frontend: http://localhost:5173
echo ===================================
echo  Close this window to stop.
echo.
if exist "%PNPM_MJS%" goto :run_dev_pnpm
if exist "%NPM_CLI%" goto :run_dev_npm
goto :fail_no_pkg

:run_dev_pnpm
if defined PNPM_CMD ("%PNPM_CMD%" dev) else ("%NODE_CMD%" "%PNPM_MJS%" dev)
goto :cleanup
:run_dev_npm
"%NODE_CMD%" "%NPM_CLI%" run dev
goto :cleanup

:cleanup
echo.
echo Stopping services...
taskkill /F /IM node.exe >nul 2>&1
echo Done.
pause
exit /b 0

:fail_node
echo [ERROR] cannot obtain node.exe
pause
exit /b 1

:fail_no_pkg
echo [ERROR] neither pnpm nor npm available under %NODE_DIR%
pause
exit /b 1