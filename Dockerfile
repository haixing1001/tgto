# ---------- 第一阶段：构建 Vue3 前端 ----------
FROM node:20-alpine AS web-builder
WORKDIR /web
# 注意：不复制 package-lock.json —— 各平台可选原生依赖（rollup/esbuild 的
# linux/musl 与 win32 变体）在不同 OS 上生成的 lock 条目互不兼容，
# 由容器在 linux 环境自行解析最稳妥。
COPY web/package.json ./
RUN npm install --no-audit --no-fund
COPY web/ .
RUN npm run build

# ---------- 第二阶段：编译 Go 单二进制（无 CGO） ----------
FROM golang:1.26-alpine AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-builder /web/dist ./web/dist
# 递增补丁版本号（0.0.1 -> 0.0.2 -> ...），main.go 经 go:embed 读取 VERSION
RUN v="$(tr -d '[:space:]' < VERSION)"; [ -z "$v" ] && v="0.0.0"; \
    IFS=. read -r ma mi pa <<<"$v"; echo "${ma}.${mi}.$((pa+1))" > VERSION
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /tgto-bin .

# ---------- 第三阶段：极简运行时 ----------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone
WORKDIR /app
COPY --from=go-builder /tgto-bin /app/Tgto

ENV PORT=9058 \
    ENV_WEB_PASSPORT=admin \
    ENV_WEB_PASSWORD=123 \
    TGTO_DB_DIR=/app/db \
    TGTO_STRM_DIR=/app/strm \
    TGTO_DATA_DIR=/app/data \
    GOMEMLIMIT=64MiB

VOLUME ["/app/db", "/app/strm", "/app/data"]
EXPOSE 9058

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:9058/api/health || exit 1

ENTRYPOINT ["/app/Tgto"]
