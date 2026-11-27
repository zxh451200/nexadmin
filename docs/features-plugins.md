---
title: 插件与授权
summary: 插件怎么交付与安装、free 与 pro 的区别、授权文件校验什么、运行期怎么启停。
---

插件以**源码包**交付，不是动态库。安装 = 把源码放进 `plugins/<id>` 并重新编译，没有"热插拔"这回事。

| 版本 | 判定 | 授权文件 |
| --- | --- | --- |
| `free` | 插件清单里标 `Edition: Free` | 不需要 |
| `pro` | 插件清单里标 `Edition: Pro` | 需要 `data/licenses/<id>.lic` |

授权文件是一份 ed25519 签名的离线文件，载荷包含插件 ID、客户名称、签发时间、到期时间，可选绑定域名。

## 校验时机

服务启动时逐个校验，通过才挂载该插件的路由与菜单：

- 缺失、过期、被篡改 → 不挂载，插件管理页显示具体原因。
- 导入授权后会对**已编译进二进制**的插件尝试热激活；返回 `needRestart` 时按提示重启。
- 域名绑定、有效期、`requires_nexadmin` 版本范围三者必须同时满足。

> [!NOTE]
> 授权导入只激活已经编译进来的插件。源码没装进去，导授权也不会出现。

## 命令

| 命令 | 用途 |
| --- | --- |
| `nexadmin plugin list` | 列出已安装插件与授权状态 |
| `nexadmin plugin install <zip>` | 解包到源码树并写回 `plugins/plugins.go` |
| `nexadmin plugin remove <id>` | 删源码与注册导入（保留表与授权文件） |
| `nexadmin plugin keygen` / `sign` / `revoke` | 发行方签发与吊销授权 |

安装或删除后都要重新构建并重启，改动才会进二进制。

## 运行期启停

插件管理页每张卡片一个开关，对应 `POST /plugins/<id>/enabled`。停用**立即生效**：

- 该插件的路由返回 403 并带上停用原因。
- 侧栏菜单入口消失，`GET /plugins/active` 也不再返回它。
- 停用清单落在节点本地的 `data/licenses/disabled.json`——多实例部署时每个节点各有一份。

> [!WARNING]
> 停用不动数据库，表和数据都还在。要用 `--purge` 连表一起清时先停服并备份：它会把插件表与迁移记录一起删掉，不可逆。

## 插件一览

### 免费

| ID | 名称 | 版本 |
| --- | --- | --- |
| `codegen` | 代码生成器 | 1.4.0 |
| `crm` | 客户主数据 | 0.1.0 |
| `cron` | 定时任务 | 1.0.0 |
| `imports` | 导入中心 | 0.1.0 |
| `kb` | 知识库 | 0.1.0 |
| `ldap` | LDAP/AD 登录 | 0.1.0 |
| `maps` | 地图服务 | 0.1.0 |
| `realname` | 证件实名 | 0.1.0 |
| `totp` | 两步验证 | 0.1.0 |
| `website-theme-auvex` | 官网主题 · AUVEX 数控机床 | 1.0.0 |
| `website-theme-solaris` | 官网主题 · SOLARIS 储能系统 | 1.0.0 |
| `website-theme-torkon` | 官网主题 · TORKON 工业工具 | 1.0.0 |

### 专业

| ID | 名称 | 版本 |
| --- | --- | --- |
| `aigateway` | AI 网关 | 0.1.0 |
| `assistant` | AI 助手 | 1.1.0 |
| `audit` | 数据变更审计 | 1.0.0 |
| `devicehub` | 设备接入与监控 | 0.1.0 |
| `formbuilder` | 表单构建器 | 1.0.0 |
| `i18n-editor` | 多语言在线编辑 | 1.0.0 |
| `identity` | 身份源 | 1.0.0 |
| `im` | 在线客服 | 0.6.0 |
| `media` | 媒体优化 | 0.1.0 |
| `pay` | 支付中台 | 0.1.0 |
| `report` | 报表中心 | 1.1.0 |
| `saml` | SAML 单点登录 | 0.9.0 |
| `scim` | SCIM 用户同步 | 1.0.0 |
| `security` | 高级安全 | 1.0.0 |
| `ticket` | 工单中心 | 0.1.0 |
| `website` | 企业官网 | 0.6.0 |
| `workflow` | 工作流 | 0.2.0 |

各插件具体做什么、入口在哪个菜单，见[内置模块清单](features-modules.md)；插件包怎么组织、怎么打包安装，见[插件开发指南](plugin-development.md)。

> [!IMPORTANT]
> 免费 / 专业只是运行期的技术分类，不是法律许可声明。价格、试用与转让条款以发行方的正式合同为准。
