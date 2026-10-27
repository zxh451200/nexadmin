# 收敛方案落地交接（nexPay 收敛方案 v0.1 · 2026-10-28）

> 本文交接《nexPay收敛方案_v0.1》01/02/03 号文档在框架母版仓库的落地改动（五个工作批次）。
> 所有改动均在工作区**未提交**；提交切分建议见第 3 节。改动与仓库既存 WIP（website 插件 / sso / web 等，
> 会话开始前已存在）在少数共享文件里交织，见第 2.2 节的精确标注。

## 1. 做了什么

| 方案步骤 | 交付 | 关键文件 |
|---|---|---|
| 03 §2.1 第 2 步 | `plugins/pay`（02 号 OpenAPI 契约全量实现 + 01 Phase 0 加固），`plugins/payment` 移除收编 | `plugins/pay/**`（新）、`web/src/plugins/pay/**`（新）、`plugins/payment/**`（删）、`web/src/plugins/payment/**`（删） |
| 03 §2.1 第 3 步 | `plugins/crm`（客户主数据 + 线索商机两张表 + 事件契约 + member 锚点） | `plugins/crm/**`、`web/src/plugins/crm/**`（均新） |
| 03 §2.2 第 4 步 | `plugins/aigateway`（nexRag ragmodel 移植 + token 用量台账 + SSRF 连通性测试；ragusage/ragmetrics 在盘面不存在，已补最小实现） | `plugins/aigateway/**`、`web/src/plugins/aigateway/**`（均新） |
| 验收门禁 §8.5 | 三引擎迁移结构一致性测试（19 插件全过），顺手修复两处历史违规 | `plugins/migrations_test.go`（新）、`plugins/imports/migrations/mariadb/001_init.sql`（新）、`plugins/ticket/migrations/postgres/001_init.sql`（新） |
| 验收门禁 §8.5/6 | 真实引擎集成测试（MySQL + PG：迁移实跑、池>1 并发退款、并发回调去重、outbox 同事务） | `plugins/pay/it_mysql_test.go`、`it_pg_test.go`（新，env 守卫默认跳过） |
| 验收门禁 §8.6 踩中并修复 | **真缺陷**：CreateRefund 事务内用 `s.db` 取池连接 → 并发退款时持锁事务与池等待互卡到 50s 锁超时；修复为事务内一律走 tx | `plugins/pay/service.go`（`channelOfDB`） |
| 03 §2.2 硬前置 | node_id 自动选号：`database.node_id: -1` 在共享缓存上按 0-1023 抢空闲号（默认 0 行为不变；memory 缓存配 -1 拒绝启动） | `core/cluster/lease.go`、`core/cluster/cluster_test.go`、`core/router.go`、`core/boot.go`、`config/default.yaml`、`docs/配置参考.md` |
| 文档 | 权限码与接口对照补录三插件 | `docs/功能与权限清单.md` |

## 2. 文件清单与提交切分

### 2.1 实际落库的提交（2026-10-28，main，未推送）

| 提交 | 内容 |
|---|---|
| `edb0fc9` | test(migrations)：三引擎一致性门禁 + imports/ticket 两处历史修复 |
| `7bed542` | feat(crm)：客户主数据插件（20 文件） |
| `c3b5a95` | feat(aigateway)：AI 网关插件（23 文件） |
| `d9c19e5` | feat(pay)：支付中台 + payment 移除 + plugins.go 切换（49 文件；**IT 测试文件一并入库**） |
| `a440ce2` | feat(cluster)：node_id 自动选号（boot.go 的 Migrate -1 分支） |
| `39596e0` | docs：权限清单/配置参考/i18n 错误码文案/本交接文档 |

### 2.2 尚未提交的部分（与既存 WIP 交织）

| 文件 | 状态 |
|---|---|
| `core/router.go` | ✅ 已解决：node_id 装配的两个 hunk 经 `git apply --cached` 剥离提交（`0e000b3`），文件剩余 diff 全属既存 WIP |
| `config/default.yaml` | 仅 `node_id` 注释一行是本轮的，其余 22 行为 WIP——默认值仍是 0，不影响行为，可随 WIP 一起提交 |

已提交树上 node_id 自动选号链路完整（cluster + boot + router，`0e000b3`），
并在独立 worktree 上验证过 HEAD 检出的 build/vet/test 全绿（无 WIP 依赖）。
原计划的"5 个主题提交"在执行中合并/拆分为 9 个（IT 测试随 `feat(pay)` 整目录入库；
router.go 用补丁剥离单 hunk 提交）。

### 2.2 与既存 WIP 交织的文件（提交时会把同文件的 WIP 一起带上）

| 文件 | 本轮改动（可识别标记） | 交织情况 |
|---|---|---|
| `core/router.go` | `buildFull` 里 `AcquireAutoNodeLease` 分支（约 25 行） | 文件总 diff 107 行，其余为既存 WIP |
| `core/i18n/locales/zh-CN.yaml` / `en-US.yaml` | `payment:` 段替换为 `pay:`（PAY_* 错误码），新增 `crm:`、`aigateway:` 段 | 各 89 行 diff，混有 WIP 的新增 key |
| `config/default.yaml` | 仅 `node_id` 注释一行 | 文件总 diff 23 行，其余为 WIP |
| `docs/配置参考.md` | 仅 `database.node_id` 表格行 | 4 行 diff |
| `docs/功能与权限清单.md` | 第 54 行插件清单句 + 附录三插件权限表（约 18 行） | 会话开始时已是 M |

纯本轮、无交织：`core/boot.go`（仅 Migrate 的 -1 分支）、`core/cluster/*`、`plugins/plugins.go`、`plugins/pay/service.go` 的 `channelOfDB` 修复（该文件其余部分为新插件本体，整目录提交）。

### 2.3 未动的既存 WIP（不要卷进本轮提交）

`plugins/website/**`、`modules/sso/**`、`modules/internal/oauthflow/**`、`web/src/framework/**`、`web/src/pages/**` 等会话开始前已修改的文件，本轮除上述共享文件外未触碰。会话中 website 插件的 `i18n.go`/`plugin.go` 曾被外部进程截空又自动恢复，终态与 WIP 一致（全仓零字节巡检通过），提交前建议人工过目这两个文件。

## 3. 验证记录

- `go build/vet/test ./...` 零失败；`tsc --noEmit` / `eslint src` 0 error。
- `nexadmin plugin lint` pay / crm / aigateway 全过；`plugin list` 三插件在册。
- 打包→安装回环：`plugin pack` 三包（`D:\tmp\pluginpack\`）→ `plugin install --force` 装回，git 状态零漂移，市场安装链路验证通过。
- 浏览器走查三页面（浅/深主题 6 截图，0 控制台错误，`D:\tmp\plugcheck\`）。
- 实库 IT（env 守卫）：MySQL 8（1.46s）与 PostgreSQL 18（1.34s）全绿——迁移实跑、并发退款恰 3/10 成功其余 409、并发重复回调出站箱恰 1 行。MariaDB 无本地实例，部署侧首落库即验。
- 派生验证：`nexadmin new` → build/vet 全过（本机无 bash，`scripts/derive_check/run.sh` 未跑，手动等效其核心步骤）。

## 4. 安全扫描（Mimosa deep，提交后补跑）

- Scan `scan-2026-10-27T21-23-11.923Z-81d236454cc6`，seal `sha256:fa918023652fdfd01b653c4ae29dd1cdc78a2ab96bcc5c6167f8252a999b0024`，状态 **inconclusive**（调用图动态派发部分覆盖）。
- **本轮新增的 pay/crm/aigateway 零命中**。20 条发现全部在既有代码：命令注入 10 条（CLI 工具链：cmd/nexadmin、core/ctl/dbtool、plugin/market/rebuild、vendor 离线件）、硬编码凭据 2 条（nexadmin-vendor/verify-db/run.sh 的 PG 口令）、path-traversal 6 条（modules/file/tus.go ×3、identity/cas_slo、website market/sign 环境变量路径）、SSRF 1 条（scripts/security/licenses.py）。
- 处置建议：这些属于既有工作流的 backlog，不阻塞本批提交；tus.go 的 3 条与 verify-db/run.sh 的 2 条建议优先人工复核（静态 advisory 需人工确认真实数据流）。

### 4.1 逐条甄别结论（2026-10-28 复核，全部 20 条）

**误报 7 条（已逐一证明，无需改码）**

| 位置 | 证明 |
|---|---|
| `modules/file/tus.go:303/319/346` | `loadSession` 第 111 行 `idgen.Parse(id)` 把入参强约束为纯整数字符串（雪花号，`.`/`/`/`%2F` 解码后都过不了 ParseInt），路径拼接无穿越面；122-126 行另有租户+用户归属校验；`drop` 的全部调用点传入的均为已校验或服务端生成的 ID |
| `plugins/identity/cas_slo.go:51` | 不可信输入（source 查询参数、XML SessionIndex）只经 `sloKnownKey` 的 SHA-256 摘要后才成为缓存 key，无任何路径内容直达 `storage.Open`；且 166-170 行要求 ticket 为本系统登记过的高熵值（validSLOIndex ≥32 位Opaque、≥8 种字符） |
| `nexadmin-vendor/verify-db/run.sh:64/65` | `PG_PASSWORD="pw-$SUFFIX"` 是每次运行的随机值（时间戳+RANDOM），喂给只绑 127.0.0.1、trap 退出即删的一次性容器，非硬编码凭据 |

**接受/缓议 13 条（属既有工作流，均非远程可利用面）**

- 命令注入 ×10（`cmd/nexadmin/new_cmd.go`、`upgrade.go`、`core/ctl/ctl.go`、`dbtool.go`、`plugin/market/rebuild.go`、vendor 离线件）：均为本地 CLI/运维工具，参数来自操作者命令行而非 HTTP 输入；`cmd/nexadmin`、`core/ctl` 当前与既存 WIP 交织，建议该工作流收尾时统一加参数白名单。
- path-traversal/污点 ×3（`website/market/sign.go` 环境变量路径 ×2、`user/oauth_http.go`→ctl）：环境变量为操作者设定，信任级别高于请求输入。
- SSRF ×1（`scripts/security/licenses.py`）：离线license 校验脚本，操作者本地运行。

若后续要让扫描器零噪音，可为 tus.go 增加显式 `^[0-9]+$` 校验（与 idgen.Parse 冗余）并在 CLI 工具侧收敛 exec 参数来源；本轮不改，避免为 appease 扫描器引入冗余代码。

## 5. 遗留与建议

1. **CI 加 MySQL/PG 服务**并注入 `NEXADMIN_IT_MYSQL_DSN` / `NEXADMIN_IT_PG_DSN` 常跑 IT，防回归。
2. `cmd/tmpquery`（含硬编码本机口令的临时诊断工具，自注"用完即删"）已删除。
3. 运维建议：退款类 HTTP 端点注意 `innodb_lock_wait_timeout`（默认 50s）与连接池上限的配比；本轮修复后事务自包含，不再有跨连接依赖。
4. 01 §7 的 5 个拍板项（资金流主线、分账、`*_micro` 改名、渠道账单 API、缺陷 A 真库复现）仍待业务方决策。
5. 第 5 步服务实体化（pay-adapter / msg-outbox / iot-ingest / idp）的前置已就位两块（pay 插件的 outbox 模式、node_id 自动选号），启动各服务立项前需外部依赖到位（渠道凭据、设备接入、合规口径）。
