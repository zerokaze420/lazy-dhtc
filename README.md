# dhtc

[![Go Version](https://img.shields.io/github/go-mod/go-version/zerokaze420/lazy-dhtc)](https://go.dev/)
[![Upstream](https://img.shields.io/badge/upstream-nbdy%2Fdhtc-blue)](https://github.com/nbdy/dhtc)
[![License](https://img.shields.io/github/license/zerokaze420/lazy-dhtc)](LICENSE)

`dhtc` 是一个私有 BitTorrent DHT 爬虫和种子搜索引擎。它可以实时采集 DHT 网络中的种子元数据，并通过 Web 界面提供搜索、浏览、统计、订阅通知和下载器联动等功能。

本仓库基于上游项目 [nbdy/dhtc](https://github.com/nbdy/dhtc) 开发，重点补充了懒猫微服部署、中文界面、可控采集和 MCP 接口等功能。

> [!WARNING]
> 本项目仅用于技术研究和个人数据管理。请遵守所在地法律法规，不要采集、传播或下载无权使用的内容。

## 与上游项目的区别

本项目不是上游仓库的纯镜像，当前主要差异如下：

| 功能 | 本项目 | 上游 `nbdy/dhtc` |
| --- | --- | --- |
| 懒猫微服部署 | 提供 `package.yml`、`lzc-manifest.yml`、`lzc-build.yml` 和专用镜像构建文件 | 未提供 |
| 中文界面 | 支持简体中文与英文切换，可根据浏览器语言自动选择 | 以英文界面为主 |
| 爬虫启停 | 可在 Web 界面手动开始和停止采集 | 启动进程后持续运行 |
| 定时停止 | 可设置采集若干分钟后自动停止 | 未提供 |
| 启动策略 | 可配置应用启动时是否自动开始采集 | 默认随进程启动 |
| 中文内容过滤 | 可仅保存名称或文件路径中含中文字符的种子 | 未提供 |
| 数据容量限制 | 可设置最多保存的种子数量，超限后删除最旧记录 | 未提供 |
| 数据清理 | 可从设置页清除已采集种子和统计数据，保留设置、订阅和黑名单 | 未提供独立入口 |
| 懒猫系统通知 | 实时捕获新种子时可调用当前设备的系统通知 | 未提供 |
| MCP 接口 | 提供 `/mcp`，支持搜索、最新种子、统计和分类查询 | 未提供 |
| 爬虫生命周期 | 增强停止流程，允许安全终止 DHT 服务和元数据下载任务 | 原实现主要面向持续运行 |
| DHT 地址族 | 可选择仅 IPv4 或 IPv4 + IPv6 双栈 | 主要以 IPv4 方式运行 |

上游已有的数据库、搜索、订阅、多渠道通知和下载器联动能力仍然保留。若需要了解原始项目的设计与历史，请访问 [nbdy/dhtc](https://github.com/nbdy/dhtc)。

## 功能

### 发现与搜索

- 实时采集 BitTorrent DHT 网络中的种子元数据。
- 通过 WebSocket 在“实时捕获”页面查看新发现的种子。
- 浏览最新收录内容，或按名称、Info Hash、文件名等字段搜索。
- 支持正则表达式名称黑名单和文件黑名单。
- 可选“仅保存中文内容”过滤规则。

### 管理与自动化

- 在 Web 界面开始或停止爬虫。
- 可选择仅 IPv4 或 IPv4 + IPv6 双栈采集。
- Dashboard 分别统计 IPv4、IPv6 DHT 成功抓取并入库的种子；种子列表和实时抓取页会显示 `V4`/`V6` 来源标记。
- 支持每日定时运行爬虫，可配置开始和停止时间，并支持 `22:00-07:00` 这类跨午夜时段。
- 配置爬虫线程数、请求速率、并发元数据下载数和自动停止时间。
- 限制数据库中保存的种子数量，自动清理最旧记录。
- 设置订阅条件，在匹配种子出现时发送通知。
- 支持 Telegram、Discord、Slack 和 Gotify 通知。
- 可将磁力链接发送到 Transmission、Aria2、Deluge 或 qBittorrent。

### 界面与集成

- 基于 Tailwind CSS 4 和 DaisyUI 5 的响应式界面。
- 支持简体中文、英文和多种界面主题。
- 提供仪表盘、采集趋势和种子分类统计。
- 提供 REST API 和 MCP 接口，便于外部工具调用。
- 支持 Basic Auth 保护 Web 界面。

### 数据库

支持以下存储后端：

- CloverDB
- SQLite（GORM）
- PostgreSQL（GORM）
- MySQL（GORM）

## 界面截图

| 仪表盘 | 搜索 |
| --- | --- |
| ![仪表盘](screenshots/dashboard.png) | ![搜索](screenshots/search.png) |

| 最新发现 | 订阅 |
| --- | --- |
| ![最新发现](screenshots/discover.png) | ![订阅](screenshots/watches.png) |

| 黑名单 | 设置 |
| --- | --- |
| ![黑名单](screenshots/blacklist.png) | ![设置](screenshots/settings.png) |

| 实时捕获 |
| --- |
| ![实时捕获](screenshots/trawl.png) |

## 部署

### 懒猫微服

仓库已包含懒猫微服应用所需的构建和清单文件：

```text
package.yml
lzc-build.yml
lzc-manifest.yml
Dockerfile.lpk
```

使用已配置懒猫开发环境的 `lzc-cli` 构建：

```shell
lzc-cli project build
```

构建产物会输出到 `dist-lpk/`。应用数据保存在 `/lzcapp/var/dhtcdb`，升级或重建容器时不会随容器层一起丢失。

### Docker Compose

```shell
docker compose up -d
```

启动后访问 [http://localhost:4200](http://localhost:4200)。

### 本地运行

需要 Node.js 20 或更高版本，以及 Go 1.25 或更高版本。

安装前端依赖并生成 CSS 与 JavaScript：

```shell
npm install
npm run build
```

启动服务：

```shell
go run ./cmd/dhtc
```

默认访问地址为 [http://localhost:4200](http://localhost:4200)。

## 常用启动参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-address` | `:4200` | Web 服务监听地址 |
| `-database` | `dhtdb` | CloverDB 数据目录或名称 |
| `-database-type` | `clover` | 数据库类型：`clover`、`sqlite`、`postgres` 或 `mysql` |
| `-database-url` | 空 | GORM 数据库连接地址 |
| `-CrawlerThreads` | `2` | DHT 爬虫线程数 |
| `-CrawlerStartOnLaunch` | `false` | 启动应用时自动开始采集 |
| `-NetworkMode` | `dual` | DHT 网络模式：`ipv4` 或 `dual` |
| `-ListenIPv4` | `0.0.0.0:0` | IPv4 DHT UDP 监听地址 |
| `-ListenIPv6` | `[::]:0` | IPv6 DHT UDP 监听地址 |
| `-RoutingTableCacheIPv4` | `routing-table-v4.json` | IPv4 路由表缓存 |
| `-RoutingTableCacheIPv6` | `routing-table-v6.json` | IPv6 路由表缓存 |
| `-config` | 空 | 可选的双栈 YAML 配置文件 |
| `-node-role` | `standalone` | 节点角色：`standalone`、`master`、`worker` |
| `-worker-urls` | 空 | Master 主动拉取的公网 Worker 地址列表 |
| `-cluster-token` | 空 | Master 与 Worker 共享的鉴权 Token |
| `-worker-id` | `worker` | Worker 唯一标识 |
| `-worker-queue` | `256` | Worker 内存待发送队列上限 |
| `-worker-batch` | `16` | Worker 单次上传数量 |
| `-CrawlerAutoStopMinutes` | `0` | 自动停止采集的分钟数，`0` 表示不自动停止 |
| `-MaxSavedTorrents` | `20000` | 最多保存的种子数量，`0` 表示不限制 |
| `-OnlyChineseContent` | `false` | 仅保存名称或文件路径包含中文字符的种子 |
| `-Statistics` | `false` | 启用统计数据采集 |
| `-auth-user` | 空 | Basic Auth 用户名 |
| `-auth-pass` | 空 | Basic Auth 密码 |

运行以下命令可查看完整参数：

```shell
go run ./cmd/dhtc -help
```

大部分运行参数也可以在 Web 设置页面中调整。部分涉及服务初始化的设置需要重启后生效。

IPv6 模式要求宿主机或容器具有可用的 IPv6 网络。按照 BEP 5 与 BEP 32，IPv4 DHT 和 IPv6 DHT 是完全独立的 Overlay：各自拥有 UDP Socket、节点 ID、KBucket 路由表、查询、Token、Bootstrap、刷新任务与持久化缓存。双栈模式会在 IPv4 查询中请求 `nodes6`，但这些地址只作为 IPv6 UDP 探测候选；只有通过 IPv6 Socket 正常响应的节点才会进入 IPv6 路由表，两套路由表不会共享或直接注入节点。

默认 `dual` 模式会通过 BEP 32 自动从内置 IPv4 Bootstrap 网络发现 IPv6 候选，再由独立 UDP6 Network 验证并加入 IPv6 路由表。整个过程由程序自动处理，不提供也不需要用户配置 IPv6 Bootstrap。项目不再提供纯 `ipv6` 模式；旧配置中的 `ipv6` 会自动迁移为 `dual`。

可选 YAML 配置示例：

```yaml
network:
  mode: dual
listen:
  ipv4: "0.0.0.0:6881"
  ipv6: "[::]:6881"
bootstrap:
  ipv4:
    - "router.bittorrent.com:6881"
routing_cache:
  ipv4: "routing-table-v4.json"
  ipv6: "routing-table-v6.json"
crawler:
  schedule:
    enabled: true
    start: "22:00"
    end: "07:00"
```

使用 `go run ./cmd/dhtc -config dhtc.yml` 启动。显式命令行参数会覆盖 YAML 中对应的网络模式、监听地址和缓存路径。

运行时由一个 Scheduler 管理 IPv4/IPv6 DHT Network。两套 Network 发现的 InfoHash 汇入同一个队列，由共享的 Metadata Downloader 去重并根据 Peer 地址自动选择 `tcp4` 或 `tcp6`，最后写入同一个数据库。InfoHash 不区分地址族，数据库结构无需修改。

## 主从部署

`master` 负责数据库、搜索、Dashboard、统计、通知，并主动从公网 Worker 拉取数据。`worker` 只运行 DHT、Metadata 下载、内存队列和带鉴权的拉取接口，不打开数据库，也不加载完整 Web UI。

该模式适用于 Worker 有公网地址、Master 没有公网入口的网络拓扑。所有集群连接均由 Master 向公网 Worker 发起，Worker 不需要访问 Master。

Worker 安装脚本默认使用 `--performance auto`，根据 VPS 的在线 CPU 数量和可用内存选择 `conservative`、`high` 或 `max` 档位。自动档位会使用 64 条传输批次，并提高下载并发、活动任务数、UDP 速率和待拉取队列；任意单项参数仍可通过 `--queue`、`--batch`、`--max-downloads`、`--max-leeches`、`--rate-limit` 覆盖。

安装时脚本会自动通过 `ufw` 或运行中的 `firewalld` 放行 Worker 监听端口（默认 `4200/tcp`）。使用云厂商安全组的 VPS 仍需在控制台放行该端口；若端口由其他方式管理，可传入 `--no-open-firewall` 跳过本机防火墙配置。

```shell
sudo ./scripts/install-worker.sh --performance auto
```

资源充足且希望强制最高档时使用 `--performance max`；小型 VPS 可使用 `--performance conservative`。

Master 示例：

```shell
dhtc \
  -node-role master \
  -worker-urls "https://worker-01.example.com:4200,https://worker-02.example.com:4200" \
  -cluster-token "change-me" \
  -database-type postgres \
  -database-url "postgres://..."
```

低资源 Worker 示例：

```shell
dhtc \
  -cluster-token "change-me" \
  -worker-id "edge-01" \
  -MaxConcurrentDownloads 2 \
  -MaxLeeches 32 \
  -worker-queue 256 \
  -worker-batch 16
```

Worker 最低建议配置为 `1 vCPU、512 MB 内存、256 MB 可用磁盘`。磁盘主要保存 IPv4/IPv6 路由表缓存；待发送 Metadata 当前使用有界内存队列，不写本地种子数据库。更稳定的配置为 `1 vCPU、1 GB 内存`。

Master 每 5 秒主动拉取一次。Worker 只有在收到 Master 的确认后才删除记录；拉取或确认中断时，同一记录会再次出现，由 Master 的 InfoHash 唯一约束保证幂等。队列满后 Worker 拒绝新数据并记录丢弃数量，避免低配节点内存持续增长。Worker 健康接口 `/health` 会返回当前排队数和丢弃数。本阶段尚未实现磁盘队列，因此 Worker 重启会丢失尚未被 Master 确认的数据。

Master GUI 的顶级导航包含 `/workers` 管理页面，与仪表盘同级。页面每 5 秒刷新，显示每个 Worker 的在线状态、Worker ID、公网 URL、待拉取数、累计拉取数、丢弃数、最后成功时间和最近错误。空队列但连接正常的 Worker 仍显示在线。

### Worker 单文件构建

Worker 不需要数据库、配置文件、Bootstrap 文件或 Web 静态资源目录。构建静态 Linux 单文件：

```shell
./scripts/build-worker.sh
```

输出文件默认为 `dist/dhtc-worker-linux-amd64`。部署时只需要这个二进制：

```shell
./dhtc-worker-linux-amd64 \
  -address 0.0.0.0:4200 \
  -cluster-token "change-me" \
  -worker-id "edge-01"
```

生产环境应通过防火墙只允许 Master 的出口 IP 访问 Worker 的 TCP `4200`，并为该接口配置 HTTPS。DHT 仍需要宿主机允许 UDP4/UDP6 出入站。

名称包含 `dhtc-worker` 的发布二进制默认自动进入 `worker` 角色，不启动 GUI，也不需要传入 `-node-role worker`。它只提供 `/health` 和带 Token 鉴权的 `/api/worker/v1/queue`。

### VPS 一键安装 Worker

Debian、Ubuntu、Rocky Linux、AlmaLinux 等使用 systemd 的 VPS 可以直接执行：

```shell
curl -fsSL https://raw.githubusercontent.com/zerokaze420/lazy-dhtc/master/scripts/install-worker.sh -o install-worker.sh
chmod +x install-worker.sh
sudo ./install-worker.sh
```

脚本会自动生成 Worker ID 和 256 位随机 Cluster Token，自动识别 `amd64/arm64`，下载最新 GitHub Release，校验 SHA256，创建低权限用户、路由表数据目录、Token 环境文件和 systemd 服务，并立即启动。安装结束会自动探测公网 IPv4/IPv6，并打印 `Worker URLs`、`Worker ID` 和 `Cluster Token`，可直接填写到 Master GUI。查看全部参数：

```shell
./install-worker.sh --help
```

常用低配参数示例：

```shell
sudo ./install-worker.sh \
  --worker-id 'tokyo-vps-01' \
  --address '0.0.0.0:4200' \
  --queue 128 \
  --batch 8 \
  --max-downloads 1 \
  --max-leeches 16 \
  --rate-limit 50
```

安装完成后的管理命令：

```shell
systemctl status dhtc-worker
journalctl -u dhtc-worker -f
systemctl restart dhtc-worker
curl http://127.0.0.1:4200/health
```

重新运行安装脚本即可升级二进制并重建服务配置；未显式传入 `--worker-id` 或 `--token` 时会复用 `/etc/dhtc-worker.env` 中原有值，不会破坏 Master 配置。公网防火墙应只允许 Master 的出口 IP 访问 Worker TCP `4200`；DHT 使用的 UDP 流量仍需允许出入站。

Master 支持通过 IPv4、IPv6 和 AAAA 域名连接 Worker。直接填写 IPv6 时必须使用标准 URL 方括号格式，例如 `http://[2001:db8::1]:4200`。Master 所在网络仍需具备对应的 IPv6出站连接能力。

## MCP 接口

MCP 端点位于：

```text
GET /mcp
POST /mcp
```

当前提供以下工具：

- `search_torrents`：搜索已收录的种子元数据。
- `latest_torrents`：读取最新收录的种子。
- `torrent_stats`：读取采集统计和种子总数。
- `category_distribution`：读取种子分类分布。

懒猫微服构建还会导出 `resources/mcp-providers/dhtc/mcp.yml` 中定义的 MCP Provider。

## 开发验证

```shell
go test ./...
npm run build
```

## 上游与许可证

- 本项目仓库：[zerokaze420/lazy-dhtc](https://github.com/zerokaze420/lazy-dhtc)
- 上游项目：[nbdy/dhtc](https://github.com/nbdy/dhtc)
- 许可证：[MIT](LICENSE)

本项目保留上游项目的许可证和版权声明。
