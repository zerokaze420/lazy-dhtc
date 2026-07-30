#!/usr/bin/env sh
set -eu

REPO="zerokaze420/lazy-dhtc"
VERSION="latest"
ADDRESS="0.0.0.0:4200"
DHT_PORT="6881"
WORKER_ID=""
TOKEN=""
WORKER_ID_EXPLICIT=false
TOKEN_EXPLICIT=false
NETWORK_MODE="dual"
PERFORMANCE="auto"
QUEUE=""
BATCH=""
MAX_DOWNLOADS=""
MAX_LEECHES=""
RATE_LIMIT=""
MASTER_URL=""
INSTALL_DIR="/usr/local/bin"
DATA_DIR="/var/lib/dhtc-worker"
SERVICE_USER="dhtc-worker"
SERVICE_NAME="dhtc-worker"
OPEN_FIREWALL=true

usage() {
  cat <<'EOF'
dhtc Worker 一键安装脚本
重复运行可覆盖升级或修复现有安装。

用法：
  sudo ./install-worker.sh [options]

连接选项：
  --address HOST:PORT        Worker HTTP 监听地址，默认：0.0.0.0:4200
  --dht-port PORT            IPv4/IPv6 DHT UDP 监听端口，默认：6881
  --worker-id ID             Master 界面显示的 Worker 名称，默认：自动生成并保留
  --token TOKEN              Master 与 Worker 共享密钥，默认：自动生成并保留
  --network-mode MODE        DHT 网络模式：dual 或 ipv4，默认：dual

性能选项：
  --performance PROFILE      性能档位：auto、high、max、conservative，默认：auto
  --queue N                  内存待拉取队列上限，默认：自动选择
  --batch N                  Master 单次拉取数量，默认：64
  --max-downloads N          元数据下载并发数，默认：自动选择
  --max-leeches N            活跃元数据任务数，默认：自动选择
  --rate-limit N             每个 DHT 网络每秒 UDP 包上限，默认：自动选择
  --master-url URL           启用 Worker 主动推送模式，填写 Master 地址

安装选项：
  --version VERSION          发布版本，例如 v1.0.13 或 latest，默认：latest
  --install-dir PATH         二进制安装目录，默认：/usr/local/bin
  --data-dir PATH            路由表缓存目录，默认：/var/lib/dhtc-worker
  --service-user USER        服务运行用户，默认：dhtc-worker
  --service-name NAME        systemd 服务名称，默认：dhtc-worker
  --no-open-firewall         不自动放行 Worker TCP 和 DHT UDP 端口
  -h, --help                 显示帮助

示例：
  sudo ./install-worker.sh

  sudo ./install-worker.sh \
    --worker-id tokyo-vps-01 \
    --address 0.0.0.0:4200 \
    --performance max

安装后检查：
  systemctl status dhtc-worker
  journalctl -u dhtc-worker -f
  curl http://127.0.0.1:4200/health

覆盖安装或升级：
  sudo ./install-worker.sh
  默认保留已有 Worker ID 和集群密钥，显式传参时才会替换。

Master 配置示例：
  dhtc -node-role master \
    -worker-urls 'https://worker.example.com:4200' \
    -cluster-token '安装脚本输出的集群密钥'

安全建议：
  尽量只允许 Master 的公网出口 IP 访问 Worker 端口。
  跨公网传输时建议为 Worker API 配置 HTTPS。
EOF
}

need_value() {
  if [ "$#" -lt 2 ] || [ -z "$2" ]; then
    echo "错误：参数 $1 缺少值" >&2
    exit 2
  fi
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --token) need_value "$@"; TOKEN="$2"; TOKEN_EXPLICIT=true; shift 2 ;;
    --address) need_value "$@"; ADDRESS="$2"; shift 2 ;;
    --dht-port) need_value "$@"; DHT_PORT="$2"; shift 2 ;;
    --worker-id) need_value "$@"; WORKER_ID="$2"; WORKER_ID_EXPLICIT=true; shift 2 ;;
    --network-mode) need_value "$@"; NETWORK_MODE="$2"; shift 2 ;;
    --performance) need_value "$@"; PERFORMANCE="$2"; shift 2 ;;
    --queue) need_value "$@"; QUEUE="$2"; shift 2 ;;
    --batch) need_value "$@"; BATCH="$2"; shift 2 ;;
    --max-downloads) need_value "$@"; MAX_DOWNLOADS="$2"; shift 2 ;;
    --max-leeches) need_value "$@"; MAX_LEECHES="$2"; shift 2 ;;
    --rate-limit) need_value "$@"; RATE_LIMIT="$2"; shift 2 ;;
    --master-url) need_value "$@"; MASTER_URL="$2"; shift 2 ;;
    --version) need_value "$@"; VERSION="$2"; shift 2 ;;
    --install-dir) need_value "$@"; INSTALL_DIR="$2"; shift 2 ;;
    --data-dir) need_value "$@"; DATA_DIR="$2"; shift 2 ;;
    --service-user) need_value "$@"; SERVICE_USER="$2"; shift 2 ;;
    --service-name) need_value "$@"; SERVICE_NAME="$2"; shift 2 ;;
    --no-open-firewall) OPEN_FIREWALL=false; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "错误：未知参数 $1" >&2; usage >&2; exit 2 ;;
  esac
done

case "$SERVICE_USER" in ''|*[!A-Za-z0-9_-]*) echo "错误：--service-user 包含无效字符" >&2; exit 2 ;; esac
case "$SERVICE_NAME" in ''|*[!A-Za-z0-9_.@-]*) echo "错误：--service-name 包含无效字符" >&2; exit 2 ;; esac
case "$INSTALL_DIR:$DATA_DIR" in *' '*|*'\t'*) echo "错误：安装路径不能包含空白字符" >&2; exit 2 ;; esac
case "$NETWORK_MODE" in dual|ipv4) ;; *) echo "错误：--network-mode 只能是 dual 或 ipv4" >&2; exit 2 ;; esac
case "$PERFORMANCE" in auto|high|max|conservative) ;; *) echo "错误：--performance 只能是 auto、high、max 或 conservative" >&2; exit 2 ;; esac
case "$MASTER_URL" in ""|http://*|https://*) ;; *) echo "错误：--master-url 必须是 http:// 或 https:// URL" >&2; exit 2 ;; esac
case "$MASTER_URL" in *' '*|*'\t'*) echo "错误：--master-url 不能包含空白字符" >&2; exit 2 ;; esac
case "$ADDRESS" in *:*) ;; *) echo "错误：--address 必须使用 HOST:PORT 格式" >&2; exit 2 ;; esac
PORT="${ADDRESS##*:}"
case "$PORT" in ''|*[!0-9]*) echo "错误：--address 的端口必须是数字" >&2; exit 2 ;; esac
[ "$PORT" -ge 1 ] && [ "$PORT" -le 65535 ] || { echo "错误：端口必须在 1 到 65535 之间" >&2; exit 2; }
case "$DHT_PORT" in ''|*[!0-9]*) echo "错误：--dht-port 必须是数字" >&2; exit 2 ;; esac
[ "$DHT_PORT" -ge 1 ] && [ "$DHT_PORT" -le 65535 ] || { echo "错误：DHT 端口必须在 1 到 65535 之间" >&2; exit 2; }

CPU_COUNT="$(getconf _NPROCESSORS_ONLN 2>/dev/null || true)"
case "$CPU_COUNT" in ''|*[!0-9]*) CPU_COUNT=1 ;; esac
[ "$CPU_COUNT" -gt 0 ] || CPU_COUNT=1
MEM_KB="$(awk '/^MemAvailable:/ { available=$2 } /^MemTotal:/ { total=$2 } END { print available ? available : total }' /proc/meminfo 2>/dev/null || true)"
case "$MEM_KB" in ''|*[!0-9]*) MEM_KB=524288 ;; esac
MEM_MB=$((MEM_KB / 1024))

SELECTED_PROFILE="$PERFORMANCE"
if [ "$SELECTED_PROFILE" = auto ]; then
  if [ "$CPU_COUNT" -ge 8 ] && [ "$MEM_MB" -ge 8192 ]; then
    SELECTED_PROFILE="max"
  elif [ "$CPU_COUNT" -ge 2 ] && [ "$MEM_MB" -ge 1536 ]; then
    SELECTED_PROFILE="high"
  else
    SELECTED_PROFILE="conservative"
  fi
fi

case "$SELECTED_PROFILE" in
  conservative)
    DEFAULT_QUEUE=2048; DEFAULT_BATCH=64; DEFAULT_DOWNLOADS=8; DEFAULT_LEECHES=128; DEFAULT_RATE=200
    ;;
  high)
    DEFAULT_QUEUE=8192; DEFAULT_BATCH=64
    DEFAULT_DOWNLOADS=$((CPU_COUNT * 8)); [ "$DEFAULT_DOWNLOADS" -le 48 ] || DEFAULT_DOWNLOADS=48
    DEFAULT_LEECHES=$((DEFAULT_DOWNLOADS * 16)); DEFAULT_RATE=$((CPU_COUNT * 150)); [ "$DEFAULT_RATE" -le 1000 ] || DEFAULT_RATE=1000
    ;;
  max)
    DEFAULT_QUEUE=16384; DEFAULT_BATCH=64
    DEFAULT_DOWNLOADS=$((CPU_COUNT * 12)); [ "$DEFAULT_DOWNLOADS" -le 96 ] || DEFAULT_DOWNLOADS=96
    DEFAULT_LEECHES=$((DEFAULT_DOWNLOADS * 16)); DEFAULT_RATE=$((CPU_COUNT * 250)); [ "$DEFAULT_RATE" -le 2000 ] || DEFAULT_RATE=2000
    ;;
esac

QUEUE="${QUEUE:-$DEFAULT_QUEUE}"
BATCH="${BATCH:-$DEFAULT_BATCH}"
MAX_DOWNLOADS="${MAX_DOWNLOADS:-$DEFAULT_DOWNLOADS}"
MAX_LEECHES="${MAX_LEECHES:-$DEFAULT_LEECHES}"
RATE_LIMIT="${RATE_LIMIT:-$DEFAULT_RATE}"

for value in "$QUEUE" "$BATCH" "$MAX_DOWNLOADS" "$MAX_LEECHES" "$RATE_LIMIT"; do
  case "$value" in ''|*[!0-9]*) echo "错误：性能参数必须是正整数" >&2; exit 2 ;; esac
  [ "$value" -gt 0 ] || { echo "错误：性能参数必须大于零" >&2; exit 2; }
done
[ "$BATCH" -le 64 ] || { echo "错误：--batch 不能超过 64" >&2; exit 2; }

for command in awk curl id install mktemp mv od sha256sum systemctl tr uname useradd; do
  command -v "$command" >/dev/null 2>&1 || { echo "错误：缺少必要命令 $command" >&2; exit 1; }
done

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "错误：不支持的处理器架构 $ARCH" >&2; exit 1 ;;
esac

if [ "$(id -u)" -ne 0 ]; then
  echo "错误：请使用 root 权限运行，例如 sudo ./install-worker.sh" >&2
  exit 1
fi

ENV_FILE="/etc/${SERVICE_NAME}.env"
if [ -f "$ENV_FILE" ]; then
  if [ "$TOKEN_EXPLICIT" = false ]; then
    TOKEN="$(awk -F= '$1 == "DHTC_CLUSTER_TOKEN" { sub(/^[^=]*=/, ""); print; exit }' "$ENV_FILE")"
  fi
  if [ "$WORKER_ID_EXPLICIT" = false ]; then
    WORKER_ID="$(awk -F= '$1 == "DHTC_WORKER_ID" { sub(/^[^=]*=/, ""); print; exit }' "$ENV_FILE")"
  fi
fi
if [ -z "$TOKEN" ]; then
  TOKEN="$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
  echo "已生成新的集群密钥。"
fi
if [ -z "$WORKER_ID" ]; then
  WORKER_ID="worker-$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')"
  echo "已生成 Worker ID：$WORKER_ID"
fi
case "$TOKEN" in *[!A-Za-z0-9._~+/=-]*) echo "错误：--token 包含不支持的字符" >&2; exit 2 ;; esac
case "$WORKER_ID" in ''|*[!A-Za-z0-9._-]*) echo "错误：--worker-id 只能包含字母、数字、点、下划线和短横线" >&2; exit 2 ;; esac

ASSET="dhtc-worker-linux-${ARCH}"
if [ "$VERSION" = "latest" ]; then
	LATEST_URL="$(curl -fsSL --retry 3 -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")"
	RESOLVED_VERSION="$(printf '%s\n' "$LATEST_URL" | awk -F'/tag/' 'NF > 1 { print $2 }')"
	if [ -z "$RESOLVED_VERSION" ]; then
		echo "错误：无法解析 GitHub 最新版本号" >&2
		exit 1
	fi
else
	RESOLVED_VERSION="$VERSION"
fi
BASE_URL="https://github.com/${REPO}/releases/download/${RESOLVED_VERSION}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

echo "准备下载版本：${RESOLVED_VERSION}（${ASSET}）"
curl -fL --retry 3 -o "$TMP_DIR/$ASSET" "$BASE_URL/$ASSET"
curl -fL --retry 3 -o "$TMP_DIR/SHA256SUMS-linux.txt" "$BASE_URL/SHA256SUMS-linux.txt"
EXPECTED="$(awk -v asset="$ASSET" '$2 == asset { print $1 }' "$TMP_DIR/SHA256SUMS-linux.txt")"
ACTUAL="$(sha256sum "$TMP_DIR/$ASSET" | awk '{ print $1 }')"
if [ -z "$EXPECTED" ] || [ "$EXPECTED" != "$ACTUAL" ]; then
  echo "错误：SHA256 校验失败" >&2
  exit 1
fi

if ! id "$SERVICE_USER" >/dev/null 2>&1; then
  NOLOGIN="$(command -v nologin || true)"
  [ -n "$NOLOGIN" ] || NOLOGIN="/sbin/nologin"
  useradd --system --home-dir "$DATA_DIR" --shell "$NOLOGIN" "$SERVICE_USER"
fi
install -d -m 0750 -o "$SERVICE_USER" -g "$SERVICE_USER" "$DATA_DIR"
install -d -m 0755 "$INSTALL_DIR"
install -m 0755 "$TMP_DIR/$ASSET" "$INSTALL_DIR/.dhtc-worker.new"
mv -f "$INSTALL_DIR/.dhtc-worker.new" "$INSTALL_DIR/dhtc-worker"

umask 077
printf 'DHTC_CLUSTER_TOKEN=%s\nDHTC_WORKER_ID=%s\n' "$TOKEN" "$WORKER_ID" > "$ENV_FILE"
chmod 0600 "$ENV_FILE"

UNIT_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
PUSH_ARGS=""
if [ -n "$MASTER_URL" ]; then
  PUSH_ARGS="-master-url ${MASTER_URL} -worker-cache-dir ${DATA_DIR}/queue"
fi
cat > "$UNIT_FILE" <<EOF
[Unit]
Description=dhtc headless DHT Worker
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
WorkingDirectory=${DATA_DIR}
EnvironmentFile=${ENV_FILE}
ExecStart=${INSTALL_DIR}/dhtc-worker -address ${ADDRESS} -NetworkMode ${NETWORK_MODE} -ListenIPv4 0.0.0.0:${DHT_PORT} -ListenIPv6 [::]:${DHT_PORT} -worker-queue ${QUEUE} -worker-batch ${BATCH} -MaxConcurrentDownloads ${MAX_DOWNLOADS} -MaxLeeches ${MAX_LEECHES} -DrainTimeout 12s -RateLimit ${RATE_LIMIT} -RoutingTableCacheIPv4 ${DATA_DIR}/routing-table-v4.json -RoutingTableCacheIPv6 ${DATA_DIR}/routing-table-v6.json ${PUSH_ARGS}
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${DATA_DIR}
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "$SERVICE_NAME"
systemctl restart "$SERVICE_NAME"

FIREWALL_STATUS="未管理"
if [ "$OPEN_FIREWALL" = true ]; then
  if command -v ufw >/dev/null 2>&1; then
    ufw allow "${PORT}/tcp"
    ufw allow "${DHT_PORT}/udp"
    FIREWALL_STATUS="ufw 已放行 ${PORT}/tcp、${DHT_PORT}/udp"
  elif command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active --quiet firewalld; then
    firewall-cmd --permanent --add-port="${PORT}/tcp"
    firewall-cmd --permanent --add-port="${DHT_PORT}/udp"
    firewall-cmd --reload
    FIREWALL_STATUS="firewalld 已放行 ${PORT}/tcp、${DHT_PORT}/udp"
  else
    FIREWALL_STATUS="未检测到启用的 ufw/firewalld"
  fi
else
  FIREWALL_STATUS="已按 --no-open-firewall 跳过"
fi

PUBLIC_IPV4="$(curl -4 -fsS --max-time 5 https://api.ipify.org 2>/dev/null || true)"
PUBLIC_IPV6="$(curl -6 -fsS --max-time 5 https://api6.ipify.org 2>/dev/null || true)"
WORKER_URLS=""
if [ -n "$PUBLIC_IPV4" ]; then
  WORKER_URLS="http://${PUBLIC_IPV4}:${PORT}"
fi
if [ -n "$PUBLIC_IPV6" ]; then
  IPV6_URL="http://[${PUBLIC_IPV6}]:${PORT}"
  if [ -n "$WORKER_URLS" ]; then
    WORKER_URLS="${WORKER_URLS},${IPV6_URL}"
  else
    WORKER_URLS="$IPV6_URL"
  fi
fi
if [ -z "$WORKER_URLS" ]; then
  WORKER_URLS="http://<VPS_PUBLIC_IP>:${PORT}"
fi

echo
echo "┌─ dhtc Worker 安装完成"
echo "│"
echo "├─ 安装信息"
printf '│  下载版本     │ %s\n' "${RESOLVED_VERSION}"
printf '│  二进制       │ %s\n' "${INSTALL_DIR}/dhtc-worker"
printf '│  数据目录     │ %s\n' "${DATA_DIR}"
printf '│  systemd 服务 │ %s.service\n' "${SERVICE_NAME}"
printf '│  监听地址     │ %s\n' "${ADDRESS}"
printf '│  DHT UDP 端口 │ %s\n' "${DHT_PORT}"
printf '│  防火墙       │ %s\n' "${FIREWALL_STATUS}"
echo "│"
echo "├─ 性能配置"
printf '│  性能档位     │ %s（%s 核 CPU，%s MiB 可用内存）\n' "$SELECTED_PROFILE" "$CPU_COUNT" "$MEM_MB"
printf '│  队列 / 批量  │ %s / %s\n' "$QUEUE" "$BATCH"
printf '│  下载 / 任务  │ %s / %s\n' "$MAX_DOWNLOADS" "$MAX_LEECHES"
printf '│  UDP 速率     │ %s 包/秒/网络\n' "$RATE_LIMIT"
if [ -n "$MASTER_URL" ]; then
  printf '│  推送模式     │ %s\n' "$MASTER_URL"
else
  printf '│  传输模式     │ Master 主动拉取\n'
fi
echo "│"
echo "├─ Master 连接信息"
printf '│  Worker ID    │ %s\n' "$WORKER_ID"
printf '│  Worker URL   │ %s\n' "$WORKER_URLS"
printf '│  集群密钥     │ %s\n' "$TOKEN"
echo "│"
echo "└─ 安装成功，服务已启动"
echo
echo "请将 Worker URL 和集群密钥填写到 Master 的 Worker 设置中。"
echo "如 VPS 启用了云安全组，还需在云平台控制台放行 ${PORT}/tcp 和 ${DHT_PORT}/udp（IPv4/IPv6）。"
echo "查看状态：systemctl status ${SERVICE_NAME}"
echo "实时日志：journalctl -u ${SERVICE_NAME} -f"
