# 部署样例（仅示例，按需调整）。构建上下文是项目根目录：
#   docker build -f deploy/Dockerfile -t nexadmin:latest .
# 多阶段：node 构建前端 -> go 构建（-tags embedweb 嵌入前端）-> 小镜像运行。

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN VERSION=$(sed -n 's/.*"framework_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' .nexadmin.manifest.json | head -n 1) \
 && CGO_ENABLED=0 go build -trimpath -tags embedweb -ldflags "-s -w -X main.version=${VERSION}" -o /out/nexadmin-server ./cmd/server

FROM alpine:3.20
# mariadb-client 提供 mysqldump / mysql（MySQL 与 MariaDB 通用），postgresql16-client 提供 pg_dump / psql（对应 compose 里的 postgres:16；
# Alpine 3.20 没有不带版本号的 postgresql-client 包名）。备份页、reinit、restore 都靠这些命令行工具。
RUN apk add --no-cache ca-certificates tzdata mariadb-client postgresql16-client \
 && addgroup -S -g 10001 nexadmin && adduser -S -u 10001 -G nexadmin nexadmin
WORKDIR /app
COPY --from=build /out/nexadmin-server /app/nexadmin-server
# 框架迁移与默认配置随镜像；config/ 是卷，默认配置放 /app/defaults，启动时同步进卷（升级镜像即升级默认值）
COPY --from=build /src/migrations /app/migrations
COPY --from=build /src/plugins /app/plugins
COPY --from=build /src/config/default.yaml /app/defaults/default.yaml
RUN printf '#!/bin/sh\nset -e\ncp -f /app/defaults/default.yaml /app/config/default.yaml\nexec /app/nexadmin-server "$@"\n' > /app/entrypoint.sh \
 && chmod +x /app/entrypoint.sh \
 && mkdir -p /app/config /app/data/backup /app/logs \
 && chown -R nexadmin:nexadmin /app/config /app/data /app/logs
USER nexadmin
# 备份目录放进 data 卷（默认 ./backup 在 /app/backup，不在任何卷里，容器重建就丢）；环境变量优先于 config.yaml 的 backup.dir。
ENV NEXADMIN_BACKUP_DIR=/app/data/backup
ENV NEXADMIN_APP_ENV=production
VOLUME ["/app/config", "/app/data", "/app/logs"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/entrypoint.sh"]
