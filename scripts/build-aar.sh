#!/usr/bin/env bash
# 本地一键生成 msi.aar（需已安装 Go、Android NDK、gomobile）
set -euo pipefail
cd "$(dirname "$0")/../go"

go mod tidy
gomobile init 2>/dev/null || true
mkdir -p ../app/libs
gomobile bind \
  -target=android \
  -androidapi=24 \
  -tags=mobile \
  -o ../app/libs/msi.aar \
  .
echo "OK -> app/libs/msi.aar"
ls -lh ../app/libs/msi.aar
