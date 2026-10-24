@echo off
rem 生产构建（Windows）：npm ci && npm run build -> go build（嵌入前端产物）-> bin\nexadmin-server.exe。不依赖 make。
setlocal
cd /d "%~dp0\.."

set VERSION=
for /f "usebackq delims=" %%v in (`powershell -NoProfile -Command "(Get-Content .nexadmin.manifest.json -Raw | ConvertFrom-Json).framework_version"`) do set VERSION=%%v
if "%VERSION%"=="" (
  echo 读不到 .nexadmin.manifest.json 的 framework_version 1>&2
  exit /b 1
)

echo ==^> 构建前端
pushd web
call npm ci || (popd & exit /b 1)
call npm run build || (popd & exit /b 1)
popd

echo ==^> 构建后端 %VERSION%
if not exist bin mkdir bin
go build -trimpath -tags embedweb -ldflags "-s -w -X main.version=%VERSION%" -o bin\nexadmin-server.exe .\cmd\server || exit /b 1
echo 完成: bin\nexadmin-server.exe
