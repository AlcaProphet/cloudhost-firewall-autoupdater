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
ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w -X github.com/alcaprophet/cloudhost-firewall-autoupdater/version.Version=${VERSION}" \
    -o /fwalizer .

# 运行阶段
FROM alpine:3.20
RUN adduser -D appuser \
    && mkdir -p /app/data /home/appuser/.config/fwalizer \
    && chown -R appuser:appuser /app/data /home/appuser/.config
WORKDIR /app
COPY --from=builder /fwalizer /usr/local/bin/fwalizer
USER appuser
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://localhost:60200/api/health 2>/dev/null || pgrep fwalizer || exit 1
ENTRYPOINT ["fwalizer"]
