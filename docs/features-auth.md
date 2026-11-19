---
title: 登录与认证
summary: 登录方式、令牌与会话的有效期和失效规则。
---

### 登录方式

登录页按配置显示，没配置的不显示、不报错（`GET /api/v1/auth/methods` 返回 `methods`、`providers`、`registerProviders`、`forgotEnabled`）。三种方式登录成功后发同一套 JWT 双 token。

| 方式 | 启用条件 | 规则（来自代码） |
| --- | --- | --- |
| 账号密码 | `auth.methods` 含 `password`（默认） | 设置页「安全与登录 → 登录安全」的验证码总开关（`captchaEnabled`，出厂开启、保存即生效）：开启时密码登录首屏就要过验证码（滑块档），**连错达到 6 次升级为点选档**；关闭后登录、自助注册、匿名短信取码都不再出现验证码，登录仍受 IP 限流与账号锁定约束（连错含密码失败，滑块拖错也计入 IP 维度升级计数——磨滑块的自动化同样会升级，且升级即时生效：拖错把 IP 计数推过硬阈值时，交卷失败响应当场带 `CAPTCHA_REQUIRED_HARD`，前端立即切点选，不必等下一次密码失败；而锁定只数密码失败且按用户名，真人手滑不会被锁号；`GET /auth/click` 出题、`POST /auth/click/verify` 交卷：384×150 主图混入干扰字 + 候选字条按顺序点选 2~3 个字，点位只存服务端，逐点容差比对，单个挑战最多校验 5 次）。验证码默认形态是滑块拼图（`GET /auth/slider` 出题、`POST /auth/slider/verify` 交卷）：384×150 背景挖一个孔（孔位 X 有合法区间约束重试；与登录表单通栏对齐，窄屏前端等比缩放），把滑块图拖到位后签发一次性 token，写进与图形验证码同一个缓存键，所以登录、短信、注册三条链路仍走同一道 `VerifyCaptcha` 消费闸（绑定 IP、校验一次即作废；签发的 token 带形态标记——滑块 `s|`、点选 `c|`，图形码文本无标记）；**登录硬档在服务端只接受点选 token（`VerifyCaptchaHard`）**，滑块 token 与图形码文本都过不了，所以直接调 `/auth/slider` 换 token 的脚本被挡在服务端，而不只是前端切换了形态；孔位横坐标不从前端可见的 key 派生（手绘回退版用 `crypto/rand` 抽取），另外还看轨迹（≥6 个点、时长 150ms–15s、推进 ≥40px、落点 ±6px、多点轨迹的速度离散度与瞬移上限），单个挑战最多校验 3 次（拖错即换发新挑战）。两种形态的底图素材共 21 张，且每次出图叠加随机色调扰动（背景与抠块同一运算），预标记图库无法定位孔位。滑块交互由 rc-slider-captcha（MIT）内嵌面板承担：拼图面板与拖轨都在表单流内，通过后收起成一行徽标（「重新验证」换新挑战）；三种形态均带右上角刷新按钮。前端只接后端契约；形态由服务端按风险指派、用户不可降级（点选档切不走），界面上唯一的出口是「改用图形验证码」（可输入、有语音，无障碍兜底），再「改用」回服务端指派的形态；**但硬档在服务端只认点选 token，图形码文本会被拒**（计数 15 分钟固定窗口后自愈）——这是用这段可用性换掉滑块旁路的取舍。运营指标经 `/metrics` 暴露：`nexadmin_auth_captcha_issued_total` / `verify_pass_total` / `verify_fail_total`（按 image/slider/click 分型）与 `escalation_total`（按 slider/click 分级）。一键可换成图形数字码（5 位数字，224×72 字形渲染带随机缩放/倾斜/波扭曲，60 秒有效，一次性；`GET /auth/captcha/:key/audio` 可把同一张验证码读成音频，只读不消费，语音随界面语言选 zh / en）。连续密码错误达到上限锁定账号；回落值为 5 次 / 15 分钟，出厂系统参数的锁定时长为 30 分钟，可在设置页 `security` 分区或系统参数 `login.max_retry`、`login.lock_minutes` 调整；登录会话默认闲置 2 小时退出、最长 12 小时（设置页 `security` 分区的 `idleMinutes` / `maxHours`，`0` = 不限制，改完即时生效）；用户不存在时也做一次 bcrypt 比较，对外统一提示“用户名或密码错误”，真实原因只进日志；锁定状态按“存在 / 不存在的用户名一视同仁”，并落库到 `sys_login_lock`（按租户 + 用户名，过期行最多每小时清理一次、下次建锁时触发）：缓存只是快路径，进程重启或换实例后仍拒绝被锁账号的正确密码 |
| 手机号 | `auth.methods` 含 `phone` 且 `sms.provider` 非空，否则自动隐藏 | 手机号必须已绑定账号；验证码 5 分钟有效，同一手机号 60 秒只能发一次、每天最多 10 条，同一 IP 每小时最多 10 条，连续输错 5 次作废 |
| 第三方快捷登录 | `auth.providers.<name>` 配置完整，或在设置页「登录方式」开启并填全（运行时现读，保存即生效） | OAuth2 授权码流程：企业微信、钉钉、飞书、Google（OIDC，授权码 + PKCE）、GitHub、通用 OIDC（Keycloak、Casdoor 等）。`state` 一次性、回调地址白名单精确匹配；首次登录按 `match_by` 匹配已有账号，匹配不上提示“绑定已有账号”，`auto_create: true` 才自动建号；邮箱只认已验证邮箱；个人中心可绑定 / 解绑 |

第三方登录的配置键见《配置参考》第 3 节。前端回调页是 `/admin/login/oauth/<provider>`，`redirect_uris` 必须是它的完整 URL——设置页「登录方式 → 回调地址」填的就是这个值，开启某个 provider 时默认带出当前站点的地址。Google / GitHub 需要服务器能访问外网，受限网络配 `auth.proxy`。

- **设置页「登录方式」是运行时数据源（2026-11-13 接通）**：账号密码 / 手机号开关、provider 的开关与凭据、回调地址、企业微信 AgentId、账号匹配与限制、出站代理都存 `sys_setting.auth`，每次请求现读，保存后下一次加载登录页即生效，不用改 `config.yaml`、不用重启。`config.yaml` 退为回退与种子：该分区从未保存过时全部按文件走；保存过的 provider 只覆盖它填了的字段（密钥留空 = 继续用文件 / 环境变量里的），开关关闭则即使文件里配了也不生效。设置页 GET 把文件里已配置的 provider 回填成「已开启」（密钥留空），因此管理员不会看不到、也不会误以为没配置。保存时按同一套合并规则校验：开启的 provider 合起来仍缺 `client_id` / `client_secret` / `redirect_uris`（企微还缺 `agent_id`，OIDC 还缺 `issuer`）直接拒绝（`config.auth.provider_incomplete`），一种登录方式都不剩也拒绝（`config.auth.no_login_method`）——不再出现「保存成功、登录页一行没有」。provider 适配器按配置指纹缓存，改了凭据 / 端点 / 代理后下一次授权自动重建，不会拿旧 client_id 去换码。

注册页（`auth.allow_register` / 系统参数 `user.register_enabled` 开启后可达）在注册按钮下方给出同一排「快捷注册」按钮，列出全部已配置的 provider —— 即 `/auth/methods` 的 `registerProviders`（开放自助注册时等于 `providers`，关闭时为空）。两条注册通道并存：填账号密码走 `POST /auth/register`，点第三方按钮走 `GET /auth/oauth/<provider>/start?intent=register`。

- **注册意图存在服务端的 state 里**（`oauthflow.PurposeRegister`），前端改不了：`start` 时查与 `/auth/register` 同一道运行时开关（关闭返回 404），并按 IP 每小时 10 次限流（`oauth_register` 桶，所有 provider 共用，不与账号密码注册的桶混算）。
- 授权回来 `resolveLogin` 仍先认已绑定身份、再按 `match_by` 匹配已有账号；都匹配不上时**建号**（而不是返回「绑定已有账号」），**不受 provider 的 `auto_create` 约束**——那道开关只管登录侧的自动建号。
- 建号走与自助注册同一条约束：租户套餐用户数上限（`ensureUserQuota`）、无密码（不可密码登录，后续用第三方登录或在个人中心设密码）、角色取 `auth.register_roles`（与账号密码注册一致，**不是** provider 的 `default_role`），成功后同样发布 `user.created` 事件。
- 授权期间管理员关掉自助注册：state 里的注册意图作废，退回登录语义（不建号），已授权的用户不会被挡在门外。

- **快捷注册真后端整链的复跑配方（2026-11-10 实测通过）**：一次性 DB + 仓库外 CWD + `NEXADMIN_SERVER_PORT=<空闲端口>`（与上面滑块的复跑配方同一套起法），`config/config.yaml` 里配一个通用 `oidc` provider 指向本机 stub IdP，`auto_create: false`、`register_roles: [guest]`。stub IdP 三个坑：① 必须给 `/.well-known/openid-configuration`，缺了 `Flow.adapter` 直接 `ErrNotConfigured`，`start` 回 404，看起来像「注册开关没开」；② id_token 的 ES256 签名要用 `dsaEncoding: 'ieee-p1363'`（Node 默认 DER，JWS 要 raw R‖S，否则 `crypto/ecdsa: verification error`）；③ 私钥要落盘复用——适配器按 `kid` 缓存 JWKS 一小时，重启换钥会被判签名无效。实测口径：`registerEnabled` 由系统参数 `user.register_enabled`（`sys_config` 种子 id `7300000000010000005`）决定，YAML 的 `auth.allow_register` 只在行缺失时兜底；打开后 `registerProviders` = `providers`；`start?intent=register` → callback 回 `{accessToken,refreshToken,user}`（`auto_create: false` 也建号，角色 `guest`，`/auth/me` 可用），同一 provider 不带 `intent` 仍回 `{needBind}`；授权途中把开关改回 false，同一 state 的 callback 退成 `{needBind}` 且不建号；关闭状态下 `start?intent=register` 回 404；连发 11 次 `start?intent=register`，前 10 次 200、第 11 次 429。

- **登录滑块的交互交给 rc-slider-captcha（MIT），三处坑已钉死（2026-11-06）**：① 组件只在挂载时 `autoRequest` 为真、或显式 `actionRef.refresh()` 才发 `request`，两者都不给就是「滑轨在、拼图永不加载」——连错 3 次密码强制验证码后等于锁死登录；现在靠挑战 `key` 变化重挂自动取图。② rc 把把手行程按 `ratio = (bgW - puzzleW) / (bgW - buttonW - 2)`（280/63/40 → 217/238）换算成拼图行程，「把手位移 = 图片像素」的假设不成立（实测鼠标 184px、拼图 167.75px）；前端尺寸全部从真实 DOM 量。交卷上报也统一到拼图行程口径：rc 的 trail 记的是绝对 clientX，原样发会让后端「推进量 ≥40px」量成页面横坐标跨度，现按首点平移、按落点缩放，与 `x` 同坐标系。时间轴随后从「按总时长线性铺开」换成 `pointerdown/move` 的真采样（`sliderTrail.ts`：样本 ≥`TRAIL_MIN_SAMPLES`=6 才用，不足才退回线性铺开）——线性铺开的最大间隔只有 `总时长/点数`，装不出真人中途的停顿，所以时间轴必须来自真实 `pointerdown/move` 采样（真人在中途停顿后仍要能通过）；顺带清掉迁移遗留：只传不用的 `actionRef`、无人使用的 `IMG_W`、过时的 `imgH` 兜底 120 → 出厂口径 110。③ `puzzleSize` 的 `height` 是它 README 里写明的字段，不传就按文档默认「高度为背景图高度」把 63×63 的滑块图拉成 63×110（`object-fit` 默认 fill），滑块块看着是一团压扁的白框、与孔位形状永远接不上；横向行程不受影响，所以照样拖得动、判得过，错的只有画面——实测渲染盒 63×110 对原图 63×63。现在按响应里的 `pieceHeight` 显式传高度（`pieceWidth` 的兜底也从库默认宽 68 换成出厂口径 63）。键盘路径按产品决策已移除，无障碍兜底是「改用图形验证码」（可输入、有语音）。归属留一行：这条泳道的前半段（rc 迁移与 e2e 改写）在 2026-11-06 被并发会话卷进了 `27a9804`、`f82ae40`、`e1adb60`、`68515d9`、`03d403b` 五个混合提交，后半段（坐标系归一化与取证闸）是 `70953b3`，本节这两条文档记录是 `f927fa9` 及其后；已推送的历史不做改写，所以要查这一段，按这几个号一起看，别只看一条。
- **真后端整链的复跑配方（本轮实测通过）**：`cd web && npm run build` → `go build -tags embedweb -o <临时目录>/nexadmin-server`（单进程同源，避开 vite dev + go run 双进程的假红）→ `nexadmin-server init --config <一次性参数>`：库用一次性名字、`NEXADMIN_MIGRATIONS_DIR` 指到 `migrations/framework`、CWD 放仓库外，免得写脏 `config/config.yaml` 与 `data/` → `NEXADMIN_SERVER_PORT=<空闲端口>` 起服务 → 浏览器侧从真 PNG 背景按 52×52 最暗窗口反查孔位、按同一 ratio 换算拖动。实测：`POST /auth/slider/verify` 回 `code:0` 并签发 token，上报轨迹 `{first:0, max:106, last:106, n:15}`（落点 106，容差 ±6），随后 `POST /auth/login` 200 且离开 `/login`，即滑块 token 真被登录那道 `VerifyCaptcha` 消费。这条冒烟要一次性库 + 本地端口，不进 CI，所以没留用例文件（留了就是没有消费者的死件）。

### 令牌与会话

- access token（默认 30 分钟）和 refresh token（默认 168 小时），HS256 签名；刷新时旧 refresh 立即作废；登出吊销当前 access 及其配对的 refresh；吊销记录存缓存，缓存不可用时拒绝放行。
- 可选 `auth.single_session: true`：同一租户、同一用户仅保留最后一次登录；旧 access/refresh 被拒绝，刷新使用缓存原子比较替换，避免旧会话覆盖新登录。默认关闭，多实例须共享 Redis 并保持配置一致。
- 租户过期 / 停用的用户不能登录、不能刷新令牌、已登录的请求也被拒绝（第 6 节）。
- 忘记密码（`POST /auth/forgot/send-code`、`/auth/forgot/reset`）：按可用的邮件 / 短信渠道及账号绑定信息提供找回；验证码 10 分钟有效，冷却 60 秒，最多尝试 5 次，另有按 IP 与按账号的小时级限流。邮箱或手机号在启用账号中不唯一时拒绝找回且不暴露账号是否存在，由管理员重置。
- 在线会话是被动登记的：已登录用户访问接口时按“用户 + IP + UA”记入缓存，强制下线会吊销该会话最近一次见到的令牌。
