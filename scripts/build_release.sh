#!/usr/bin/env bash
set -e

if ! command -v go >/dev/null 2>&1; then
  if [ -d "/mnt/c/Program Files/Go/bin" ]; then
    export PATH="/mnt/c/Program Files/Go/bin:$PATH"
  fi
fi

mkdir -p bin

VERSION="${GITHUB_REF_NAME:-$(git describe --tags --always --dirty 2>/dev/null || echo "1.0.0")}"
LDFLAGS="-s -w -X main.version=${VERSION}"

echo "Building Windows binaries (.exe)..."
GOOS=windows GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o bin/packets-windows-amd64.exe ./cmd/packets
GOOS=windows GOARCH=arm64 go build -ldflags "${LDFLAGS}" -o bin/packets-windows-arm64.exe ./cmd/packets
GOOS=windows GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o bin/packetsd-windows-amd64.exe ./cmd/packetsd
GOOS=windows GOARCH=arm64 go build -ldflags "${LDFLAGS}" -o bin/packetsd-windows-arm64.exe ./cmd/packetsd

echo "Packaging Windows archives (.zip)..."
if command -v zip >/dev/null 2>&1; then
  (cd bin && zip -q packets-windows-amd64.zip packets-windows-amd64.exe)
  (cd bin && zip -q packets-windows-arm64.zip packets-windows-arm64.exe)
  (cd bin && zip -q packetsd-windows-amd64.zip packetsd-windows-amd64.exe)
  (cd bin && zip -q packetsd-windows-arm64.zip packetsd-windows-arm64.exe)
fi

echo "Building and packaging Linux tarballs (.tar.gz)..."
for arch in amd64 arm64; do
  TMP_DIR=$(mktemp -d)
  GOOS=linux GOARCH=${arch} go build -ldflags "${LDFLAGS}" -o "${TMP_DIR}/packets" ./cmd/packets
  tar -czf "bin/packets-linux-${arch}.tar.gz" -C "${TMP_DIR}" packets
  rm -rf "${TMP_DIR}"

  TMP_DIR=$(mktemp -d)
  GOOS=linux GOARCH=${arch} go build -ldflags "${LDFLAGS}" -o "${TMP_DIR}/packetsd" ./cmd/packetsd
  tar -czf "bin/packetsd-linux-${arch}.tar.gz" -C "${TMP_DIR}" packetsd
  rm -rf "${TMP_DIR}"
done

ROOT_DIR="$(pwd)"

echo "Building and packaging macOS tarballs (.tar.gz) and disk images (.dmg)..."
for arch in amd64 arm64; do
  TMP_DIR=$(mktemp -d)
  GOOS=darwin GOARCH=${arch} go build -ldflags "${LDFLAGS}" -o "${TMP_DIR}/packets" ./cmd/packets
  tar -czf "bin/packets-darwin-${arch}.tar.gz" -C "${TMP_DIR}" packets
  if command -v zip >/dev/null 2>&1; then
    (cd "${TMP_DIR}" && zip -q "${ROOT_DIR}/bin/packets-darwin-${arch}.zip" packets)
  fi
  if command -v genisoimage >/dev/null 2>&1; then
    genisoimage -V "packets" -D -R -apple -no-pad -o "${ROOT_DIR}/bin/packets-darwin-${arch}.dmg" "${TMP_DIR}" 2>/dev/null || true
  fi
  rm -rf "${TMP_DIR}"

  TMP_DIR=$(mktemp -d)
  GOOS=darwin GOARCH=${arch} go build -ldflags "${LDFLAGS}" -o "${TMP_DIR}/packetsd" ./cmd/packetsd
  tar -czf "bin/packetsd-darwin-${arch}.tar.gz" -C "${TMP_DIR}" packetsd
  if command -v zip >/dev/null 2>&1; then
    (cd "${TMP_DIR}" && zip -q "${ROOT_DIR}/bin/packetsd-darwin-${arch}.zip" packetsd)
  fi
  if command -v genisoimage >/dev/null 2>&1; then
    genisoimage -V "packetsd" -D -R -apple -no-pad -o "${ROOT_DIR}/bin/packetsd-darwin-${arch}.dmg" "${TMP_DIR}" 2>/dev/null || true
  fi
  rm -rf "${TMP_DIR}"
done

echo "Building Debian packages (.deb)..."
bash scripts/package_deb.sh

echo "Release build finished successfully!"
