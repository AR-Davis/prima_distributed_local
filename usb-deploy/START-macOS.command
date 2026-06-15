#!/bin/bash
# Prima Distributed Local - macOS Launcher

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# Detect architecture
ARCH=$(uname -m)

if [ "$ARCH" = "x86_64" ]; then
    BINARY="./bin/prima-installer-macos-intel"
elif [ "$ARCH" = "arm64" ]; then
    BINARY="./bin/prima-installer-macos-apple"
else
    echo "❌ Unsupported architecture: $ARCH"
    exit 1
fi

# Check if binary exists
if [ ! -f "$BINARY" ]; then
    echo "❌ Binary not found: $BINARY"
    exit 1
fi

# Make executable
chmod +x "$BINARY"

# Launch TUI
echo "🔌 Starting Prima Distributed Local..."
echo ""
exec "$BINARY" tui
