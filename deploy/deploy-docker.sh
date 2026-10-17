#!/usr/bin/env bash
# =============================================================================
# Tgto Docker 一键部署脚本（在 VPS 上以 root 或 sudo 执行）
#   用法：  bash deploy-docker.sh
#   前提：  本目录为 Tgto 工程根目录（含 docker-compose.yml）；无需 Dockerfile，
#          镜像从 GHCR 拉取（ghcr.io/haixing1001/tgto，amd64/arm64 多架构）
#   行为：  安装 Docker（如缺失）→ 生成 .env（首次随机密码）→ 拉取镜像并启动 → 健康检查
# =============================================================================
set -euo pipefail

APP_NAME="tgto"
DEFAULT_PORT="9058"
cd "$(dirname "$0")/.."   # 进入 Tgto 工程根目录

say()  { echo -e "\033[1;32m[部署]\033[0m $*"; }
warn() { echo -e "\033[1;33m[注意]\033[0m $*"; }
die()  { echo -e "\033[1;31m[失败]\033[0m $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "请使用 root 执行（或加 sudo）"

# ---------- 1. Docker 安装检测 ----------
if ! command -v docker >/dev/null 2>&1; then
  say "未检测到 Docker，开始自动安装..."
  if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y
    apt-get install -y ca-certificates curl gnupg
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    chmod a+r /etc/apt/keyrings/docker.gpg
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" \
      > /etc/apt/sources.list.d/docker.list
    apt-get update -y
    apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  elif command -v yum >/dev/null 2>&1; then
    yum install -y yum-utils
    yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
    yum install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
    systemctl enable --now docker
  else
    die "无法识别的包管理器，请手动安装 Docker 后重试"
  fi
  systemctl enable --now docker 2>/dev/null || true
fi
docker compose version >/dev/null 2>&1 || die "docker compose 插件不可用，请升级 Docker"

# ---------- 2. 生成 .env（首次部署生成随机密码） ----------
if [ ! -f .env ]; then
  RAND_PASS="$(head -c 12 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 16)"
  cat > .env <<EOF
# Tgto 部署配置（由 deploy-docker.sh 生成）
TGTO_USER=admin
TGTO_PASSWORD=${RAND_PASS}
TGTO_PORT=${DEFAULT_PORT}
TGTO_MEMLIMIT=64MiB
# 固定镜像版本（留空/注释则跟随 ghcr.io/haixing1001/tgto:latest）
#TGTO_IMAGE=ghcr.io/haixing1001/tgto:latest
EOF
  chmod 600 .env
  say "已生成 .env：管理员 admin / 随机密码 ${RAND_PASS}（请妥善保存，可随时编辑 .env 后 docker compose up -d 生效）"
else
  say "检测到已有 .env，沿用现有配置"
fi
set -a; . ./.env; set +a
IMAGE="${TGTO_IMAGE:-ghcr.io/haixing1001/tgto:latest}"

# ---------- 3. 拉取镜像并启动 ----------
# 镜像由 GitHub Actions 构建（amd64 + arm64），客户端不再本地编译
say "拉取镜像 ${IMAGE}（多架构，首次约 1~3 分钟）..."
if ! docker compose pull; then
  warn "拉取失败排查：网络是否可达 ghcr.io；镜像 tag 是否存在；私有包需先 docker login ghcr.io"
  die "镜像拉取失败，已中止部署"
fi
say "启动容器..."
docker compose up -d --remove-orphans

# ---------- 4. 健康检查 ----------
say "等待服务就绪..."
PORT="${TGTO_PORT:-$DEFAULT_PORT}"
for i in $(seq 1 30); do
  if curl -fsS --noproxy '*' "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
    say "部署成功 ✅"
    echo "------------------------------------------------------------"
    echo "  访问地址:  http://$(hostname -I 2>/dev/null | awk '{print $1}'):${PORT}"
    echo "  公网访问:  http://<你的VPS公网IP>:${PORT}"
    echo "  管理员:    ${TGTO_USER:-admin}"
    echo "  密码:     见 .env 中 TGTO_PASSWORD"
    echo "------------------------------------------------------------"
    warn "公网部署安全提醒："
    warn "  1. 已使用随机密码，切勿改回 123 等弱口令"
    warn "  2. 建议防火墙只放通必要端口：ufw allow ${PORT}/tcp && ufw enable"
    warn "  3. 强烈建议套 Nginx + HTTPS（域名字证书）后再对公网开放"
    exit 0
  fi
  sleep 2
done
die "服务未在预期时间内就绪，请执行 docker compose logs -f 排查"
