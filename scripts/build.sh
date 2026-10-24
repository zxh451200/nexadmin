#!/bin/sh
# 生产构建：前端 npm ci && npm run build -> go build（嵌入前端产物）-> bin/nexadmin-server。不依赖 make。
# 版本号取 .nexadmin.manifest.json 的 framework_version，注入 main.version。
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

VERSION=$(sed -n 's/.*"framework_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' .nexadmin.manifest.json | head -n 1)
[ -n "$VERSION" ] || { echo "读不到 .nexadmin.manifest.json 的 framework_version" >&2; exit 1; }

echo "==> 构建前端"
(cd web && npm ci && npm run build)

echo "==> 构建后端 $VERSION"
mkdir -p bin
go build -trimpath -tags embedweb -ldflags "-s -w -X main.version=$VERSION" -o bin/nexadmin-server ./cmd/server
echo "完成: bin/nexadmin-server"
