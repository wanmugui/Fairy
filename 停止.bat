@echo off
chcp 65001 >nul
echo ================================
echo   AgentLoop - Stop
echo ================================
echo.
echo Stopping API + Frontend...
taskkill /F /IM node.exe >nul 2>&1
echo Done.
timeout /t 2 /nobreak >nul