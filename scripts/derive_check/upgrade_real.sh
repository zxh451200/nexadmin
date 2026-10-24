#!/usr/bin/env bash
# 真实升级回环（仅框架仓库，不下发；nexadmin new 与源码包都排除本目录）。
# A) new 派生 demo  B) 二开侧改动  C) 框架侧模拟 0.2.0 并 release pack 成 zip
# D) demo 上 upgrade --dry-run 再正式 upgrade --from <zip>  E) 断言
# 全程禁网（GOPROXY=off）；不跑 npm install / 前端构建（package.json 只做文本合并断言）。
# 环境变量：SRC（框架源码根，默认本脚本上两级）、CLI（已编译的 nexadmin，缺省自行编译）、
#           WORK_DIR（工作目录，缺省 mktemp 并在结束删除）。
set -euo pipefail

SRC=${SRC:-$(cd "$(dirname "$0")/../.." && pwd)}
if [ -n "${WORK_DIR:-}" ]; then
	mkdir -p "$WORK_DIR"
	KEEP=1
else
	WORK_DIR=$(mktemp -d)
	KEEP=0
fi
cleanup() { [ "$KEEP" = 1 ] || rm -rf "$WORK_DIR"; }
trap cleanup EXIT
step() { printf '\n==> [upgrade_real] %s\n' "$*"; }
fail() { echo "FAIL(upgrade_real): $*" >&2; exit 1; }
export GIT_AUTHOR_NAME=derive-check GIT_AUTHOR_EMAIL=derive-check@example.invalid
export GIT_COMMITTER_NAME=derive-check GIT_COMMITTER_EMAIL=derive-check@example.invalid
# 禁网：模块只能来自本地缓存；-mod=mod 允许 go get / tidy 改 go.mod；校验库关闭以免联网校验
export GOPROXY=off GOFLAGS=-mod=mod GOSUMDB=off GONOSUMDB='*' GOTOOLCHAIN=local

if [ -z "${CLI:-}" ]; then
	CLI="$WORK_DIR/nexadmin-cli"
	(cd "$SRC" && go build -o "$CLI" ./cmd/nexadmin)
fi
DEMO="$WORK_DIR/demo"
FW="$WORK_DIR/fw"
ZIP="$WORK_DIR/nexadmin-0.2.0.zip"
USER_MARK="UPGRADE_REAL_USER_MARK"
FW_MARK="UPGRADE_REAL_FRAMEWORK_0_2_0"

# 用户区快照：internal/、web/src/pages/、config.yaml、theme.css 的 sha256 清单（config.yaml 被 .gitignore，不能靠 git diff）
user_snapshot() {
	(cd "$DEMO" && { find internal web/src/pages -type f 2>/dev/null; echo config/config.yaml; echo web/src/theme.css; } | LC_ALL=C sort | xargs sha256sum)
}
need() { grep -q -- "$2" "$1" || fail "$1 缺少: $2"; }
absent() { ! grep -q -- "$2" "$1" || fail "$1 不应包含: $2"; }

step "A) nexadmin new demo"
"$CLI" new demo --out "$DEMO" --git >/dev/null
[ -f "$DEMO/scripts/build.bat" ] && [ -f "$DEMO/core/health.go" ] || fail "demo 缺少预期的框架文件"
grep -q '"merge_paths"' "$DEMO/.nexadmin.manifest.json" || fail "demo 清单没有 merge_paths"
# 立刻再派生一份作为"框架 0.2.0 的工作拷贝"（保持 module 为 nexadmin）
"$CLI" new fw --module nexadmin --out "$FW" >/dev/null

step "B) 二开侧改动"
(cd "$DEMO" && "$CLI" add module article --fields "title:string,status:int" >/dev/null)
[ -d "$DEMO/internal/modules/article" ] || fail "add module 没有生成 internal/modules/article"
# 二开自己的 Go 依赖：缓存里已有的 github.com/google/uuid v1.6.0，并在 internal/ 里真的用到
[ -d "$(go env GOMODCACHE)/github.com/google/uuid@v1.6.0" ] || fail "模块缓存里没有 uuid@v1.6.0，无法离线验证"
mkdir -p "$DEMO/internal/userprobe"
cat >"$DEMO/internal/userprobe/probe.go" <<'EOF'
package userprobe

import "github.com/google/uuid"

// ID 仅用于验证：二开自己加的依赖在框架升级后仍在 go.mod 里。
func ID() string { return uuid.NewString() }
EOF
(cd "$DEMO" && go get github.com/google/uuid@v1.6.0 >/dev/null 2>&1) || fail "demo 离线 go get uuid 失败"
# 离线跑不了 go mod tidy：手动去掉 go get 留下的 // indirect（等价于 tidy 对"被 internal/ 直接 import"的结果）
sed -i 's#\(github.com/google/uuid v1.6.0\) // indirect#\1#' "$DEMO/go.mod"
# 二开自己的前端依赖（只改文件，不 npm install）
sed -i '0,/"dependencies": {/s//"dependencies": {\n    "user-only-dep": "1.0.0",/' "$DEMO/web/package.json"
grep -q user-only-dep "$DEMO/web/package.json" || fail "写入 user-only-dep 失败"
mkdir -p "$DEMO/config"
printf 'user_marker: %s\n' "$USER_MARK" >"$DEMO/config/config.yaml"
printf '\n/* %s */\n' "$USER_MARK" >>"$DEMO/web/src/theme.css"
# 手改一个框架文件（框架 0.2.0 也会改它 => 触发 .bak + WARN）
printf '\n// %s hand edit\n' "$USER_MARK" >>"$DEMO/core/health.go"
(cd "$DEMO" && git add -A && git commit -q -m "二开改动")
BEFORE=$(user_snapshot)

step "C) 框架侧：在拷贝里模拟 0.2.0 并 release pack"
[ -d "$(go env GOMODCACHE)/github.com/google/go-cmp@v0.7.0" ] || fail "模块缓存里没有 go-cmp@v0.7.0，无法离线验证"
# 新增一个 core 文件（用到新依赖 go-cmp）
mkdir -p "$FW/core/upgradeprobe"
cat >"$FW/core/upgradeprobe/probe.go" <<EOF
// Package upgradeprobe 仅用于升级回环：框架 0.2.0 新增的 core 文件，依赖新引入的 go-cmp。
package upgradeprobe

import "github.com/google/go-cmp/cmp"

// $FW_MARK
func Same(a, b string) bool { return cmp.Equal(a, b) }
EOF
(cd "$FW" && go get github.com/google/go-cmp@v0.7.0 >/dev/null 2>&1) || fail "框架拷贝离线 go get go-cmp 失败"
# 修改已有 core 文件（与 demo 手改的是同一个）、修改 cmd/server/main.go、删除一个框架文件
printf '\n// %s\n' "$FW_MARK" >>"$FW/core/health.go"
printf '\n// %s\n' "$FW_MARK" >>"$FW/cmd/server/main.go"
[ -f "$FW/scripts/build.bat" ] || fail "框架拷贝没有 scripts/build.bat"
rm "$FW/scripts/build.bat"
# package.json：改一个依赖版本 + 新增一个框架依赖（不 npm install）
sed -i 's/"zustand": "5\.0\.15"/"zustand": "5.0.16"/' "$FW/web/package.json"
grep -q '"zustand": "5.0.16"' "$FW/web/package.json" || fail "修改 zustand 版本失败（基线版本变了？）"
sed -i '0,/"dependencies": {/s//"dependencies": {\n    "fw-new-dep": "2.0.0",/' "$FW/web/package.json"
sed -i 's/"framework_version": "0\.1\.0"/"framework_version": "0.2.0"/' "$FW/.nexadmin.manifest.json"
grep -q '"framework_version": "0.2.0"' "$FW/.nexadmin.manifest.json" || fail "framework_version 未升到 0.2.0"
(cd "$FW" && go build ./core/upgradeprobe/) || fail "新增的 core 文件在框架拷贝里编译失败"
if (cd "$FW" && "$CLI" release check >/dev/null 2>&1); then
	(cd "$FW" && "$CLI" release pack --out "$ZIP")
else
	# 有 FAIL（例如仍是开发公钥）：不加 --force 必须拒绝，再用 --force 打内部测试包
	if (cd "$FW" && "$CLI" release pack --out "$WORK_DIR/refused.zip" >/dev/null 2>&1); then fail "release check 有 FAIL 时 pack 没有拒绝"; fi
	[ ! -e "$WORK_DIR/refused.zip" ] || fail "拒绝打包却产生了 zip"
	(cd "$FW" && "$CLI" release pack --force --out "$ZIP")
fi
ZIP_LIST=$(unzip -Z1 "$ZIP")   # 先落变量，避免 grep -q 提前退出让 unzip 吃 SIGPIPE（pipefail）
grep -q 'nexadmin-0.2.0/.nexadmin.manifest.json' <<<"$ZIP_LIST" || fail "zip 缺少根目录 nexadmin-0.2.0/ 或清单"
if grep -Eq 'derive_check|\.dev-keys|config/config\.yaml|\.nexadmin\.files\.json' <<<"$ZIP_LIST"; then fail "zip 含应排除的文件"; fi

step "D) demo: upgrade --dry-run，再正式 upgrade --from zip"
cd "$DEMO"
"$CLI" upgrade --from "$ZIP" --dry-run | tee "$WORK_DIR/dry.log"
need "$WORK_DIR/dry.log" '~  go.mod'
need "$WORK_DIR/dry.log" '+ require github.com/google/go-cmp'
need "$WORK_DIR/dry.log" '= 保留二开独有依赖 github.com/google/uuid'
need "$WORK_DIR/dry.log" '~  web/package.json'
need "$WORK_DIR/dry.log" 'user-only-dep'
need "$WORK_DIR/dry.log" 'WARN'
[ -z "$(git status --porcelain)" ] || fail "dry-run 改动了 demo"
"$CLI" upgrade --from "$ZIP" | tee "$WORK_DIR/up.log"
need "$WORK_DIR/up.log" 'go mod tidy 与 (cd web && npm install)'

step "E) 断言"
[ "$(user_snapshot)" = "$BEFORE" ] || fail "用户区（internal/、web/src/pages/、config.yaml、theme.css）发生变化"
git status --porcelain -- internal web/src/pages | grep -q . && fail "internal/ 或 web/src/pages/ 有 git 变化"
[ -f core/upgradeprobe/probe.go ] || fail "框架新增的 core 文件没有落地"
need core/health.go "$FW_MARK"
need cmd/server/main.go "$FW_MARK"
[ ! -e scripts/build.bat ] || fail "框架删除的文件仍在"
ls .nexadmin-trash/*/scripts/build.bat >/dev/null 2>&1 || fail "删除的文件没有进 .nexadmin-trash"
need core/health.go.bak "$USER_MARK hand edit"
need "$WORK_DIR/up.log" 'WARN: 被手改过'
need go.mod '^module demo$'
need go.mod 'github.com/google/uuid v1.6.0'
need go.mod 'github.com/google/go-cmp v0.7.0'
need web/package.json '"name": "demo"'
need web/package.json '"user-only-dep": "1.0.0"'
need web/package.json '"fw-new-dep": "2.0.0"'
need web/package.json '"zustand": "5.0.16"'
need .nexadmin.manifest.json '"framework_version": "0.2.0"'
absent core/upgradeprobe/probe.go '"nexadmin/'
echo "  go mod tidy（离线）..."
if ! timeout 280 go mod tidy >"$WORK_DIR/tidy.log" 2>&1; then
	echo "  go mod tidy 离线失败，改用 go build ./...：$(tail -n 2 "$WORK_DIR/tidy.log" | tr '\n' ' ')"
fi
timeout 280 go build ./... && timeout 280 go vet ./... || fail "升级后 demo 编译/vet 失败"
need go.mod 'github.com/google/uuid'
cd "$SRC"
echo "PASS(upgrade_real): 用户区零变化；框架新增/修改/删除落地；.bak+WARN；go.mod/package.json 同时含二开与框架依赖；编译通过（未跑 npm install / 前端构建）"
