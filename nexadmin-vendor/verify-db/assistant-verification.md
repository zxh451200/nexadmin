# Assistant 持久化验收

本目录的 `assistant-matrix.sh` 为 assistant 命名会话创建一次性数据库容器，执行主项目中的真实迁移集成测试。它不读取 `config/config.yaml`，也不连接应用已配置的数据库。退出时删除本次容器及其匿名卷。

在已安装 Docker 和 Go、能获取数据库镜像的环境执行：

```bash
bash assistant-matrix.sh all
```

也可只执行 `postgres`、`mysql` 或 `mariadb`。默认镜像为 `postgres:16-alpine`、`mysql:8.0`、`mariadb:11`。有内部镜像仓库时，可分别设置 `ASSISTANT_POSTGRES_IMAGE`、`ASSISTANT_MYSQL_IMAGE`、`ASSISTANT_MARIADB_IMAGE`，源码路径可通过 `NEXADMIN_SRC` 指定。

测试直接执行 `plugins/assistant/migrations/<engine>/001_conversations.sql`，检查：

- 同租户不同用户及不同租户不能读写彼此会话。
- 每个用户的 slot 唯一且只能在 0–29 之间，不超过 30 个会话。
- 两个并发写入使用同一 revision 时只能有一个成功，过期写入不覆盖历史。
- 关闭并重新打开数据库连接池后，历史及 revision 仍存在。

普通 `go test ./plugins/assistant` 不带数据库参数时会跳过真实库用例；普通测试成功不能替代三引擎验收。单元测试另覆盖非法 ID、工具消息不落库、创建上限和空槽复用。

本轮环境检查结果：

- Docker 可用，但本地没有 PostgreSQL/MySQL/MariaDB 镜像。
- 拉取 `postgres:16-alpine` 时访问 `registry-1.docker.io:443` 超时，真实数据库用例未执行成功。
- `govulncheck ./...` 获取 `vuln.go.dev` 数据超时，漏洞扫描没有完成。
- 没有安装 `browser-use`，浏览器页面验证尚未执行。

这里记录的是验证入口与实际阻塞，不是验收通过记录。LDAP/AD/CAS、共享 Redis、S3 和插件授权挂载仍需要各自的集成环境。
