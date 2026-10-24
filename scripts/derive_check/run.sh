#!/usr/bin/env bash
# 派生验证 lane（仅框架仓库，不下发；nexadmin new 与源码包都排除本目录）。设计文档 §11。
# 步骤：nexadmin new -> 后端 build/vet -> 前端 npm ci && build -> 起服务探 /healthz 与 /
#       -> nexadmin add module -> nexadmin upgrade --from <当前源码> -> 再编译冒烟 -> 断言 internal/ 零 diff。
# 环境变量：
#   WORK_DIR          工作目录（默认 mktemp，结束删除；显式指定则保留）
#   PORT              冒烟端口（默认 18080）
#   INIT_CONFIG       非交互初始化文件（JSON/YAML，同向导请求体）；设置后先 `init --config` 再起服务，
#                     不设置则以未初始化模式起服务（/healthz 仍应为 200）
#   SKIP_FRONTEND=1   跳过前端 npm ci/build（离线或只验后端时用）
set -euo pipefail

SRC=$(cd "$(dirname "$0")/../.." && pwd)
PORT=${PORT:-18080}
NAME=demo-product
if [ -n "${WORK_DIR:-}" ]; then
	mkdir -p "$WORK_DIR"
	KEEP=1
else
	WORK_DIR=$(mktemp -d)
	KEEP=0
fi
DEMO="$WORK_DIR/$NAME"
CLI="$WORK_DIR/nexadmin-cli"
SRV_PID=""

stop_server() {
	if [ -n "$SRV_PID" ]; then
		kill "$SRV_PID" 2>/dev/null || true
		wait "$SRV_PID" 2>/dev/null || true
		SRV_PID=""
	fi
}
cleanup() {
	stop_server
	[ "$KEEP" = 1 ] || rm -rf "$WORK_DIR"
}
trap cleanup EXIT
step() { printf '\n==> %s\n' "$*"; }
# CI 机器没有 git 身份：nexadmin new --git 与后续提交都靠这组环境变量
export GIT_AUTHOR_NAME=derive-check GIT_AUTHOR_EMAIL=derive-check@example.invalid
export GIT_COMMITTER_NAME=derive-check GIT_COMMITTER_EMAIL=derive-check@example.invalid
fail() { echo "FAIL: $*" >&2; exit 1; }

# 编译 demo 的服务端并探活：/healthz 必须 200；带 embedweb 且前端已构建时 / 必须返回 HTML。
smoke() {
	local tag=$1 label=$2
	(cd "$DEMO" && go build -tags "$tag" -o "$WORK_DIR/server" ./cmd/server)
	if [ -n "${INIT_CONFIG:-}" ] && [ ! -f "$DEMO/data/.initialized" ]; then
		(cd "$DEMO" && NEXADMIN_SERVER_PORT=$PORT "$WORK_DIR/server" init --config "$INIT_CONFIG")
	fi
	(cd "$DEMO" && NEXADMIN_SERVER_PORT=$PORT exec "$WORK_DIR/server") >"$WORK_DIR/server.log" 2>&1 &
	SRV_PID=$!
	local ok=0
	for _ in $(seq 1 60); do
		if curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then ok=1; break; fi
		kill -0 "$SRV_PID" 2>/dev/null || break
		sleep 0.5
	done
	if [ "$ok" != 1 ]; then
		sed 's/^/  server.log| /' "$WORK_DIR/server.log" | tail -n 40 >&2
		fail "$label: /healthz 未在 30s 内返回 200"
	fi
	if [ "$tag" = embedweb ]; then
		curl -fsS "http://127.0.0.1:$PORT/" | grep -qi '<html' || fail "$label: / 没有返回 index.html"
	fi
	echo "  $label: /healthz 200 OK"
	stop_server
}

step "编译 nexadmin CLI 并派生 $NAME"
(cd "$SRC" && go build -o "$CLI" ./cmd/nexadmin)
(cd "$SRC" && "$CLI" new "$NAME" --out "$DEMO" --git)
for p in scripts/derive_check .github/workflows/derive.yml; do
	[ ! -e "$DEMO/$p" ] || fail "派生项目不应包含 $p"
done

step "后端 go build / go vet"
(cd "$DEMO" && go build ./... && go vet ./...)

TAG=""
if [ "${SKIP_FRONTEND:-0}" != 1 ]; then
	step "前端 npm ci && npm run build"
	(cd "$DEMO/web" && npm ci && npm run build)
	TAG=embedweb
fi

step "冒烟（起服务探 /healthz）"
smoke "$TAG" "派生后"

step "升级回环：add module -> upgrade --from 当前源码"
(cd "$DEMO" && "$CLI" add module article --fields "title:string,status:int")
(cd "$DEMO" && git add -A && git commit -q -m "add module article")
[ -d "$DEMO/internal/modules/article" ] || fail "add module 没有生成 internal/modules/article"
(cd "$DEMO" && "$CLI" upgrade --from "$SRC")

step "升级后再编译冒烟"
(cd "$DEMO" && go build ./... && go vet ./...)
if [ "${SKIP_FRONTEND:-0}" != 1 ]; then
	(cd "$DEMO/web" && npm ci && npm run build)
fi
smoke "$TAG" "升级后"

step "断言 internal/ 零 diff"
if [ -n "$(cd "$DEMO" && git status --porcelain -- internal)" ]; then
	(cd "$DEMO" && git status --short -- internal) >&2
	fail "升级改动了 internal/（用户区）"
fi
(cd "$DEMO" && git diff --quiet HEAD -- internal) || fail "internal/ 与升级前提交不一致"

echo
step "真实升级回环（merge_paths / release pack / 离线，见 upgrade_real.sh）"
SRC="$SRC" CLI="$CLI" WORK_DIR="$WORK_DIR/upgrade_real" bash "$SRC/scripts/derive_check/upgrade_real.sh"

echo
echo "PASS: 派生验证 lane 全部通过"
