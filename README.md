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
| `-BootstrapNodeFileIPv6` | `bootstrap-nodes6.txt` | IPv6 DHT Bootstrap 节点文件 |
| `-BootstrapNodesIPv6` | 空 | 逗号、空格或换行分隔的 IPv6 Bootstrap 节点 |
| `-RoutingTableCacheIPv4` | `routing-table-v4.json` | IPv4 路由表缓存 |
| `-RoutingTableCacheIPv6` | `routing-table-v6.json` | IPv6 路由表缓存 |
| `-config` | 空 | 可选的双栈 YAML 配置文件 |
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

常用 Mainline DHT Bootstrap 域名通常没有 AAAA 记录，因此首次运行 IPv6-only 模式时，应在 `bootstrap-nodes6.txt` 或 YAML 的 `bootstrap.ipv6` 中提供可用的 IPv6 Mainline DHT 节点。支持 AAAA 域名和 `[IPv6]:端口` 格式。Bootstrap 失败不会阻止程序运行；程序会先恢复 IPv6 路由表缓存，仅在缓存节点不足时继续尝试 Bootstrap。

默认 `dual` 模式会通过 BEP 32 自动从内置 IPv4 Bootstrap 网络发现 IPv6 候选，无需用户填写。设置页面的“IPv6 Bootstrap 节点”仅作为可选补充。项目不再提供纯 `ipv6` 模式；旧配置中的 `ipv6` 会自动迁移为 `dual`。

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
  ipv6:
    - "[2001:db8::1]:6881"
routing_cache:
  ipv4: "routing-table-v4.json"
  ipv6: "routing-table-v6.json"
```

使用 `go run ./cmd/dhtc -config dhtc.yml` 启动。显式命令行参数会覆盖 YAML 中对应的网络模式、监听地址和缓存路径。

运行时由一个 Scheduler 管理 IPv4/IPv6 DHT Network。两套 Network 发现的 InfoHash 汇入同一个队列，由共享的 Metadata Downloader 去重并根据 Peer 地址自动选择 `tcp4` 或 `tcp6`，最后写入同一个数据库。InfoHash 不区分地址族，数据库结构无需修改。

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
