@echo off
setlocal enabledelayedexpansion

set PREFIX=stylle
if not "%IMAGE_PREFIX%"=="" set PREFIX=%IMAGE_PREFIX%
for /f %%i in ('git rev-parse --short HEAD') do set VERSION=%%i
if "%VERSION%"=="" goto :fail
if "%PLATFORMS%"=="" set PLATFORMS=linux/amd64,linux/arm64
set IMAGE=%PREFIX%/today-random-movie-web

echo Building %IMAGE%:%VERSION% for %PLATFORMS%
docker buildx build --platform %PLATFORMS% -t %IMAGE%:%VERSION% -t %IMAGE%:latest --provenance=mode=max --sbom=true --push . || goto :fail
echo Published %IMAGE%:%VERSION% and %IMAGE%:latest
exit /b 0

:fail
echo BUILD/PUSH FAILED
exit /b 1
