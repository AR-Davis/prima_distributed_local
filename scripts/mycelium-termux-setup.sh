#!/data/data/com.termux/files/usr/bin/sh
# mycelium-termux-setup.sh
# Run this once on a Termux device to set up the Mycelium compute node.
# This builds rpc-server with the INIT_TENSOR patch for protocol compatibility.
set -e

echo "  Mycelium Network — Termux Setup"
echo ""

# Update packages
echo "[1/5] Updating packages..."
pkg update -y && pkg upgrade -y

# Install dependencies
echo "[2/5] Installing dependencies..."
pkg install -y git cmake clang make wget

# Build rpc-server with patched source
echo "[3/5] Building rpc-server (with INIT_TENSOR patch)..."
BUILD_DIR="${BUILD_DIR:-$HOME/build}"
mkdir -p "$BUILD_DIR"
if [ ! -d "$BUILD_DIR/prima.cpp" ]; then
    git clone https://github.com/ggml-org/prima.cpp.git "$BUILD_DIR/prima.cpp"
fi
cd "$BUILD_DIR/prima.cpp"

# Check for patched ggml-rpc.cpp (pushed via ADB)
PATCHED_SRC="${PATCHED_SRC:-/sdcard/ggml-rpc.cpp.patched}"
if [ -f "$PATCHED_SRC" ]; then
    echo "  Applying INIT_TENSOR patch from $PATCHED_SRC"
    cp "$PATCHED_SRC" ggml/src/ggml-rpc.cpp
    grep -q "RPC_CMD_INIT_TENSOR" ggml/src/ggml-rpc.cpp && echo "  Patch verified" || { echo "ERROR: Patch verification failed"; exit 1; }
else
    echo "  WARNING: Patched ggml-rpc.cpp not found at $PATCHED_SRC"
    echo "  Building from unpatched source — protocol may not match mycelium-api"
    echo "  Push the patched file: adb push ggml-rpc.cpp.patched /sdcard/"
fi

mkdir -p build && cd build
cmake .. -DGGML_RPC=ON -DGGML_CUDA=OFF -DGGML_METAL=OFF \
    -DCMAKE_C_COMPILER=clang -DCMAKE_CXX_COMPILER=clang++ \
    -DCMAKE_BUILD_TYPE=Release
cmake --build . --target rpc-server --config Release -j$(nproc)

# Install
echo "[4/5] Installing..."
MYCELIUM_DIR="${MYCELIUM_DIR:-$HOME/mycelium}"
mkdir -p "$MYCELIUM_DIR"
cp bin/rpc-server "$MYCELIUM_DIR/rpc-server"
chmod +x "$MYCELIUM_DIR/rpc-server"

# Install launcher
mkdir -p "$HOME/bin"
cat > "$HOME/bin/mycelium" << 'LAUNCHER'
#!/data/data/com.termux/files/usr/bin/sh
MYCELIUM_RPC_PORT="${MYCELIUM_RPC_PORT:-50052}"
MYCELIUM_RPC_HOST="${MYCELIUM_RPC_HOST:-0.0.0.0}"
MYCELIUM_RPC_BIN="${MYCELIUM_RPC_BIN:-$HOME/mycelium/rpc-server}"

case "${1:-node}" in
    node)
        echo "  Mycelium Compute Node — ${MYCELIUM_RPC_HOST}:${MYCELIUM_RPC_PORT}"
        exec "$MYCELIUM_RPC_BIN" -H "$MYCELIUM_RPC_HOST" -p "$MYCELIUM_RPC_PORT"
        ;;
    status)
        API_HOST="${MYCELIUM_API_HOST:-localhost}"
        API_PORT="${MYCELIUM_API_PORT:-11435}"
        curl -s "http://${API_HOST}:${API_PORT}/api/status"
        ;;
    probe)
        API_HOST="${MYCELIUM_API_HOST:-localhost}"
        API_PORT="${MYCELIUM_API_PORT:-11435}"
        curl -s "http://${API_HOST}:${API_PORT}/api/rpc/probe"
        ;;
    help|"")
        echo "mycelium — Termux compute node"
        echo "  node   Start RPC compute node (default)"
        echo "  status Check network status"
        echo "  probe  Probe RPC nodes"
        ;;
esac
LAUNCHER
chmod +x "$HOME/bin/mycelium"

# Add to PATH
grep -q "$HOME/bin" "$HOME/.bashrc" 2>/dev/null || echo 'export PATH="$HOME/bin:$PATH"' >> "$HOME/.bashrc"
export PATH="$HOME/bin:$PATH"

echo "[5/5] Done! Type 'mycelium' to start the compute node."
