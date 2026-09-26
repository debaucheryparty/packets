#!/usr/bin/env bash
set -e

if ! command -v go >/dev/null 2>&1; then
  if [ -d "/mnt/c/Program Files/Go/bin" ]; then
    export PATH="/mnt/c/Program Files/Go/bin:$PATH"
  fi
fi

VERSION="${GITHUB_REF_NAME:-1.0.0}"
VERSION="${VERSION#v}"

for arch in amd64 arm64; do
  TMP_BIN=$(mktemp -d)
  GOOS=linux GOARCH=${arch} go build -ldflags "-s -w" -o "${TMP_BIN}/packets" ./cmd/packets
  PKG_DIR=$(mktemp -d)
  mkdir -p "${PKG_DIR}/DEBIAN" "${PKG_DIR}/usr/local/bin"
  cp "${TMP_BIN}/packets" "${PKG_DIR}/usr/local/bin/packets"
  chmod 755 "${PKG_DIR}/usr/local/bin/packets"
  cat << EOF > "${PKG_DIR}/DEBIAN/control"
Package: packets
Version: ${VERSION}
Section: devel
Priority: optional
Architecture: ${arch}
Maintainer: debaucheryparty
Description: Distributed remote build and development platform
EOF
  if command -v dpkg-deb >/dev/null 2>&1; then
    dpkg-deb --build "${PKG_DIR}" "bin/packets-linux-${arch}.deb"
  fi
  rm -rf "${PKG_DIR}" "${TMP_BIN}"

  TMP_BIN=$(mktemp -d)
  GOOS=linux GOARCH=${arch} go build -ldflags "-s -w" -o "${TMP_BIN}/packetsd" ./cmd/packetsd
  PKG_DIR=$(mktemp -d)
  mkdir -p "${PKG_DIR}/DEBIAN" "${PKG_DIR}/usr/local/bin"
  cp "${TMP_BIN}/packetsd" "${PKG_DIR}/usr/local/bin/packetsd"
  chmod 755 "${PKG_DIR}/usr/local/bin/packetsd"
  cat << EOF > "${PKG_DIR}/DEBIAN/control"
Package: packetsd
Version: ${VERSION}
Section: devel
Priority: optional
Architecture: ${arch}
Maintainer: debaucheryparty
Description: Distributed remote build and development scheduler daemon
EOF
  if command -v dpkg-deb >/dev/null 2>&1; then
    dpkg-deb --build "${PKG_DIR}" "bin/packetsd-linux-${arch}.deb"
  fi
  rm -rf "${PKG_DIR}" "${TMP_BIN}"
done
