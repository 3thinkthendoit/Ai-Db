@echo off
setlocal EnableExtensions EnableDelayedExpansion
title AI-DB Pack

REM ============================================================
REM  AI-DB (Go + Wails v2 + React) build script
REM
REM  Usage:
REM    pack.bat            normal build (adds -s -w to strip symbols)
REM    pack.bat clean      clean old output, then rebuild
REM    pack.bat nsis       build and generate NSIS installer
REM    pack.bat nofe       skip frontend build (Go-only changes)
REM    pack.bat embed      embed WebView2 bootstrapper
REM    pack.bat dist       copy the exe to the project root
REM    pack.bat install    install dependencies only, no build
REM    pack.bat help       show this help
REM
REM  Options can be combined, e.g.: pack.bat clean nsis dist
REM
REM  Keep this file ASCII-only. Non-ASCII text breaks the cmd
REM  batch parser (line offset errors) under some code pages.
REM ============================================================

set "ROOT=%~dp0"
pushd "%ROOT%"

set "START_TIME=%TIME%"
set "OPT_CLEAN="
set "OPT_NSIS="
set "OPT_NOFE="
set "OPT_EMBED="
set "OPT_DIST="
set "OPT_INSTALL="
set "BUILD_OPT="
set "WAILS_VER="
set "GO_VER="
set "NODE_VER="

for %%A in (%*) do (
    set "ARG=%%~A"
    if /i "!ARG!"=="clean"   set "OPT_CLEAN=1"
    if /i "!ARG!"=="nsis"    set "OPT_NSIS=1"
    if /i "!ARG!"=="nofe"    set "OPT_NOFE=1"
    if /i "!ARG!"=="embed"   set "OPT_EMBED=1"
    if /i "!ARG!"=="dist"    set "OPT_DIST=1"
    if /i "!ARG!"=="install" set "OPT_INSTALL=1"
    if /i "!ARG!"=="help"    goto :usage
    if /i "!ARG!"=="-h"      goto :usage
    if /i "!ARG!"=="/?"      goto :usage
)

if defined OPT_CLEAN  set "BUILD_OPT=!BUILD_OPT! -clean"
if defined OPT_NSIS   set "BUILD_OPT=!BUILD_OPT! -nsis"
if defined OPT_NOFE   set "BUILD_OPT=!BUILD_OPT! -s"
if defined OPT_EMBED  set "BUILD_OPT=!BUILD_OPT! -webview2 embed"

echo ============================================================
echo  AI-DB pack
echo  Root: %ROOT%
echo ============================================================

REM ---------- 1. environment ----------
echo.
echo [1/4] Checking build environment ...

where wails >nul 2>nul
if errorlevel 1 (
    echo   [FAIL] wails CLI not found
    echo   Install: go install github.com/wailsapp/wails/v2/cmd/wails@latest
    goto :fail
)
for /f "delims=" %%V in ('wails version 2^>nul') do if not defined WAILS_VER set "WAILS_VER=%%V"

where go >nul 2>nul
if errorlevel 1 (
    echo   [FAIL] go not found, install Go 1.22 or newer
    goto :fail
)
for /f "delims=" %%V in ('go version 2^>nul') do set "GO_VER=%%V"

where node >nul 2>nul
if errorlevel 1 (
    echo   [FAIL] node not found, install Node.js 18 or newer
    goto :fail
)
for /f "delims=" %%V in ('node -v 2^>nul') do set "NODE_VER=%%V"

where npm >nul 2>nul
if errorlevel 1 (
    echo   [FAIL] npm not found
    goto :fail
)

echo   wails : !WAILS_VER!
echo   go    : !GO_VER!
echo   node  : !NODE_VER!

if defined OPT_NSIS (
    where makensis >nul 2>nul
    if errorlevel 1 (
        echo   [WARN] makensis not found, the NSIS installer may fail
        echo          Download: https://nsis.sourceforge.io
    )
)

REM ---------- 2. dependencies ----------
echo.
echo [2/4] Installing frontend dependencies ...

pushd "%ROOT%frontend"
call npm install
set "NPM_ERR=!errorlevel!"
popd
if not "!NPM_ERR!"=="0" (
    echo   [FAIL] npm install exited with code !NPM_ERR!
    goto :fail
)
echo   frontend dependencies ready

if defined OPT_INSTALL (
    echo.
    echo [3/4] Downloading Go modules ...
    go mod download
    if errorlevel 1 goto :fail
    echo   dependencies installed, skipping build as requested
    goto :done
)

REM ---------- 3. build ----------
echo.
echo [3/4] Running: wails build -ldflags "-s -w"!BUILD_OPT!
echo.

wails build -ldflags "-s -w" !BUILD_OPT!
if errorlevel 1 (
    echo.
    echo   [FAIL] wails build did not succeed, see the output above
    goto :fail
)

REM ---------- 4. output ----------
echo.
echo [4/4] Verifying output ...

set "EXE=%ROOT%build\bin\AI-DB.exe"
if not exist "!EXE!" (
    echo   [FAIL] output not found: !EXE!
    goto :fail
)
for %%F in ("!EXE!") do echo   file : %%~fF
for %%F in ("!EXE!") do echo   size : %%~zF bytes
for %%F in ("!EXE!") do echo   time : %%~tF

if defined OPT_NSIS (
    if exist "%ROOT%build\bin\AI-DB-amd64-installer.exe" (
        echo   installer : %ROOT%build\bin\AI-DB-amd64-installer.exe
    ) else (
        echo   [WARN] installer not found under build\bin, check NSIS
    )
)

if defined OPT_DIST (
    echo.
    copy /y "!EXE!" "%ROOT%AI-DB.exe" >nul
    if errorlevel 1 (
        echo   [WARN] copy to project root failed, the file may be running
    ) else (
        echo   copied to: %ROOT%AI-DB.exe
    )
)

goto :done

:usage
echo.
echo AI-DB pack script
echo.
echo   pack.bat            normal build
echo   pack.bat clean      clean old output, then rebuild
echo   pack.bat nsis       build and generate NSIS installer
echo   pack.bat nofe       skip frontend build
echo   pack.bat embed      embed WebView2 bootstrapper
echo   pack.bat dist       copy the exe to the project root
echo   pack.bat install    install dependencies only
echo.
echo Options can be combined: pack.bat clean nsis dist
echo.
echo Output: build\bin\AI-DB.exe
goto :end

:fail
echo.
echo ============================================================
echo  BUILD FAILED
echo ============================================================
popd
endlocal
exit /b 1

:done
echo.
echo ============================================================
echo  BUILD OK
echo  start: %START_TIME%    end: %TIME%
echo ============================================================
popd
endlocal
exit /b 0

:end
popd
endlocal
exit /b 0
