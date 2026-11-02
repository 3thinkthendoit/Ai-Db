@echo off
setlocal EnableExtensions
title AI-DB Frontend (Vite)

REM Start Vite dev server. Proxies /api to http://localhost:9900
REM (same as .vscode/launch.json AIDB_ADDR). Start the Go server first (F5).

set "ROOT=%~dp0"
pushd "%ROOT%frontend" || (
  echo [ERR] frontend\ not found
  exit /b 1
)

where npm >nul 2>&1 || (
  echo [ERR] npm not found. Install Node.js and retry.
  popd
  exit /b 1
)

if not exist "node_modules\" (
  echo [..] npm install
  call npm install
  if errorlevel 1 (
    echo [ERR] npm install failed
    popd
    exit /b 1
  )
)

echo [..] npm run dev
echo     Open the URL Vite prints (usually http://localhost:5172)
echo     API proxy -^> http://localhost:9900
call npm run dev
set "EC=%ERRORLEVEL%"
popd
exit /b %EC%
