@echo off
rem NexAdmin 恢复出厂（Windows 薄壳）：双击 .bat 时 cmd 窗口本来就会打开，所以不需要另开终端。
rem 调用 nexadmin-server.exe reinit，结束时暂停。清单、确认、备份、清库等逻辑都在二进制里。可附加参数，例如 --dry-run。
setlocal
chcp 65001 >nul
set "DIR=%~dp0"

rem 二进制在脚本同目录或项目根；都没有时开发环境回退 go run
set "RUN="
if exist "%DIR%nexadmin-server.exe" (
  set "ROOT=%DIR%"
  set RUN="%DIR%nexadmin-server.exe"
) else if exist "%DIR%..\nexadmin-server.exe" (
  set "ROOT=%DIR%.."
  set RUN="%DIR%..\nexadmin-server.exe"
) else if exist "%DIR%..\go.mod" (
  set "ROOT=%DIR%.."
  set "RUN=go run ./cmd/server"
) else (
  echo 找不到 nexadmin-server.exe，请先构建: go build -o nexadmin-server.exe ./cmd/server
  goto :end
)
cd /d "%ROOT%"

%RUN% reinit %*

:end
echo.
pause
endlocal
