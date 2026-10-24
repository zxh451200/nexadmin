#!/bin/sh
# NexAdmin 恢复出厂（macOS 薄壳）：双击 .command 时 Terminal 本来就会打开，所以不需要另开终端。
# 调用 nexadmin-server reinit，结束时暂停。清单、确认、备份、清库等逻辑都在二进制里。可附加参数，例如 --dry-run。
DIR=$(cd "$(dirname "$0")" && pwd)

pause() { printf '\n按回车键关闭...'; read -r _; }

# 二进制在脚本同目录或项目根；都没有时开发环境回退 go run
RUN=""
if [ -x "$DIR/nexadmin-server" ]; then
	ROOT=$DIR; RUN="$DIR/nexadmin-server"
elif [ -x "$DIR/../nexadmin-server" ]; then
	ROOT=$(cd "$DIR/.." && pwd); RUN="$ROOT/nexadmin-server"
elif [ -f "$DIR/../go.mod" ] && command -v go >/dev/null 2>&1; then
	ROOT=$(cd "$DIR/.." && pwd)
else
	echo "找不到 nexadmin-server，请先构建: go build -o nexadmin-server ./cmd/server"
	pause; exit 1
fi
cd "$ROOT" || exit 1

if [ -n "$RUN" ]; then "$RUN" reinit "$@"; else go run ./cmd/server reinit "$@"; fi
pause
