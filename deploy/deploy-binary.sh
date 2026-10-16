#!/usr/bin/env bash
# =============================================================================
# Tgto 免 Docker 部署脚本（systemd，适用于不想装 Docker 的 VPS）
#   用法：  sudo bash deploy-binary.sh <Tgto-linux-二进制路径>
#   示例：  sudo bash deploy-binary.sh ./Tgto-amd64
#   说明：  二进制需提前准备（amd64/arm64 与 VPS 架构一致），可在本机执行：
#          CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o Tgto-amd64 .
# =============================================================================
set -euo pipefail

BIN_PATH="${1:-}"
[ -n "$BIN_PATH" ] || { echo "用法: sudo bash deploy-binary.sh <Tgto-linux二进制>"; exit 1; }
[ -f "$BIN_PATH" ] || { echo "找不到二进制: $BIN_PATH"; exit 1; }

say()  { echo -e "\033[1;32m[部署]\033[0m $*"; }
warn() { echo -e "\033[1;33m[注意]\033[0m $*"; }
die()  { echo -e "\033[1;31m[失败]\033[0m $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "请使用 root（或 sudo）执行"

INSTALL_DIR=/opt/tgto
say "安装到 $INSTALL_DIR"
mkdir -p "$INSTALL_DIR"/{db,strm,data}
install -m 0755 "$BIN_PATH" "$INSTALL_DIR/Tgto"

# 随机初始密码（可通过编辑 unit 后 daemon-reload 修改）
RAND_PASS="$(head -c 12 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 16)"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
sed "s/__WEB_PASSWORD__/${RAND_PASS}/" "$SCRIPT_DIR/tgto.service" > /etc/systemd/system/tgto.service
systemctl daemon-reload
systemctl enable --now tgto

# 健康检查
for i in $(seq 1 15); do
  if curl -fsS --noproxy '*' http://127.0.0.1:9058/api/health >/dev/null 2>&1; then
    say "部署成功 ✅"
    echo "------------------------------------------------------------"
    echo "  访问地址: http://<你的VPS公网IP>:9058"
    echo "  管理员:   admin"
    echo "  密码:     ${RAND_PASS}"
    echo "  服务管理: systemctl {status|restart|stop} tgto"
    echo "  查看日志: journalctl -u tgto -f"
    echo "------------------------------------------------------------"
    warn "公网部署安全提醒：防火墙只放通 9058（ufw allow 9058/tcp），并尽快套 HTTPS 反代"
    exit 0
  fi
  sleep 2
done
die "服务未就绪，请执行 journalctl -u tgto -n 50 排查"
