#!/usr/bin/env bash
# 在全新临时容器内验证 assistant 的实际迁移和持久化，不读取应用数据库配置。
# 用法：bash assistant-matrix.sh [postgres|mysql|mariadb|all]
set -euo pipefail

VERIFY_SRC="${NEXADMIN_SRC:-$(cd "$(dirname "$0")/../../nexadmin" && pwd)}"
VERIFY_ENGINE="${1:-all}"
VERIFY_CONTAINER=""
VERIFY_CREATED=0
cleanup() {
  if [ "$VERIFY_CREATED" -eq 1 ]; then
    docker rm -f -v "$VERIFY_CONTAINER" >/dev/null ||
      { printf '临时容器清理失败：%s\n' "$VERIFY_CONTAINER" >&2; return 1; }
    VERIFY_CREATED=0
  fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

command -v docker >/dev/null
command -v go >/dev/null
case "$VERIFY_ENGINE" in
  postgres|mysql|mariadb) VERIFY_ENGINES=("$VERIFY_ENGINE") ;;
  all) VERIFY_ENGINES=(postgres mysql mariadb) ;;
  *) printf '支持 postgres、mysql、mariadb 或 all\n' >&2; exit 2 ;;
esac

for VERIFY_ENGINE in "${VERIFY_ENGINES[@]}"; do
  VERIFY_CONTAINER="nexadmin-assistant-verify-${VERIFY_ENGINE}-$$-${RANDOM}"
  VERIFY_PASSWORD="disposable-${RANDOM}-${RANDOM}-$$"
  case "$VERIFY_ENGINE" in
    postgres)
      VERIFY_IMAGE="${ASSISTANT_POSTGRES_IMAGE:-postgres:16-alpine}"
      VERIFY_DB_PORT=5432
      VERIFY_DRIVER=postgres
      VERIFY_ENV=(-e "POSTGRES_PASSWORD=$VERIFY_PASSWORD" -e POSTGRES_DB=assistant_verify)
      VERIFY_READY=(pg_isready -U postgres -d assistant_verify)
      ;;
    mysql)
      VERIFY_IMAGE="${ASSISTANT_MYSQL_IMAGE:-mysql:8.0}"
      VERIFY_DB_PORT=3306
      VERIFY_DRIVER=mysql
      VERIFY_ENV=(-e "MYSQL_ROOT_PASSWORD=$VERIFY_PASSWORD" -e MYSQL_DATABASE=assistant_verify
        -e MYSQL_USER=assistant_verify -e "MYSQL_PASSWORD=$VERIFY_PASSWORD")
      VERIFY_READY=(sh -c 'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysqladmin ping -h 127.0.0.1 -u root --silent')
      ;;
    mariadb)
      VERIFY_IMAGE="${ASSISTANT_MARIADB_IMAGE:-mariadb:11}"
      VERIFY_DB_PORT=3306
      VERIFY_DRIVER=mysql
      VERIFY_ENV=(-e "MARIADB_ROOT_PASSWORD=$VERIFY_PASSWORD" -e MARIADB_DATABASE=assistant_verify
        -e MARIADB_USER=assistant_verify -e "MARIADB_PASSWORD=$VERIFY_PASSWORD")
      VERIFY_READY=(sh -c 'MYSQL_PWD="$MARIADB_ROOT_PASSWORD" mariadb-admin ping -h 127.0.0.1 -u root --silent')
      ;;
  esac
  printf '验证 %s（%s）\n' "$VERIFY_ENGINE" "$VERIFY_IMAGE"
  docker run -d --name "$VERIFY_CONTAINER" --memory 1g \
    "${VERIFY_ENV[@]}" -p "127.0.0.1::$VERIFY_DB_PORT" "$VERIFY_IMAGE" >/dev/null
  VERIFY_CREATED=1
  VERIFY_BINDING="$(docker port "$VERIFY_CONTAINER" "$VERIFY_DB_PORT/tcp")"
  VERIFY_PORT="${VERIFY_BINDING##*:}"
  [[ "$VERIFY_PORT" =~ ^[0-9]+$ ]] || { printf '无法获取临时端口\n' >&2; exit 1; }
  VERIFY_READY_OK=0
  for ((VERIFY_ATTEMPT=0; VERIFY_ATTEMPT<60; VERIFY_ATTEMPT++)); do
    if docker exec "$VERIFY_CONTAINER" "${VERIFY_READY[@]}" >/dev/null 2>&1; then
      VERIFY_READY_OK=1
      break
    fi
    sleep 1
  done
  [ "$VERIFY_READY_OK" -eq 1 ] || { printf '临时数据库未就绪\n' >&2; exit 1; }
  if [ "$VERIFY_DRIVER" = postgres ]; then
    VERIFY_DSN="host=127.0.0.1 port=$VERIFY_PORT user=postgres password=$VERIFY_PASSWORD dbname=assistant_verify sslmode=disable"
  else
    VERIFY_DSN="assistant_verify:$VERIFY_PASSWORD@tcp(127.0.0.1:$VERIFY_PORT)/assistant_verify?parseTime=true&charset=utf8mb4"
  fi
  (
    cd "$VERIFY_SRC"
    ASSISTANT_TEST_ALLOW_DISPOSABLE=1 \
    ASSISTANT_TEST_DB_DRIVER="$VERIFY_DRIVER" \
    ASSISTANT_TEST_DB_ENGINE="$VERIFY_ENGINE" \
    ASSISTANT_TEST_DISPOSABLE_DSN="$VERIFY_DSN" \
      go test ./plugins/assistant -run '^TestConversationPersistenceDisposableDB$' -count=1 -v -timeout 2m
  )
  cleanup
done
