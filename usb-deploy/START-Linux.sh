#!/bin/bash
# Prima Distributed Local - Linux Launcher

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ARCH=$(uname -m)

cd "$SCRIPT_DIR"

# Detect architecture
if [ "$ARCH" = "x86_64" ]; then
    BINARY="./bin/prima-installer-linux-x64"
elif [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
    BINARY="./bin/prima-installer-linux-arm64"
else
    echo "❌ Unsupported architecture: $ARCH"
    echo "   Supported: x86_64, aarch64, arm64"
    exit 1
fi

# Check if binary exists
if [ ! -f "$BINARY" ]; then
    echo "❌ Binary not found: $BINARY"
    exit 1
fi

# FAT32 USB drives don't preserve execute permissions
# If not executable, copy to /tmp and run from there
if [ ! -x "$BINARY" ]; then
    echo "📦 USB permissions detected, preparing to run..."
    TMP_BIN="/tmp/$(basename $BINARY)"
    cp "$BINARY" "$TMP_BIN"
    chmod +x "$TMP_BIN"
    BINARY="$TMP_BIN"
fi

echo "🔌 Starting Prima Distributed Local..."
echo ""
exec "$BINARY" tui
