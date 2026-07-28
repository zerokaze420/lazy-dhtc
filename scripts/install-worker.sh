#!/usr/bin/env sh
set -eu

REPO="zerokaze420/lazy-dhtc"
VERSION="latest"
ADDRESS="0.0.0.0:4200"
WORKER_ID="$(hostname 2>/dev/null || echo worker)"
TOKEN=""
NETWORK_MODE="dual"
QUEUE=256
BATCH=16
MAX_DOWNLOADS=2
MAX_LEECHES=32
RATE_LIMIT=100
INSTALL_DIR="/usr/local/bin"
DATA_DIR="/var/lib/dhtc-worker"
SERVICE_USER="dhtc-worker"
SERVICE_NAME="dhtc-worker"

usage() {
  cat <<'EOF'
Install dhtc Worker as a systemd service.

Usage:
  sudo ./install-worker.sh --token TOKEN [options]

Required:
  --token TOKEN              Shared token. Must match Master's -cluster-token.

Connection:
  --address HOST:PORT        Public Worker HTTP listen address. Default: 0.0.0.0:4200
  --worker-id ID             Worker name shown in Master GUI. Default: VPS hostname
  --network-mode MODE        DHT mode: dual or ipv4. Default: dual

Resource limits:
  --queue N                  Maximum in-memory metadata queue. Default: 256
  --batch N                  Maximum records returned per Master pull. Default: 16
  --max-downloads N          Concurrent metadata downloads. Default: 2
  --max-leeches N            Active metadata tasks. Default: 32
  --rate-limit N             UDP packets per second per DHT network. Default: 100

Installation:
  --version VERSION          Release tag such as v1.0.13, or latest. Default: latest
  --install-dir PATH         Binary directory. Default: /usr/local/bin
  --data-dir PATH            Routing-table cache directory. Default: /var/lib/dhtc-worker
  --service-user USER        Unprivileged system account. Default: dhtc-worker
  --service-name NAME        systemd unit name. Default: dhtc-worker
  -h, --help                 Show this help.

Examples:
  sudo ./install-worker.sh --token 'replace-with-long-random-token'

  sudo ./install-worker.sh \
    --token 'replace-with-long-random-token' \
    --worker-id tokyo-vps-01 \
    --address 0.0.0.0:4200 \
    --queue 128 \
    --max-downloads 1

After installation:
  systemctl status dhtc-worker
  journalctl -u dhtc-worker -f
  curl http://127.0.0.1:4200/health

Master configuration:
  dhtc -node-role master \
    -worker-urls 'https://worker.example.com:4200' \
    -cluster-token 'the-same-token'

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
    --token) need_value "$@"; TOKEN="$2"; shift 2 ;;
    --address) need_value "$@"; ADDRESS="$2"; shift 2 ;;
    --worker-id) need_value "$@"; WORKER_ID="$2"; shift 2 ;;
    --network-mode) need_value "$@"; NETWORK_MODE="$2"; shift 2 ;;
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
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [ -z "$TOKEN" ]; then
  echo "--token is required" >&2
  exit 2
fi
case "$TOKEN" in *[!A-Za-z0-9._~+/=-]*) echo "--token contains unsupported characters" >&2; exit 2 ;; esac
case "$WORKER_ID" in ''|*[!A-Za-z0-9._-]*) echo "--worker-id may contain only letters, numbers, dot, underscore and dash" >&2; exit 2 ;; esac
case "$SERVICE_USER" in ''|*[!A-Za-z0-9_-]*) echo "--service-user contains invalid characters" >&2; exit 2 ;; esac
case "$SERVICE_NAME" in ''|*[!A-Za-z0-9_.@-]*) echo "--service-name contains invalid characters" >&2; exit 2 ;; esac
case "$INSTALL_DIR:$DATA_DIR" in *' '*|*'\t'*) echo "Installation paths cannot contain whitespace" >&2; exit 2 ;; esac
case "$NETWORK_MODE" in dual|ipv4) ;; *) echo "--network-mode must be dual or ipv4" >&2; exit 2 ;; esac
case "$ADDRESS" in *:*) ;; *) echo "--address must be HOST:PORT" >&2; exit 2 ;; esac
for value in "$QUEUE" "$BATCH" "$MAX_DOWNLOADS" "$MAX_LEECHES" "$RATE_LIMIT"; do
  case "$value" in ''|*[!0-9]*) echo "Resource limits must be positive integers" >&2; exit 2 ;; esac
  [ "$value" -gt 0 ] || { echo "Resource limits must be greater than zero" >&2; exit 2; }
done

for command in awk curl id install mktemp sha256sum systemctl uname useradd; do
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
install -m 0755 "$TMP_DIR/$ASSET" "$INSTALL_DIR/dhtc-worker"

ENV_FILE="/etc/${SERVICE_NAME}.env"
umask 077
printf 'DHTC_CLUSTER_TOKEN=%s\nDHTC_WORKER_ID=%s\n' "$TOKEN" "$WORKER_ID" > "$ENV_FILE"

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
systemctl enable --now "$SERVICE_NAME"

echo
echo "Installed successfully."
echo "  Binary:  ${INSTALL_DIR}/dhtc-worker"
echo "  Data:    ${DATA_DIR}"
echo "  Service: ${SERVICE_NAME}.service"
echo "  Listen:  ${ADDRESS}"
echo
echo "Check status: systemctl status ${SERVICE_NAME}"
echo "Follow logs:  journalctl -u ${SERVICE_NAME} -f"
