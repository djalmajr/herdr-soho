@echo off
setlocal
rem herdr-soho - prefer the installed Go binary and retain the JavaScript fallback.

rem The installed binary lives outside the skill tree: tell it where the skill is.
if not defined HERDR_SOHO_SKILL_DIR for %%I in ("%~dp0..") do set "HERDR_SOHO_SKILL_DIR=%%~fI"

if "%HERDR_SOHO_JS%"=="1" goto js

if not defined HERDR_SOHO_BIN goto path_binary
if not exist "%HERDR_SOHO_BIN%" goto invalid_override
for %%I in ("%HERDR_SOHO_BIN%") do if /I not "%%~xI"==".exe" goto invalid_override
"%HERDR_SOHO_BIN%" %*
exit /b %errorlevel%

:path_binary
for /f "delims=" %%B in ('where herdr-soho.exe 2^>nul') do (
  set "HERDR_SOHO_PATH_BIN=%%B"
  goto found_path_binary
)
goto js

:found_path_binary
"%HERDR_SOHO_PATH_BIN%" %*
exit /b %errorlevel%

:js
node -e "process.exit(Number(process.versions.node.split('.')[0]) >= 20 ? 0 : 1)" >nul 2>nul
if errorlevel 1 goto try_bun
node "%~dp0herdr-soho.mjs" %*
exit /b %errorlevel%

:try_bun
where bun >nul 2>nul
if errorlevel 1 goto no_runtime
bun "%~dp0herdr-soho.mjs" %*
exit /b %errorlevel%

:invalid_override
echo herdr-soho: HERDR_SOHO_BIN is missing or not executable: %HERDR_SOHO_BIN% 1>&2
exit /b 2

:no_runtime
echo herdr-soho: install the herdr-soho binary (install.sh / install.ps1 from the GitHub releases) or Node.js 20+ / Bun 1>&2
exit /b 2
