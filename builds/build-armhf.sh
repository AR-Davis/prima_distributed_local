#!/usr/bin/env bash
# build-rpc-server.sh — armhf (32-bit ARM) build for RPi Zero 2W / RPi Model B
# Builds a protocol-compatible rpc-server matching mycelium-api's Go client.
# WARNING: This takes 2-4 hours on RPi Zero 2W. Be patient.
set -e

echo "  Mycelium RPC Server Build — armhf (32-bit ARM)"
echo "  WARNING: This will take 2-4 hours on RPi Zero. Be patient."
echo ""

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PATCHED_SRC="$SCRIPT_DIR/ggml-rpc.cpp.patched"

if [ ! -f "$PATCHED_SRC" ]; then
    echo "ERROR: ggml-rpc.cpp.patched not found in $SCRIPT_DIR"
    exit 1
fi

# Install build deps
echo "[1/6] Installing build dependencies..."
sudo apt update -y && sudo apt install -y build-essential git cmake

# Enable swap (RPi Zero has only 512MB RAM)
echo "[2/6] Enabling swap (512MB -> 1GB)..."
sudo sed -i 's/CONF_SWAPSIZE=.*/CONF_SWAPSIZE=1024/' /etc/dphys-swapfile 2>/dev/null || true
sudo dphys-swapfile setup 2>/dev/null && sudo dphys-swapfile swapon 2>/dev/null || true
echo "  Swap enabled"

# Clone prima.cpp
echo "[3/6] Cloning prima.cpp..."
BUILD_DIR="${BUILD_DIR:-$HOME/build}"
mkdir -p "$BUILD_DIR"
if [ ! -d "$BUILD_DIR/prima.cpp" ]; then
    git clone https://github.com/AR-Davis/prima.cpp.git "$BUILD_DIR/prima.cpp"
fi
cd "$BUILD_DIR/prima.cpp"

# Verify the patch is already committed
echo "[4/6] Verifying INIT_TENSOR patch..."
grep -q "RPC_CMD_INIT_TENSOR" ggml/src/ggml-rpc.cpp && echo "  Patch verified" || { echo "ERROR: Patch missing in source tree"; exit 1; }

# Build CPU-only, single thread to save RAM
echo "[5/6] Building rpc-server (armhf, single thread)..."
echo "  This is the slow part. Go get a coffee. Or lunch."
mkdir -p build && cd build
cmake .. -DGGML_RPC=ON -DGGML_CUDA=OFF -DGGML_METAL=OFF -DCMAKE_BUILD_TYPE=Release
cmake --build . --target rpc-server --config Release -j1

# Install
echo "[6/6] Installing..."
MYCELIUM_DIR="${MYCELIUM_DIR:-$HOME/mycelium}"
mkdir -p "$MYCELIUM_DIR"
cp bin/rpc-server "$MYCELIUM_DIR/rpc-server"
chmod +x "$MYCELIUM_DIR/rpc-server"

echo ""
echo "  Done! rpc-server installed to $MYCELIUM_DIR/rpc-server"
echo "  Start with: $MYCELIUM_DIR/rpc-server -H 0.0.0.0 -p 50052"
echo "  Or use:     mycelium node"
echo ""
echo "  Note: RPi Zero 2W has limited RAM. Compute contribution will be minimal."
echo "  Consider weight=1 and long timeouts in mycelium.yaml."
