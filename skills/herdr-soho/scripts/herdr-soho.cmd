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
goto install_dir

rem A shell opened before the install keeps a PATH without the binary:
rem try HERDR_SOHO_INSTALL_DIR, then the install.ps1 default. Top-level
rem lines (no parenthesized block) so %errorlevel% is the binary's.
:install_dir
if not defined HERDR_SOHO_INSTALL_DIR goto default_install_dir
if not exist "%HERDR_SOHO_INSTALL_DIR%\herdr-soho.exe" goto default_install_dir
"%HERDR_SOHO_INSTALL_DIR%\herdr-soho.exe" %*
exit /b %errorlevel%

:default_install_dir
if not defined LOCALAPPDATA goto js
if not exist "%LOCALAPPDATA%\Programs\herdr-soho\herdr-soho.exe" goto js
"%LOCALAPPDATA%\Programs\herdr-soho\herdr-soho.exe" %*
exit /b %errorlevel%
goto js

:found_path_binary
"%HERDR_SOHO_PATH_BIN%" %*
exit /b %errorlevel%

rem One warning before the JavaScript fallback, only when it is a fallback:
rem HERDR_SOHO_JS=1 asked for the JavaScript on purpose.
:js
node -e "process.exit(Number(process.versions.node.split('.')[0]) >= 20 ? 0 : 1)" >nul 2>nul
if errorlevel 1 goto try_bun
call :warn_js_fallback
node "%~dp0herdr-soho.mjs" %*
exit /b %errorlevel%

:try_bun
where bun >nul 2>nul
if errorlevel 1 goto no_runtime
call :warn_js_fallback
bun "%~dp0herdr-soho.mjs" %*
exit /b %errorlevel%

:warn_js_fallback
if "%HERDR_SOHO_JS%"=="1" exit /b
echo herdr-soho: warning: the herdr-soho binary was not found (PATH or the install directory); running the JavaScript fallback, which lags the binary; reopen the shell after installing, or set HERDR_SOHO_BIN 1>&2
exit /b

:invalid_override
echo herdr-soho: HERDR_SOHO_BIN is missing or not executable: %HERDR_SOHO_BIN% 1>&2
exit /b 2

:no_runtime
echo herdr-soho: install the herdr-soho binary (install.sh / install.ps1 from the GitHub releases) or Node.js 20+ / Bun 1>&2
exit /b 2
