#!/bin/sh
# NexAdmin 服务管理（Linux 薄壳）：没有终端就开一个，调用 nexadmin-server，结束时暂停。逻辑都在二进制里。
DIR=$(cd "$(dirname "$0")" && pwd)

# 双击运行时 stdin 不是终端：换成终端窗口重新运行自己
if [ ! -t 0 ]; then
	for t in x-terminal-emulator gnome-terminal xterm; do
		command -v "$t" >/dev/null 2>&1 || continue
		if [ "$t" = gnome-terminal ]; then exec "$t" -- "$0" "$@"; fi
		exec "$t" -e "$0" "$@"
	done
	echo "没有找到终端程序（x-terminal-emulator / gnome-terminal / xterm），请在终端里运行: $0" >&2
	exit 1
fi

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

srv() { if [ -n "$RUN" ]; then "$RUN" "$@"; else go run ./cmd/server "$@"; fi; }

echo "========== NexAdmin 服务管理 =========="
echo "  1 启动   2 停止   3 重启   4 状态"
printf '请选择 [1-4]: '
read -r choice
case "$choice" in
	1) srv start ;;
	2) srv stop ;;
	3) srv restart ;;
	4) srv status ;;
	*) echo "无效选择" ;;
esac
pause
