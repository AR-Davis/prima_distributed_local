#!/bin/bash
# prepare-usb.sh - Prepare a folder for USB deployment

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
USB_DIR="${PROJECT_ROOT}/usb-deploy"

echo "🔌 Preparing Prima Distributed Local for USB deployment"
echo ""

# Clean and create USB directory
rm -rf "$USB_DIR"
mkdir -p "$USB_DIR"

# Build all platforms
echo "🔨 Building binaries..."
cd "$PROJECT_ROOT"
make build-all

# Copy binaries
echo "📦 Copying binaries..."
mkdir -p "$USB_DIR/bin"
cp "$PROJECT_ROOT/dist/prima-installer-linux-amd64" "$USB_DIR/bin/prima-installer-linux-x64"
cp "$PROJECT_ROOT/dist/prima-installer-linux-arm64" "$USB_DIR/bin/prima-installer-linux-arm64"
cp "$PROJECT_ROOT/dist/prima-installer-darwin-amd64" "$USB_DIR/bin/prima-installer-macos-intel"
cp "$PROJECT_ROOT/dist/prima-installer-darwin-arm64" "$USB_DIR/bin/prima-installer-macos-apple"
cp "$PROJECT_ROOT/dist/prima-installer-windows-amd64.exe" "$USB_DIR/bin/prima-installer-windows.exe"

# Copy configs
echo "📝 Copying configurations..."
mkdir -p "$USB_DIR/config"
cp -r "$PROJECT_ROOT/config/"* "$USB_DIR/config/"

# Create launcher scripts
echo "🚀 Creating launcher scripts..."

# Linux launcher
cat > "$USB_DIR/START-Linux.sh" << 'EOF'
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

# Make executable
chmod +x "$BINARY"

# Launch TUI
echo "🔌 Starting Prima Distributed Local..."
echo ""
exec "$BINARY" tui
EOF
chmod +x "$USB_DIR/START-Linux.sh"

# macOS launcher
cat > "$USB_DIR/START-macOS.command" << 'EOF'
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
EOF
chmod +x "$USB_DIR/START-macOS.command"

# Windows launcher
cat > "$USB_DIR/START-Windows.bat" << 'EOF'
@echo off
:: Prima Distributed Local - Windows Launcher

cd /d "%~dp0"

echo 🔌 Starting Prima Distributed Local...
echo.

bin\prima-installer-windows.exe tui

if errorlevel 1 (
    echo.
    echo ❌ Error starting Prima. Press any key to exit...
    pause > nul
)
EOF

# Universal launcher (auto-detect)
cat > "$USB_DIR/START.sh" << 'EOF'
#!/bin/bash
# Prima Distributed Local - Universal Launcher

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

OS=$(uname -s)
ARCH=$(uname -m)

echo "🔌 Prima Distributed Local"
echo "   Detected: $OS $ARCH"
echo ""

case "$OS" in
    Linux*)
        exec ./START-Linux.sh
        ;;
    Darwin*)
        exec ./START-macOS.command
        ;;
    CYGWIN*|MINGW*|MSYS*)
        exec ./START-Windows.bat
        ;;
    *)
        echo "❌ Unsupported operating system: $OS"
        exit 1
        ;;
esac
EOF
chmod +x "$USB_DIR/START.sh"

# Create README for USB
cat > "$USB_DIR/README.txt" << 'EOF'
╔══════════════════════════════════════════════════════════════════╗
║     PRIMA DISTRIBUTED LOCAL - Portable LLM Cluster Node          ║
╚══════════════════════════════════════════════════════════════════╝

QUICK START
───────────

1. Plug USB into any PC
2. Open USB folder
3. Double-click the START file for your OS:

   • Windows:    START-Windows.bat
   • macOS:      START-macOS.command
   • Linux:      START-Linux.sh
   
   Or use:       START.sh (auto-detects)

FIRST TIME SETUP
────────────────

1. Select "🔍 Detect Hardware" to analyze this PC
2. Select "📦 Install Service" to install as system service
3. Configure cluster settings (head node IP, etc.)

TIER EXPLANATION
────────────────

Your PC will be scored and assigned a tier:

• LIGHTWEIGHT (<50 pts): Background only, minimal resources
• MIDDLE (50-99 pts): User-controlled, runs when idle
• FULL (100+ pts): Dedicated node, maximum resources

ACCESS MODES
────────────

• PERSONAL: Local LAN only (home network)
• FRIEND:   QR pairing for friend groups
• DISCORD:  Public bot (rate-limited)

FILES
─────

├── START-*.sh/bat/command  → Launchers
├── bin/                    → Binaries for all platforms
├── config/                 → Configuration templates
└── README.txt             → This file

SUPPORT
───────

https://github.com/AR-Davis/prima_distributed_local

EOF

# Create .gitignore for USB folder
cat > "$USB_DIR/.gitignore" << 'EOF'
# This is a USB deployment folder - don't track
# Contents are generated from source
EOF

echo "✅ USB deployment ready!"
echo ""
echo "Location: $USB_DIR"
echo ""
echo "Contents:"
ls -la "$USB_DIR"
echo ""
echo "📦 Binaries:"
ls -la "$USB_DIR/bin/"
echo ""
echo "Next steps:"
echo "1. Copy $USB_DIR to a USB drive"
echo "2. Plug into target PC"
echo "3. Run START-* for that platform"
