#!/data/data/com.termux/files/usr/bin/sh
# build-rpc-server.sh — Termux/Android build for Pixel 2
# Builds a protocol-compatible rpc-server matching mycelium-api's Go client.
# Android's Bionic libc requires native build (cross-compile fails).
set -e

echo "  Mycelium RPC Server Build — Termux/Android"
echo ""

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PATCHED_SRC="$SCRIPT_DIR/ggml-rpc.cpp.patched"

if [ ! -f "$PATCHED_SRC" ]; then
    echo "ERROR: ggml-rpc.cpp.patched not found in $SCRIPT_DIR"
    echo "       Push it via ADB: adb push ggml-rpc.cpp.patched /sdcard/"
    exit 1
fi

# Install build deps
echo "[1/5] Installing build dependencies..."
pkg update -y && pkg install -y git cmake clang make

# Clone prima.cpp
echo "[2/5] Cloning prima.cpp..."
BUILD_DIR="${BUILD_DIR:-$HOME/build}"
mkdir -p "$BUILD_DIR"
if [ ! -d "$BUILD_DIR/prima.cpp" ]; then
    git clone https://github.com/AR-Davis/prima.cpp.git "$BUILD_DIR/prima.cpp"
fi
cd "$BUILD_DIR/prima.cpp"

# Verify the patch is already committed
echo "[3/5] Verifying INIT_TENSOR patch..."
grep -q "RPC_CMD_INIT_TENSOR" ggml/src/ggml-rpc.cpp && echo "  Patch verified" || { echo "ERROR: Patch missing in source tree"; exit 1; }

# Build with clang (Android's gcc is not available)
echo "[4/5] Building rpc-server (Termux, clang)..."
mkdir -p build && cd build
cmake .. -DGGML_RPC=ON -DGGML_CUDA=OFF -DGGML_METAL=OFF     -DCMAKE_C_COMPILER=clang -DCMAKE_CXX_COMPILER=clang++     -DCMAKE_BUILD_TYPE=Release
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
