#!/usr/bin/env bash
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
RESET='\033[0m'

echo -e "${BLUE}${BOLD}"
echo "📦 Installing packets CLI..."
echo -e "${RESET}"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

if [ "$ARCH" = "x86_64" ]; then
    ARCH="amd64"
elif [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
    ARCH="arm64"
else
    echo -e "${RED}Error: Unsupported architecture $ARCH${RESET}"
    exit 1
fi

if [ "$OS" != "linux" ] && [ "$OS" != "darwin" ]; then
    echo -e "${RED}Error: Unsupported OS $OS${RESET}"
    exit 1
fi

REPO="debaucheryparty/packets"
BINARY_NAME="packets-${OS}-${ARCH}"

echo -e "${YELLOW}Fetching latest release info from GitHub...${RESET}"
LATEST_RELEASE=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST_RELEASE" ]; then
    echo -e "${RED}Error: Failed to fetch the latest release.${RESET}"
    exit 1
fi

DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${LATEST_RELEASE}/${BINARY_NAME}"

INSTALL_DIR="${HOME}/.packets/bin"
TMP_FILE="/tmp/packets-latest"

echo -e "Downloading ${BOLD}packets ${LATEST_RELEASE}${RESET} for ${OS}/${ARCH}..."
curl -L -# -o "$TMP_FILE" "$DOWNLOAD_URL"

if [ $? -ne 0 ]; then
    echo -e "${RED}Error: Failed to download the binary.${RESET}"
    rm -f "$TMP_FILE"
    exit 1
fi

echo -e "${YELLOW}Installing to ${INSTALL_DIR}...${RESET}"
mkdir -p "$INSTALL_DIR"
mv "$TMP_FILE" "${INSTALL_DIR}/packets"
chmod +x "${INSTALL_DIR}/packets"

echo -e "\n${GREEN}${BOLD}✔ Installation complete!${RESET}\n"

if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo -e "${YELLOW}Almost done!${RESET} The 'packets' executable is not in your PATH."
    echo -e "To use the packets CLI, add this to your shell profile (e.g. ~/.bashrc or ~/.zshrc):\n"
    echo -e "    ${BOLD}export PATH=\"\$PATH:${INSTALL_DIR}\"${RESET}\n"
else
    echo -e "You can now run ${BOLD}packets --help${RESET} to get started."
fi
