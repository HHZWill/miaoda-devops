@echo off
setlocal

rem Run the launcher without changing the system execution policy.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0start.ps1"
set "START_EXIT_CODE=%ERRORLEVEL%"

rem Keep startup errors visible when launched by double-click.
if not "%START_EXIT_CODE%"=="0" pause
exit /b %START_EXIT_CODE%
