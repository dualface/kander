@echo off
setlocal
rem Windows build script mirroring the Makefile build target. Run it to produce kander.exe.

cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
    echo error: go toolchain not found in PATH 1>&2
    exit /b 1
)

for /f "usebackq delims=" %%i in (`powershell -NoProfile -Command "(Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssZ')"`) do set "BUILD_TIMESTAMP=%%i"
if not defined BUILD_TIMESTAMP set "BUILD_TIMESTAMP=unknown"

set "GIT_HASH=unknown"
for /f "usebackq delims=" %%i in (`git rev-parse --short^=12 HEAD 2^>nul`) do set "GIT_HASH=%%i"

set "VERSION_PACKAGE=github.com/dualface/kander/internal/version"

go build -ldflags "-X %VERSION_PACKAGE%.BuildTimestamp=%BUILD_TIMESTAMP% -X %VERSION_PACKAGE%.GitHash=%GIT_HASH%" -o kander.exe ./cmd/kander
if errorlevel 1 exit /b 1

echo built kander.exe %BUILD_TIMESTAMP%-%GIT_HASH%
