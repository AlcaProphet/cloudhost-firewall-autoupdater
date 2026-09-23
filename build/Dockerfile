# 前端构建阶段（与 CI 的 setup-node 版本保持一致，见 docker-publish.yml）
FROM node:24-alpine AS frontend-builder
WORKDIR /src/webui/frontend
COPY webui/frontend/package*.json ./
RUN npm ci
COPY webui/frontend/ ./
RUN npm run build

# 编译阶段
FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend-builder /src/webui/frontend/dist ./webui/frontend/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /fwalizer .

# 运行阶段
FROM alpine:3.20
RUN adduser -D appuser \
    && mkdir -p /app/data /home/appuser/.config/fwalizer \
    && chown -R appuser:appuser /app/data /home/appuser/.config
WORKDIR /app
COPY --from=builder /fwalizer /usr/local/bin/fwalizer
# 部署参数：容器内必须监听所有接口；端口可由运行时环境覆盖，健康检查跟随该变量
ENV WEBUI_HOST=0.0.0.0 \
    WEBUI_PORT=60200 \
    FWALIZER_DATA_DIR=/app/data
USER appuser
# 健康检查只依赖 HTTP /api/health：进程存活但 WebUI 不可用时必须判为 unhealthy
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null "http://127.0.0.1:${WEBUI_PORT:-60200}/api/health" || exit 1
ENTRYPOINT ["fwalizer"]
