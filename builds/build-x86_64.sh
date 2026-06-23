#!/usr/bin/env bash
# build-rpc-server.sh — x86_64 CPU-only build for Shepherd / Intel / AMD
# Builds a protocol-compatible rpc-server matching mycelium-api's Go client.
set -e

echo "  Mycelium RPC Server Build — x86_64 CPU-only"
echo ""

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
PATCHED_SRC="$SCRIPT_DIR/ggml-rpc.cpp.patched"

if [ ! -f "$PATCHED_SRC" ]; then
    echo "ERROR: ggml-rpc.cpp.patched not found in $SCRIPT_DIR"
    echo "       This file contains the INIT_TENSOR patch (command 11)."
    echo "       Without it, the protocol won't match mycelium-api."
    exit 1
fi

# Install build deps
echo "[1/5] Installing build dependencies..."
sudo apt update -y && sudo apt install -y build-essential git cmake

# Clone prima.cpp
echo "[2/5] Cloning prima.cpp..."
BUILD_DIR="${BUILD_DIR:-$HOME/build}"
mkdir -p "$BUILD_DIR"
if [ ! -d "$BUILD_DIR/prima.cpp" ]; then
    git clone https://github.com/ggml-org/prima.cpp.git "$BUILD_DIR/prima.cpp"
fi
cd "$BUILD_DIR/prima.cpp"

# Apply the patch
echo "[3/5] Applying INIT_TENSOR patch..."
cp "$PATCHED_SRC" ggml/src/ggml-rpc.cpp
grep -q "RPC_CMD_INIT_TENSOR" ggml/src/ggml-rpc.cpp && echo "  Patch verified: INIT_TENSOR present" || { echo "ERROR: Patch verification failed"; exit 1; }

# Build CPU-only
echo "[4/5] Building rpc-server (CPU-only, no CUDA)..."
mkdir -p build && cd build
cmake .. -DGGML_RPC=ON -DGGML_CUDA=OFF -DGGML_METAL=OFF -DCMAKE_BUILD_TYPE=Release
cmake --build . --target rpc-server --config Release -j$(nproc)

# Install
echo "[5/5] Installing..."
MYCELIUM_DIR="${MYCELIUM_DIR:-$HOME/mycelium}"
mkdir -p "$MYCELIUM_DIR"
cp bin/rpc-server "$MYCELIUM_DIR/rpc-server"
chmod +x "$MYCELIUM_DIR/rpc-server"

echo ""
echo "  Done! rpc-server installed to $MYCELIUM_DIR/rpc-server"
echo "  Start with: $MYCELIUM_DIR/rpc-server -H 0.0.0.0 -p 50052"
echo "  Or use:     mycelium node"
