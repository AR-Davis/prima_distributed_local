#!/bin/bash
# Prima Distributed Local - Universal Launcher

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

OS=$(uname -s)
ARCH=$(uname -m)

echo "🔌 Prima Distributed Local"
echo "   Detected: $OS $ARCH"
echo ""

# Run appropriate launcher via bash (handles FAT32 permission issues)
case "$OS" in
    Linux*)
        bash ./START-Linux.sh
        ;;
    Darwin*)
        bash ./START-macOS.command
        ;;
    CYGWIN*|MINGW*|MSYS*)
        bash ./START-Windows.bat
        ;;
    *)
        echo "❌ Unsupported operating system: $OS"
        exit 1
        ;;
esac
