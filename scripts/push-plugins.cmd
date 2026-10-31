@echo off
rem Push the official plugin monorepo to GitHub (one repo, 10 plugin dirs).
rem Usage: scripts\push-plugins.cmd <github-account-or-org> [monorepo-path]
rem
rem ASCII only on purpose: cmd.exe parses .cmd with the system code page, so
rem UTF-8 Chinese comments get split and executed as commands.
rem Uses SSH over port 443 (ssh.github.com): ports 22 and github.com:443 are
rem blocked on some networks, this transport avoids extra proxies.
setlocal
set ACCOUNT=%1
if "%ACCOUNT%"=="" ( echo Usage: %~nx0 ^<github-account^> [monorepo-path] & exit /b 1 )
set BASE=%2
if "%BASE%"=="" set BASE=D:\var\home\work\data\xxx\nexadmin-plugins

git -C "%BASE%" remote remove origin >nul 2>&1
git -C "%BASE%" remote add origin ssh://git@ssh.github.com:443/%ACCOUNT%/nexadmin-plugins.git
git -C "%BASE%" push -u origin main
if errorlevel 1 ( echo Push failed - check the repo exists and your SSH key is added to GitHub. & exit /b 1 )
echo.
echo Done. Two follow-up steps:
echo   1. set repo fields in config/plugin-market.yaml to %ACCOUNT%/nexadmin-plugins
echo   2. go run ./cmd/nexadmin plugin market sync --docs nexadmin-vendor/docs-site
endlocal
