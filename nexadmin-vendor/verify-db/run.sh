#!/usr/bin/env bash
# 真实数据库验证脚本（发行方自用，放在 nexadmin-vendor，不属于框架模板）。
#
# 做什么：构建 nexadmin-server → 准备一个临时 PostgreSQL → 非交互初始化 → 迁移状态 → 启动服务
#         → 登录并读取菜单 → 重启一次验证迁移与种子幂等 → 停止 → 清理。
#
# 用法（框架源码目录默认 ../../nexadmin）：
#   ./run.sh                           # 用 docker 起一个临时 postgres:16-alpine 容器（只监听 127.0.0.1，随机端口）
#   PG_HOST=127.0.0.1 PG_PORT=5432 PG_USER=postgres PG_PASSWORD=xxx ./run.sh   # 用已有的 PostgreSQL
#
# 安全：
#   - docker 模式：容器名带随机后缀 nexadmin-verify-*，只绑定 127.0.0.1，退出时无论成败都删除（trap）。
#   - 已有 PG 模式：只创建并最终删除一个 nexadmin_verify_<随机> 库，不动其他库。
#   - 只用临时工作目录，不改框架源码目录里的文件。
set -u

SRC="${NEXADMIN_SRC:-$(cd "$(dirname "$0")/../../nexadmin" && pwd)}"
TMP="$(mktemp -d /tmp/nexadmin-verify.XXXXXX)"
SUFFIX="$(date +%s)$RANDOM"
CONTAINER="nexadmin-verify-$SUFFIX"
DBNAME="nexadmin_verify_$SUFFIX"
APP_PORT="${APP_PORT:-18080}"
ADMIN_USER="verifyadmin"
ADMIN_PASS="Verify-$SUFFIX"
FAILS=0
DOCKER_MODE=0
CONTAINER_CREATED=0
DATABASE_INIT_ATTEMPTED=0
[ -n "${PG_HOST:-}" ] || DOCKER_MODE=1

pass() { echo "  [通过] $1"; }
fail() { echo "  [失败] $1"; FAILS=$((FAILS + 1)); }

cleanup() {
  echo "== 清理 =="
  (cd "$TMP/work" 2>/dev/null && "$TMP/nexadmin-server" stop >/dev/null 2>&1)
  if [ "$DOCKER_MODE" -eq 1 ]; then
    if [ "$CONTAINER_CREATED" -eq 1 ]; then
      docker rm -f "$CONTAINER" >/dev/null 2>&1 && echo "  已删除容器 $CONTAINER" ||
        echo "  清理失败，请检查临时容器 $CONTAINER"
    fi
  elif [ "$DATABASE_INIT_ATTEMPTED" -eq 0 ]; then
    :
  elif command -v psql >/dev/null 2>&1; then
    PGPASSWORD="$PG_PASSWORD" psql -h "$PG_HOST" -p "${PG_PORT:-5432}" -U "$PG_USER" -d postgres -c "DROP DATABASE IF EXISTS $DBNAME" >/dev/null 2>&1 && echo "  已删除库 $DBNAME"
  else
    echo "  未找到 psql，请手工删除库 $DBNAME"
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT
if [ "$DOCKER_MODE" -eq 0 ]; then
  PG_USER="${PG_USER:-postgres}"
  : "${PG_PASSWORD:?使用已有 PostgreSQL 时必须显式设置 PG_PASSWORD}"
fi

echo "== 构建 =="
(cd "$SRC" && timeout 300 go build -o "$TMP/nexadmin-server" ./cmd/server) || { echo "构建失败"; exit 1; }
pass "go build"

echo "== 准备 PostgreSQL =="
if [ "$DOCKER_MODE" -eq 1 ]; then
  command -v docker >/dev/null 2>&1 || { echo "没有 docker，请设置 PG_HOST 等变量使用已有 PostgreSQL"; exit 1; }
  PG_USER=postgres; PG_PASSWORD="pw-$SUFFIX"
  docker run -d --name "$CONTAINER" --memory 512m -e POSTGRES_PASSWORD="$PG_PASSWORD" \
    -p 127.0.0.1::5432 postgres:16-alpine >/dev/null || { echo "启动容器失败"; exit 1; }
  CONTAINER_CREATED=1
  PG_HOST=127.0.0.1
  PG_PORT="$(docker port "$CONTAINER" 5432/tcp | head -1 | sed 's/.*://')"
  for _ in $(seq 1 40); do docker exec "$CONTAINER" pg_isready -U postgres >/dev/null 2>&1 && break; sleep 1; done
  docker exec "$CONTAINER" pg_isready -U postgres >/dev/null 2>&1 || { echo "数据库未就绪"; exit 1; }
  sleep 2
else
  PG_PORT="${PG_PORT:-5432}"; PG_USER="${PG_USER:-postgres}"
fi
pass "PostgreSQL $PG_HOST:$PG_PORT 库 $DBNAME"

echo "== 准备工作目录 =="
mkdir -p "$TMP/work/config" "$TMP/work/data" "$TMP/work/logs"
cp "$SRC/config/default.yaml" "$TMP/work/config/"
ln -s "$SRC/migrations" "$TMP/work/migrations"
cat > "$TMP/init.yaml" <<EOF
db: { type: postgresql, host: "$PG_HOST", port: $PG_PORT, username: "$PG_USER", password: "$PG_PASSWORD", database: "$DBNAME", autoCreate: true }
cache: { type: memory }
admin: { username: "$ADMIN_USER", password: "$ADMIN_PASS", siteName: Verify, language: zh-CN, timezone: Asia/Shanghai }
EOF
cd "$TMP/work" || exit 1
export NEXADMIN_SERVER_PORT="$APP_PORT"

echo "== 初始化（迁移 + 种子 + 创建管理员）=="
DATABASE_INIT_ATTEMPTED=1
if timeout 300 "$TMP/nexadmin-server" init --config "$TMP/init.yaml" >"$TMP/init.log" 2>&1; then pass "init"; else fail "init"; tail -20 "$TMP/init.log"; fi

echo "== 迁移状态（初始化后应无待执行）=="
"$TMP/nexadmin-server" migrate --status >"$TMP/status.log" 2>&1
cat "$TMP/status.log" | head -30
SUMMARY="$(grep -E '^已执行 [0-9]+，待执行 [0-9]+，checksum 异常 [0-9]+' "$TMP/status.log" | tail -1)"
PENDING="$(echo "$SUMMARY" | sed -n 's/.*待执行 \([0-9]*\).*/\1/p')"
BAD="$(echo "$SUMMARY" | sed -n 's/.*checksum 异常 \([0-9]*\).*/\1/p')"
[ "${PENDING:-x}" = "0" ] && pass "无待执行迁移" || fail "待执行迁移不为 0（$SUMMARY）"
[ "${BAD:-x}" = "0" ] && pass "checksum 正常" || fail "checksum 异常（$SUMMARY）"

echo "== 启动并登录 =="
"$TMP/nexadmin-server" start >"$TMP/start.log" 2>&1 || { fail "start"; tail -20 "$TMP/start.log"; }
for _ in $(seq 1 30); do curl -fs "http://127.0.0.1:$APP_PORT/healthz" >/dev/null 2>&1 && break; sleep 1; done
curl -fs "http://127.0.0.1:$APP_PORT/healthz" >/dev/null 2>&1 && pass "/healthz" || fail "/healthz"
LOGIN="$(curl -s -X POST "http://127.0.0.1:$APP_PORT/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}")"
TOKEN="$(echo "$LOGIN" | sed -n 's/.*"accessToken":"\([^"]*\)".*/\1/p')"
[ -n "$TOKEN" ] && pass "登录" || { fail "登录"; echo "$LOGIN" | cut -c1-300; }
if [ -n "$TOKEN" ]; then
  MENUS="$(curl -s "http://127.0.0.1:$APP_PORT/api/v1/menus/mine" -H "Authorization: Bearer $TOKEN")"
  N="$(echo "$MENUS" | grep -o '"path"' | wc -l)"
  [ "$N" -gt 10 ] && pass "菜单 $N 项" || { fail "菜单过少($N)"; echo "$MENUS" | cut -c1-300; }
fi

echo "== 重启一次（验证迁移与种子幂等）=="
"$TMP/nexadmin-server" restart >"$TMP/restart.log" 2>&1
for _ in $(seq 1 30); do curl -fs "http://127.0.0.1:$APP_PORT/healthz" >/dev/null 2>&1 && break; sleep 1; done
curl -fs "http://127.0.0.1:$APP_PORT/healthz" >/dev/null 2>&1 && pass "重启后 /healthz" || fail "重启后 /healthz"
grep -h "迁移与种子已是最新" "$TMP/work/logs/"* 2>/dev/null | tail -1 | grep -q . && pass "第二次启动无新迁移/种子" || echo "  [提示] 未在日志里找到「迁移与种子已是最新」，请人工查看 logs/"

echo "== reinit --dry-run =="
"$TMP/nexadmin-server" reinit --dry-run >"$TMP/reinit.log" 2>&1 && pass "reinit --dry-run" || { fail "reinit --dry-run"; tail -15 "$TMP/reinit.log"; }
head -15 "$TMP/reinit.log"

echo
if [ "$FAILS" -eq 0 ]; then echo "全部通过"; else echo "失败 $FAILS 项"; fi
exit "$FAILS"
