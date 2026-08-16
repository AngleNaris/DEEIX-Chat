@echo off
rem Temporary dev shim: delegates `go tool swag init` to the deeix-build container
rem because the host has no Go toolchain. Replaced by the real `go.exe` on CI.
setlocal
set OUT=
for %%I in ("%~dp0..\..\..\backend") do set "BACKEND=%%~fI"
:parse
if "%~1"=="" goto run
if "%~1"=="-o" (
  set OUT=%~2
  shift
)
shift
goto parse
:run
if "%OUT%"=="" (
  echo go-tool: missing -o argument 1>&2
  exit /b 127
)
docker run --rm -v deeix-gomod:/go/pkg/mod -v "%BACKEND%:/app" -v "%OUT%:/host-out" -w /app deeix-build:1 sh -c "go tool swag init -g cmd/server/main.go -o /host-out --packageName docs --parseDependency --parseInternal --requiredByDefault --quiet"
exit /b %ERRORLEVEL%
