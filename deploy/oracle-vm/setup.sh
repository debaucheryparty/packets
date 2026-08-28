#!/usr/bin/env bash
set -e

echo "Setting up Oracle VM for Packetsd..."
# Install docker
sudo apt-get update
sudo apt-get install -y docker.io docker-compose

# Start docker
sudo systemctl enable docker
sudo systemctl start docker

# Ensure mutagen is installed
# Ensure tailscale is installed and connected

echo "Setup script completed."
