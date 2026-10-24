@echo off
rem NexAdmin 服务管理（Windows 薄壳）：双击 .bat 时 cmd 窗口本来就会打开，所以不需要另开终端。
rem 调用 nexadmin-server.exe，结束时暂停。逻辑都在二进制里。
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

echo ========== NexAdmin 服务管理 ==========
echo   1 启动   2 停止   3 重启   4 状态
set "CHOICE="
set /p CHOICE=请选择 [1-4]: 
if "%CHOICE%"=="1" (%RUN% start) else if "%CHOICE%"=="2" (%RUN% stop) else if "%CHOICE%"=="3" (%RUN% restart) else if "%CHOICE%"=="4" (%RUN% status) else (echo 无效选择)

:end
echo.
pause
endlocal
