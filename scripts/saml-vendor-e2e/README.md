# SAML 厂商租户模拟器

按 Okta / Microsoft Entra / Keycloak 的常见协议形态生成 IdP 元数据与签名断言，直接打到真实 SP 的 ACS，验证：

- 登录（自动建号）
- 组 → 角色同步（登录后 `roles` 含映射角色）
- SLO（带 SessionIndex 吊销后旧令牌失效）

## 前提

先把一次性实例按 `docs/功能与权限清单.md` 2.3 起好（独立库 + 独立端口 + 管理员），并预置一个可授予的非内置角色（默认 `ops-role`）。

## 用法

```bash
go run ./scripts/saml-vendor-e2e \
  -base http://127.0.0.1:18099 \
  -admin-user admin -admin-pass <密码> -role ops-role
```

输出每个 profile 一行 PASS/FAIL，任一失败退出码非 0。

## 覆盖的差异

| profile | 属性名 | NameID | 签名 |
| --- | --- | --- | --- |
| okta | `uid` / `mail` / `groups` | emailAddress | 只签断言 |
| entra | `http://schemas...` claim URI | persistent | 响应 + 断言 |
| keycloak | `uid` / `mail` / `groups` | persistent | 响应 + 断言 |

## 限制

- 断言由脚本自签并直连 ACS，不经过厂商 IdP 的登录页；浏览器整页跳转链路由 `docs/功能与权限清单.md` 2.3 的真 IdP 步骤覆盖。
- 属性写死为示例值（用户 `alice`、组 `ops`）；换值改 `main.go` 的 profile 表。
