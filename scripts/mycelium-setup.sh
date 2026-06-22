#!/usr/bin/env bash
# mycelium-setup.sh
# Run this once on any Linux/macOS machine to set up the Mycelium network.
set -e

echo "  Mycelium Network — Linux/macOS Setup"
echo ""

# Check for Go
if ! command -v go >/dev/null 2>&1; then
    echo "Go not found. Install from https://go.dev/dl/"
    exit 1
fi
echo "Go: $(go version)"

# Build mycelium-api
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
mkdir -p "$HOME/bin"

echo "Building mycelium-api..."
cd "$PROJECT_DIR"
go build -o "$HOME/bin/mycelium-api" ./cmd/mycelium-api/

# Install launcher
cp "$SCRIPT_DIR/../mycelium" "$HOME/bin/mycelium"
chmod +x "$HOME/bin/mycelium"

# Add to PATH
grep -q "$HOME/bin" "$HOME/.bashrc" 2>/dev/null || echo "export PATH=\"$HOME/bin:\$PATH\"" >> "$HOME/.bashrc"
export PATH="$HOME/bin:$PATH"

# Config
mkdir -p "$HOME/.config/mycelium"
cp "$PROJECT_DIR/configs/mycelium.yaml" "$HOME/.config/mycelium/config.yaml" 2>/dev/null || true

echo "Done! Type 'mycelium' to start."
