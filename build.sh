#!/usr/bin/env bash
# One static binary with the page embedded, so deploying is a copy.
set -euo pipefail
cd "$(dirname "$0")"
VERSION="${1:-$(git describe --tags --always 2>/dev/null || echo dev)}"
mkdir -p dist
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.Version=$VERSION" -o dist/essaim-ui-linux-amd64 .
( cd dist && sha256sum essaim-ui-linux-amd64 > essaim-ui-linux-amd64.sha256 )
# Rename, never copy: you cannot write over the inode of a running process
# (ETXTBSY), and a rename swaps the directory entry without disturbing it.
mkdir -p bin
cp dist/essaim-ui-linux-amd64 bin/.essaim-ui.new
mv bin/.essaim-ui.new bin/essaim-ui
echo "built dist/essaim-ui-linux-amd64 ($VERSION, $(stat -c%s dist/essaim-ui-linux-amd64) bytes)"
