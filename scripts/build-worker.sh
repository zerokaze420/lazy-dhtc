#!/usr/bin/env sh
set -eu

output="${1:-dist/dhtc-worker-linux-amd64}"
mkdir -p "$(dirname "$output")"
CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH:-amd64}" go build \
  -trimpath \
  -ldflags="-s -w" \
  -o "$output" \
  ./cmd/dhtc
echo "$output"
