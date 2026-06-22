#!/data/data/com.termux/files/usr/bin/sh
# mycelium-termux-setup.sh
# Run this once on a Termux device to set up the Mycelium compute node.
set -e

echo "  Mycelium Network — Termux Setup"
echo ""

# Update packages
echo "[1/4] Updating packages..."
pkg update -y && pkg upgrade -y

# Install dependencies
echo "[2/4] Installing dependencies..."
pkg install -y git cmake clang make wget

# Build rpc-server
echo "[3/4] Building rpc-server..."
if [ ! -d "$HOME/prima.cpp" ]; then
    git clone https://github.com/AR-Davis/prima_distributed_local.git "$HOME/prima.cpp"
fi
cd "$HOME/prima.cpp" && mkdir -p build && cd build
cmake .. -DGGML_RPC=ON -DCMAKE_BUILD_TYPE=Release
cmake --build . --config Release -j$(nproc)

# Install launcher
mkdir -p "$HOME/bin"
cat > "$HOME/bin/mycelium" << 'LAUNCHER'
#!/data/data/com.termux/files/usr/bin/sh
MYCELIUM_RPC_PORT="${MYCELIUM_RPC_PORT:-50052}"
MYCELIUM_RPC_HOST="${MYCELIUM_RPC_HOST:-0.0.0.0}"
MYCELIUM_RPC_BIN="${MYCELIUM_RPC_BIN:-$HOME/prima.cpp/build/bin/rpc-server}"

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

echo "[4/4] Done! Type 'mycelium' to start the compute node."
