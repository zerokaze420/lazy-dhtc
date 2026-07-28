#!/usr/bin/env sh
set -eu

REPO="zerokaze420/lazy-dhtc"
VERSION="latest"
ADDRESS="0.0.0.0:4200"
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
INSTALL_DIR="/usr/local/bin"
DATA_DIR="/var/lib/dhtc-worker"
SERVICE_USER="dhtc-worker"
SERVICE_NAME="dhtc-worker"
OPEN_FIREWALL=true

usage() {
  cat <<'EOF'
Install dhtc Worker as a systemd service.
Running the installer again upgrades or repairs the existing installation.

Usage:
  sudo ./install-worker.sh [options]

Connection:
  --address HOST:PORT        Public Worker HTTP listen address. Default: 0.0.0.0:4200
  --worker-id ID             Worker name shown in Master GUI. Default: generated and persisted
  --token TOKEN              Shared Master token. Default: generated and persisted
  --network-mode MODE        DHT mode: dual or ipv4. Default: dual

Resource limits:
  --performance PROFILE      auto, high, max or conservative. Default: auto
  --queue N                  Maximum in-memory metadata queue. Default: selected automatically
  --batch N                  Maximum records returned per Master pull. Default: 64
  --max-downloads N          Concurrent metadata downloads. Default: selected automatically
  --max-leeches N            Active metadata tasks. Default: selected automatically
  --rate-limit N             UDP packets per second per DHT network. Default: selected automatically

Installation:
  --version VERSION          Release tag such as v1.0.13, or latest. Default: latest
  --install-dir PATH         Binary directory. Default: /usr/local/bin
  --data-dir PATH            Routing-table cache directory. Default: /var/lib/dhtc-worker
  --service-user USER        Unprivileged system account. Default: dhtc-worker
  --service-name NAME        systemd unit name. Default: dhtc-worker
  --no-open-firewall         Do not add a local firewall rule for the Worker TCP port
  -h, --help                 Show this help.

Examples:
  sudo ./install-worker.sh

  sudo ./install-worker.sh \
    --worker-id tokyo-vps-01 \
    --address 0.0.0.0:4200 \
    --performance max

After installation:
  systemctl status dhtc-worker
  journalctl -u dhtc-worker -f
  curl http://127.0.0.1:4200/health

Reinstall or upgrade:
  sudo ./install-worker.sh
  Existing Worker ID and cluster token are preserved unless explicitly replaced.

Master configuration:
  dhtc -node-role master \
    -worker-urls 'https://worker.example.com:4200' \
    -cluster-token 'token printed by this installer'

Security:
  Expose the Worker port only to the Master's public egress IP when possible.
  Put HTTPS in front of the Worker API when traffic crosses the public Internet.
EOF
}

need_value() {
  if [ "$#" -lt 2 ] || [ -z "$2" ]; then
    echo "Missing value for $1" >&2
    exit 2
  fi
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --token) need_value "$@"; TOKEN="$2"; TOKEN_EXPLICIT=true; shift 2 ;;
    --address) need_value "$@"; ADDRESS="$2"; shift 2 ;;
    --worker-id) need_value "$@"; WORKER_ID="$2"; WORKER_ID_EXPLICIT=true; shift 2 ;;
    --network-mode) need_value "$@"; NETWORK_MODE="$2"; shift 2 ;;
    --performance) need_value "$@"; PERFORMANCE="$2"; shift 2 ;;
    --queue) need_value "$@"; QUEUE="$2"; shift 2 ;;
    --batch) need_value "$@"; BATCH="$2"; shift 2 ;;
    --max-downloads) need_value "$@"; MAX_DOWNLOADS="$2"; shift 2 ;;
    --max-leeches) need_value "$@"; MAX_LEECHES="$2"; shift 2 ;;
    --rate-limit) need_value "$@"; RATE_LIMIT="$2"; shift 2 ;;
    --version) need_value "$@"; VERSION="$2"; shift 2 ;;
    --install-dir) need_value "$@"; INSTALL_DIR="$2"; shift 2 ;;
    --data-dir) need_value "$@"; DATA_DIR="$2"; shift 2 ;;
    --service-user) need_value "$@"; SERVICE_USER="$2"; shift 2 ;;
    --service-name) need_value "$@"; SERVICE_NAME="$2"; shift 2 ;;
    --no-open-firewall) OPEN_FIREWALL=false; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

case "$SERVICE_USER" in ''|*[!A-Za-z0-9_-]*) echo "--service-user contains invalid characters" >&2; exit 2 ;; esac
case "$SERVICE_NAME" in ''|*[!A-Za-z0-9_.@-]*) echo "--service-name contains invalid characters" >&2; exit 2 ;; esac
case "$INSTALL_DIR:$DATA_DIR" in *' '*|*'\t'*) echo "Installation paths cannot contain whitespace" >&2; exit 2 ;; esac
case "$NETWORK_MODE" in dual|ipv4) ;; *) echo "--network-mode must be dual or ipv4" >&2; exit 2 ;; esac
case "$PERFORMANCE" in auto|high|max|conservative) ;; *) echo "--performance must be auto, high, max or conservative" >&2; exit 2 ;; esac
case "$ADDRESS" in *:*) ;; *) echo "--address must be HOST:PORT" >&2; exit 2 ;; esac
PORT="${ADDRESS##*:}"
case "$PORT" in ''|*[!0-9]*) echo "--address port must be numeric" >&2; exit 2 ;; esac
[ "$PORT" -ge 1 ] && [ "$PORT" -le 65535 ] || { echo "--address port must be between 1 and 65535" >&2; exit 2; }

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
  case "$value" in ''|*[!0-9]*) echo "Resource limits must be positive integers" >&2; exit 2 ;; esac
  [ "$value" -gt 0 ] || { echo "Resource limits must be greater than zero" >&2; exit 2; }
done
[ "$BATCH" -le 64 ] || { echo "--batch cannot exceed 64" >&2; exit 2; }

for command in awk curl id install mktemp mv od sha256sum systemctl tr uname useradd; do
  command -v "$command" >/dev/null 2>&1 || { echo "Required command not found: $command" >&2; exit 1; }
done

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

if [ "$(id -u)" -ne 0 ]; then
  echo "Run this installer as root (for example with sudo)." >&2
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
  echo "Generated a new cluster token."
fi
if [ -z "$WORKER_ID" ]; then
  WORKER_ID="worker-$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')"
  echo "Generated Worker ID: $WORKER_ID"
fi
case "$TOKEN" in *[!A-Za-z0-9._~+/=-]*) echo "--token contains unsupported characters" >&2; exit 2 ;; esac
case "$WORKER_ID" in ''|*[!A-Za-z0-9._-]*) echo "--worker-id may contain only letters, numbers, dot, underscore and dash" >&2; exit 2 ;; esac

ASSET="dhtc-worker-linux-${ARCH}"
if [ "$VERSION" = "latest" ]; then
  BASE_URL="https://github.com/${REPO}/releases/latest/download"
else
  BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
fi
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

echo "Downloading ${ASSET} (${VERSION})..."
curl -fL --retry 3 -o "$TMP_DIR/$ASSET" "$BASE_URL/$ASSET"
curl -fL --retry 3 -o "$TMP_DIR/SHA256SUMS-linux.txt" "$BASE_URL/SHA256SUMS-linux.txt"
EXPECTED="$(awk -v asset="$ASSET" '$2 == asset { print $1 }' "$TMP_DIR/SHA256SUMS-linux.txt")"
ACTUAL="$(sha256sum "$TMP_DIR/$ASSET" | awk '{ print $1 }')"
if [ -z "$EXPECTED" ] || [ "$EXPECTED" != "$ACTUAL" ]; then
  echo "SHA256 verification failed" >&2
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
ExecStart=${INSTALL_DIR}/dhtc-worker -address ${ADDRESS} -NetworkMode ${NETWORK_MODE} -worker-queue ${QUEUE} -worker-batch ${BATCH} -MaxConcurrentDownloads ${MAX_DOWNLOADS} -MaxLeeches ${MAX_LEECHES} -RateLimit ${RATE_LIMIT} -RoutingTableCacheIPv4 ${DATA_DIR}/routing-table-v4.json -RoutingTableCacheIPv6 ${DATA_DIR}/routing-table-v6.json
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

FIREWALL_STATUS="not managed"
if [ "$OPEN_FIREWALL" = true ]; then
  if command -v ufw >/dev/null 2>&1; then
    ufw allow "${PORT}/tcp"
    FIREWALL_STATUS="ufw allows ${PORT}/tcp"
  elif command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active --quiet firewalld; then
    firewall-cmd --permanent --add-port="${PORT}/tcp"
    firewall-cmd --reload
    FIREWALL_STATUS="firewalld allows ${PORT}/tcp"
  else
    FIREWALL_STATUS="no active ufw/firewalld detected"
  fi
else
  FIREWALL_STATUS="skipped by --no-open-firewall"
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
echo "Installed successfully."
echo "  Binary:  ${INSTALL_DIR}/dhtc-worker"
echo "  Data:    ${DATA_DIR}"
echo "  Service: ${SERVICE_NAME}.service"
echo "  Listen:  ${ADDRESS}"
echo "  Firewall: ${FIREWALL_STATUS}"
echo "  Performance: ${SELECTED_PROFILE} (${CPU_COUNT} CPU, ${MEM_MB} MiB available memory)"
echo "  Limits: queue=${QUEUE}, batch=${BATCH}, downloads=${MAX_DOWNLOADS}, leeches=${MAX_LEECHES}, rate=${RATE_LIMIT}"
echo
echo "Master connection settings:"
echo "  Worker ID:     ${WORKER_ID}"
echo "  Worker URLs:   ${WORKER_URLS}"
echo "  Cluster Token: ${TOKEN}"
echo
echo "Add Worker URLs and Cluster Token to the Master's Worker settings."
echo "Also allow TCP port ${PORT} in the VPS provider security group when one is enabled."
echo "Check status: systemctl status ${SERVICE_NAME}"
echo "Follow logs:  journalctl -u ${SERVICE_NAME} -f"
