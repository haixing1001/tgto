# Tgto · 网盘媒体自动化管理平台（小型 NAS 轻量单二进制版 · 去 Emby）

对标 Tgto 原版功能，**彻底移除全部 Emby 与飞牛反相关模块**，专注：

- **四大网盘**（115网盘 / 123云盘 / 光鸭云盘 / 天翼云盘）Telegram 频道监控转存
- **盘内影视（TMDB）与无损音乐（MusicBrainz）识别、整理、重命名**
- **全量 / 增量 STRM 文件树生成**（含字幕与海报边车同步）
- **多源影视发现**（TMDB / 豆瓣 / AniList / Bangumi / 热门演员）与榜单、追剧日历

技术栈：**Go 1.26+（Gin + modernc.org/sqlite 无 CGO）+ Vue 3（Vite + Tailwind CSS）**，前端经 `go:embed` 内嵌，单进程部署，无需 Nginx / Node。

## 快速开始

### Docker Compose（推荐）

```bash
docker compose up -d
# 访问 http://<NAS-IP>:9058 ，默认账号 admin / 密码 123
```

镜像多阶段构建：`node:20-alpine` 构建前端 → `golang:1.26-alpine`（`CGO_ENABLED=0` + `-ldflags="-s -w"`）编译静态二进制 → `alpine:3.20` 极简运行时。支持 `linux/amd64` 与 `linux/arm64`（`docker buildx build --platform linux/amd64,linux/arm64`）。

### 直接运行二进制

```bash
# linux/arm64 或 amd64
chmod +x Tgto-arm64 && ./Tgto-arm64
```

### 本地开发

```bash
# 前端（热更新，API 代理到 9058）
cd web && npm install && npm run dev

# 后端
go run . 
```

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `9058` | HTTP 监听端口 |
| `ENV_WEB_PASSPORT` | `admin` | 登录用户名 |
| `ENV_WEB_PASSWORD` | `123` | 登录密码 |
| `TGTO_DB_DIR` | `/app/db` | SQLite 数据目录 |
| `TGTO_STRM_DIR` | `/app/strm` | STRM 输出根目录 |
| `TGTO_DATA_DIR` | `/app/data` | 本地媒体源 / 整理沙盒目录 |
| `GOMEMLIMIT` | 空 | Go 运行时内存上限，如 `64MiB`（compose 已默认设置） |

## 小型 NAS 专项优化

- **无 CGO SQLite**：`modernc.org/sqlite`，`CGO_ENABLED=0` 可交叉编译 arm64；启动即执行 `journal_mode=WAL / synchronous=NORMAL / temp_store=MEMORY / cache_size=-4000`，降低机械硬盘随机写。
- **内存环形日志**：1000 条容量，高频轮询日志只进内存；仅 WARN/ERROR 与配置变更、转存/整理结果落库，避免唤醒磁盘。
- **令牌桶限流**：`golang.org/x/time/rate`，各网盘 QPS/Burst 可在「实用工具 - 123云盘API限速」等卡片实时调整。
- **STRM 并发生成**：4 协程 Worker Pool；增量模式按源文件 mtime 跳过；全量重建先清空目标树。
- **单二进制常驻内存约 15~25MB**（`-s -w` 裁剪后 arm64 约 24MB，UPX 可进一步压至 ~10MB 以内）。

## 目录结构

```
Tgto/
├── main.go                     # 入口：环境变量、go:embed、优雅退出
├── internal/
│   ├── db/sqlite.go            # WAL 初始化、4 张核心表、默认种子数据
│   ├── logger/ringbuffer.go    # 1000 条并发安全环形日志缓冲
│   ├── service/
│   │   ├── services.go         # 配置/会话/限流/监控调度器
│   │   ├── organizer.go        # 影视+音乐文件名正则解析、TMDB 路径生成
│   │   ├── strm.go             # STRM 并发生成、字幕/海报边车、115 分享失效检测
│   │   ├── drives.go           # 四大网盘链接识别、转存去重、Telegram 轮询
│   │   ├── meta.go             # 内置示例片库 + TMDB/AniList 真实数据源
│   │   └── http.go             # 共享 HTTP 客户端（代理支持）
│   └── api/                    # Gin 路由、认证中间件、SPA 静态回退
├── web/                        # Vue3 + Vite + Tailwind 前端（13 个业务视图）
├── Dockerfile                  # 三阶段构建
└── docker-compose.yml
```

## 功能视图（12 业务视图 + 登录页）

| 分组 | 视图 |
| --- | --- |
| 系统工具 | 全局设置、日志中心、监控历史（4 网盘 Tab）、整理历史（7 子分类 Tab + 批量回退 + 识别测试沙盒）、实用工具（7 个非 Emby 工具） |
| 影视发现 | 影视探索（5 数据源 + 五维筛选 + 一键转存）、榜单推荐（10 平台）、追剧日历（7 列时间轴）、基础配置 |
| 云盘支持 | 115网盘、123云盘、光鸭云盘、天翼云盘（统一架构：配置搜索定位 + 子菜单 + 折叠卡片 + 一键增量/全量 STRM） |

## VPS 部署（公网）

> ⚠️ 公网部署前务必修改默认密码（脚本已默认生成随机密码），并配置防火墙。

### 方式一：Docker 一键部署（推荐）

```bash
# 1. 上传工程到 VPS（本地 Windows  PowerShell / Git Bash 执行）
scp -r Tgto root@<VPS-IP>:/opt/tgto

# 2. SSH 登录后一键部署（自动装 Docker / 生成随机密码 / 构建 / 健康检查）
ssh root@<VPS-IP>
cd /opt/tgto && bash deploy/deploy-docker.sh
```

完成后访问 `http://<VPS-IP>:9058`，账号 `admin`，密码见 `.env` 的 `TGTO_PASSWORD`。

### 方式二：免 Docker（systemd 单二进制）

```bash
# 本地构建对应架构二进制（VPS 一般为 x86_64 → GOARCH=amd64；ARM 机器改 arm64）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o Tgto-amd64 .

# 上传后在 VPS 执行
scp Tgto-amd64 root@<VPS-IP>:/opt/tgto/
ssh root@<VPS-IP>
cd /opt/tgto && bash deploy/deploy-binary.sh ./Tgto-amd64
```

### 方式三：Nginx + HTTPS 反代（生产建议）

```bash
apt install -y nginx
certbot --nginx -d tgto.example.com          # 需域名解析到 VPS
cp deploy/nginx-tgto.conf /etc/nginx/sites-enabled/tgto
nginx -t && systemctl reload nginx
ufw allow 80,443/tcp && ufw allow 22/tcp && ufw enable   # 9058 仅本机回源
```

### 部署目录说明

| 文件 | 用途 |
| --- | --- |
| `deploy/deploy-docker.sh` | Docker 一键部署（装 Docker、`.env` 随机密码、构建、健康检查） |
| `deploy/deploy-binary.sh` | 免 Docker 部署（systemd 服务、随机密码、健康检查） |
| `deploy/tgto.service` | systemd 单元（内存限制、开机自启、崩溃重启） |
| `deploy/nginx-tgto.conf` | Nginx 反代 + HTTPS 示例 |

## RESTful API

`POST /api/auth/login` · `GET /api/state` · `POST/GET /api/config/:section` · `GET /api/logs` · `POST /api/logs/mock` · `GET /api/logs/download` · `GET /api/transfer/history` · `POST /api/transfer/submit` · `GET /api/organize/history` · `POST /api/organize/test` · `POST /api/organize/run` · `POST /api/organize/rollback|delete|clear` · `POST /api/strm/generate` · `POST /api/util/{proxy_test,telegram_test,tmdb_test,strm_check}` · `GET /api/discovery` · `GET /api/trending` · `GET /api/calendar` · `GET /api/meta/sources`

除 `/api/auth/login` 与 `/api/health` 外均需 `Authorization: Bearer <token>`。

## 实际功能与沙盒边界

**开箱可用的真实功能**：登录鉴权、全量配置持久化、日志环形缓冲/过滤/导出、文件名正则解析引擎（剧集/电影/无损音乐，支持自定义发布组白名单与正则强制映射）、整理任务与历史（含批量改名回退）、STRM 真实文件树生成与边车同步、分享链接识别与转存历史登记（含同链同目标去重）、令牌桶限流、Telegram Bot 真实拉取与发信（配置 Token 后）、代理/TMDB Key 连通性测试、内置示例片库检索筛选。

**沙盒边界（界面可见、不假装）**：未配置云盘凭证时，远端转存为"链接校验 + 本地登记"模式，历史消息中明确标注；豆瓣/Bangumi 为内置示例数据；配置 TMDB API Key 后影视探索、榜单自动切换 TMDB 真实数据，AniList 为公共 API 直连。

## 验证记录

- `go vet ./...` 通过；`CGO_ENABLED=0` 交叉编译 `linux/amd64`、`linux/arm64`、`windows/amd64` 全部成功。
- 前端 `vite build` 成功：JS 157KB（gzip 55.5KB）、CSS 31KB（gzip 5.9KB）。
- 冒烟测试：登录/鉴权（401）、整理队列（成功/失败计数）、转存提交与去重、STRM 增量/全量生成（字幕+海报边车）、首页与 SPA History 回退、日志导出均符合预期。
