@echo off
setlocal
rem Windows build script mirroring the Makefile build target. Run it to produce kander.exe.

cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
    echo error: go toolchain not found in PATH 1>&2
    exit /b 1
)

if not defined VERSION (
    set "VERSION=dev"
    git describe --tags >nul 2>nul
    if not errorlevel 1 (
        for /f "usebackq delims=" %%i in (`git describe --tags --always 2^>nul`) do set "VERSION=%%i"
    )
)

set "VERSION_PACKAGE=github.com/dualface/kander/internal/version"

go build -ldflags "-X %VERSION_PACKAGE%.Version=%VERSION%" -o kander.exe ./cmd/kander
if errorlevel 1 exit /b 1

echo built kander.exe %VERSION%
