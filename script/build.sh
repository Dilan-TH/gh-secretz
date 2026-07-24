#!/usr/bin/env bash
# Cross compiles release binaries for gh-extension-precompile.
#
# The action invokes this with the release tag as the first argument and
# expects executables in dist/ named {os}-{arch}{ext}. The tag is injected
# into main.version so that "gh secretz --version" reports the released
# version rather than the "dev" default.
set -euo pipefail

tag="${1:-dev}"
mkdir -p dist

platforms=(
  darwin-amd64
  darwin-arm64
  freebsd-386
  freebsd-amd64
  freebsd-arm64
  linux-386
  linux-amd64
  linux-arm
  linux-arm64
  windows-386
  windows-amd64
  windows-arm64
)

for p in "${platforms[@]}"; do
  os="${p%%-*}"
  arch="${p##*-}"
  ext=""
  if [ "$os" = "windows" ]; then
    ext=".exe"
  fi
  echo "building dist/${p}${ext}"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w -X main.version=${tag}" -o "dist/${p}${ext}" .
done
