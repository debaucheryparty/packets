#!/usr/bin/env bash
set -e

echo "Installing Packets..."
# In a real install script, this would download the binary from GitHub releases
# based on OS and architecture. For now, it assumes local compilation.

make build
mkdir -p ~/.packets/bin
cp bin/packets ~/.packets/bin/packets

echo "Installation complete. Add ~/.packets/bin to your PATH."
