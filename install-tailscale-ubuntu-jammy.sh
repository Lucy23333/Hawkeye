#!/usr/bin/env bash
set -euo pipefail

echo "[1/6] Preparing Tailscale apt keyring..."
sudo mkdir -p --mode=0755 /usr/share/keyrings

if curl -fsSL --connect-timeout 15 https://pkgs.tailscale.com/stable/ubuntu/jammy.noarmor.gpg \
  | sudo tee /usr/share/keyrings/tailscale-archive-keyring.gpg >/dev/null; then
  echo "[2/6] Installed Tailscale apt key."

  echo "[3/6] Installing Tailscale apt source..."
  curl -fsSL https://pkgs.tailscale.com/stable/ubuntu/jammy.tailscale-keyring.list \
    | sudo tee /etc/apt/sources.list.d/tailscale.list >/dev/null

  echo "[4/6] Updating apt indexes..."
  sudo apt-get update

  echo "[5/6] Installing and enabling Tailscale from apt..."
  sudo apt-get install -y tailscale
  sudo systemctl enable --now tailscaled
else
  echo "[2/6] Tailscale apt source is unreachable from this network."
  echo "[3/6] Falling back to the Canonical-maintained Tailscale snap."
  sudo snap install tailscale --channel=1/stable
  sudo snap connect tailscale:network-control || true
  sudo snap connect tailscale:firewall-control || true
  sudo snap connect tailscale:network-bind || true
  echo "[4/6] Snap install completed."
  echo "[5/6] Skipping apt install."
fi

echo "[6/6] Starting Tailscale login. Open the URL it prints and authorize this machine."
sudo tailscale up --hostname=ubuntu-go-dev

echo
echo "Tailscale install/login command completed. Current status:"
tailscale status || true
tailscale ip -4 || true
