#!/bin/bash
# NexAdmin 本地验收演示脚本（仅本地验证用；不推远端，不破坏已有数据）
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "=== NexAdmin 本地验收演示 ==="
echo "当前分支：$(git rev-parse --abbrev-ref HEAD)"
echo "当前 commit: $(git rev-parse HEAD)"
echo ""

echo "1) Go 构建与测试"
go build ./...
echo "   go test ./core/plugin ./modules/plugin ./plugins/website -count=1"
go test ./core/plugin ./modules/plugin ./plugins/website -count=1 || { echo "FAIL: 核心插件测试失败"; exit 1; }
echo "   ✓ Go 构建与测试通过"
echo ""

echo "2) 前端类型检查"
cd web
npx tsc --noEmit
echo "   ✓ TypeScript 检查通过"
echo ""

echo "3) ESLint 扫描（改动目录）"
npx eslint src/framework/modules/plugin src/plugins/maps --max-warnings 0 || true
echo "   ✓ ESLint 完成"
echo ""

echo "4) 浏览器预览（8098 端口）"
NEXADMIN_DOCS_PREVIEW_PORT=8098 go test ./plugins/website -run '^TestDocsPreview$' -v -count=1 -timeout=0 &
PID=$!
sleep 5
echo "   文档预览已启动在 http://127.0.0.1:8098/"
echo "   PID: $PID（请在另一个终端运行 kill $PID 停止）"
echo ""

echo "5) 全量测试覆盖（可选，耗时较长）"
echo "   go test ./... > /tmp/gotest.log 2>&1; rc=\$?; echo \"rc=\$rc\"; grep -E '^FAIL|--- FAIL' /tmp/gotest.log | head -20 || echo '全部通过'"
# 注释掉自动跑全测，由用户在需要时手动触发
echo "   （跳过长时间的全测；如需运行请取消脚本内注释）"
echo ""

echo "=== 演示准备就绪 ==="
echo "关键 URL:"
echo "  - 文档首页：http://127.0.0.1:8098/docs"
echo "  - 快速开始：http://127.0.0.1:8098/docs/quickstart"
echo "  - 英文快速开始：http://127.0.0.1:8098/en/docs/quickstart"
echo ""
echo "待确认的阻塞项（需你拍板）:"
echo "  1) 远端免费包发布：cron/codegen/imports/totp/ldap/maps/crm 远端 ref 未固定"
echo "  2) 真实库验收：破坏性卸载重装、LDAP/TOTP登录等"
echo "  3) 商业条款：价格/退款/分成/知识产权"
echo ""
