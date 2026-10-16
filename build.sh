#!/usr/bin/env bash
# Tgto 构建脚本：递增补丁版本号（0.0.1 -> 0.0.2 -> ...）后构建前端与 Go 单二进制。
#
# 用法:
#   ./build.sh                 # 递增补丁号并构建当前平台
#   ./build.sh linux/amd64     # 递增补丁号并交叉编译
#   ./build.sh --no-bump       # 只构建，不递增版本号
#
# 版本号写入 VERSION 文件，main.go 通过 go:embed 读取，登录后前端显示。
set -euo pipefail
cd "$(dirname "$0")"

NO_BUMP=0
if [ "${1:-}" = "--no-bump" ]; then
  NO_BUMP=1
  shift
fi
TARGET="${1:-}"

# ---- 递增补丁版本号 ----
bump_version() {
  local cur
  cur="$(cat VERSION 2>/dev/null | tr -d '[:space:]')"
  [ -z "$cur" ] && cur="0.0.0"
  IFS='.' read -r major minor patch <<<"$cur"
  major=${major:-0}; minor=${minor:-0}; patch=${patch:-0}
  patch=$((patch + 1))
  echo "${major}.${minor}.${patch}" > VERSION
  echo "版本号递增: ${cur} -> ${major}.${minor}.${patch}"
}

if [ "$NO_BUMP" -eq 0 ]; then
  bump_version
fi
VERSION="$(cat VERSION | tr -d '[:space:]')"

# ---- 构建前端 ----
echo "==> 构建前端 (web)"
( cd web && npm install --no-audit --no-fund && npm run build )

# ---- 构建 Go 单二进制 ----
echo "==> 构建 Go 单二进制 (v${VERSION})"
if [ -n "$TARGET" ]; then
  GOOS="${TARGET%/*}" GOARCH="${TARGET#*/}" CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w" -o "Tgto-${TARGET#*/}" .
else
  CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o Tgto .
fi

echo "==> 完成: v${VERSION}"
