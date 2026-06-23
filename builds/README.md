# RPC Server Builds

Platform-specific build scripts for prima.cpp `rpc-server` with the INIT_TENSOR patch.

## Why These Exist

The mycelium-api Go client implements RPC command 11 (`INIT_TENSOR`), which is a patch to the upstream ggml-rpc.cpp. Without this patch, the server crashes on quantized tensors with `ne[0] % 512 != 0` and the connection resets.

These scripts automate cloning prima.cpp, applying the patch, and building a compatible `rpc-server` binary.

## Files

| File | Purpose |
|------|---------|
| `ggml-rpc.cpp.patched` | Patched source with INIT_TENSOR (command 11) |
| `ggml-rpc.cpp.original` | Original unpatched source for reference |
| `build-x86_64.sh` | Build for Intel/AMD Linux (Shepherd) |
| `build-arm64.sh` | Build for arm64 Linux (Rhubarb RPi5, Ember) |
| `build-armhf.sh` | Build for armhf Linux (Crow/Wren RPi Zero 2W) |
| `build-termux.sh` | Build for Android/Termux (Pixel 2) |

## Usage

```bash
# On the target device, from the repo root:
bash builds/build-x86_64.sh     # Intel/AMD
bash builds/build-arm64.sh      # RPi5 / arm64
bash builds/build-armhf.sh      # RPi Zero 2W (2-4 hours)
```

For Termux/Android, push files via ADB first:
```bash
adb push builds/ggml-rpc.cpp.patched /sdcard/
adb push builds/build-termux.sh /sdcard/
# Then in Termux:
bash /sdcard/build-termux.sh
```

## After Building

```bash
# Start the compute node
~/mycelium/rpc-server -H 0.0.0.0 -p 50052

# Or via the mycelium launcher:
mycelium node

# Verify from any gateway:
curl http://localhost:11435/api/status
# Your node should show "healthy"
```

## Alternative: API Gateway Only

If you don't need local compute, skip the rpc-server build entirely:
```bash
mycelium api    # API gateway only, routes to other nodes for compute
```

Three compute nodes (Hearth, Ember, Pixel 2) are sufficient for the mesh.
