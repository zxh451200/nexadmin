@echo off
rem 一键推送官方插件 monorepo 到 GitHub（1 个仓库，含 10 个插件目录）。
rem 用法：
rem   scripts\push-plugins.cmd <你的GitHub账号或组织> [monorepo路径]
rem 例：
rem   scripts\push-plugins.cmd my-account
rem   scripts\push-plugins.cmd my-org D:\var\home\work\data\xxx\nexadmin-plugins
rem 前提：GitHub 上已建空仓库 <账号>/nexadmin-plugins（或用 gh repo create <账号>/nexadmin-plugins --public），
rem       本机已装好可用的 GitHub SSH key。统一走 ssh.github.com:443 —— 22 端口和 github.com:443
rem       在部分网络里是被拦的，走这条通道不需要额外代理。
setlocal
set ACCOUNT=%1
if "%ACCOUNT%"=="" ( echo 用法: %~nx0 ^<GitHub账号^> [monorepo路径，默认 D:\var\home\work\data\xxx\nexadmin-plugins] & exit /b 1 )
set BASE=%2
if "%BASE%"=="" set BASE=D:\var\home\work\data\xxx\nexadmin-plugins

git -C "%BASE%" remote remove origin >nul 2>&1
git -C "%BASE%" remote add origin ssh://git@ssh.github.com:443/%ACCOUNT%/nexadmin-plugins.git
git -C "%BASE%" push -u origin main
if errorlevel 1 ( echo 推送失败 —— 确认仓库已创建、本机 SSH key 已加到 GitHub。& exit /b 1 )
echo.
echo 推送完成。收尾两步：
echo   1. 把 config/plugin-market.yaml 里的 your-account 替换为 %ACCOUNT%
echo   2. go run ./cmd/nexadmin plugin market sync --docs nexadmin-vendor/docs-site
endlocal
