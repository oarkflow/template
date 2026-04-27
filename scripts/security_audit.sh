#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export GOCACHE="${GOCACHE:-/tmp/go-build-cache}"

echo "[audit] go test"
go test ./...

echo "[audit] go test -race"
go test -race ./...

echo "[audit] go test -bench"
go test -bench . -run '^$'

echo "[audit] pentest"
"$ROOT_DIR/scripts/pentest.sh"

echo "[audit] completed"
