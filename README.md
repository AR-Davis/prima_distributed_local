# Mycelium API

Ollama-compatible API gateway for the Mycelium distributed inference network.

Routes requests through the **Three Ravens**:
- **Huginn** — fast local (small model, low latency)
- **Muninn** — deep remote (large model, high quality)
- **Skald** — precise vocabulary (deterministic)

## Quick Start

```bash
# Build
go build -o mycelium-api ./cmd/mycelium-api

# Run with defaults (port 11435)
./mycelium-api

# Run with config
./mycelium-api -config configs/mycelium.yaml

# Custom port
./mycelium-api -port 8080
```

## API Endpoints

### Ollama-Compatible

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/generate` | POST | Text generation |
| `/api/chat` | POST | Chat completion |
| `/api/tags` | GET | List models |
| `/api/show` | POST | Model info |
| `/api/version` | GET | Version info |

### Mycelium Extensions

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/status` | GET | Node health and routing status |
| `/api/routes` | GET | Current routing configuration |
| `/api/rpc/probe` | GET | Probe RPC nodes for device memory |

### Routing Headers

Every response includes routing metadata:
- `X-Mycelium-Profile`: Which Raven handled it (huginn/muninn/skald)
- `X-Mycelium-Node`: Which node served it
- `X-Mycelium-Model`: Which model was used

### Request Extensions

Pass `"profile": "huginn"`, `"profile": "muninn"`, or `"profile": "skald"` in any request to explicitly select a Raven.

## Configuration

See `configs/mycelium.yaml` for the default config (3 nodes: Hearth, Ember, Pixel 2).

See `configs/mycelium-unified.yaml` for a full 7-node example (all Coven devices).

Key concepts:
- **Nodes** have a `pool` (local/remote/edge) and `weight` for load balancing
- **Routes** map Raven profiles to pools, with optional model overrides
- **Fallback** sends requests to local Ollama when all Mycelium nodes are down

Each gateway should set its own host to `localhost` and pool to `local`. Remote nodes use Tailscale IPs.

## Building RPC Servers

To contribute compute via the RPC protocol, nodes need a `rpc-server` binary built from [prima.cpp](https://github.com/ggml-org/prima.cpp) with a specific patch.

### Why a patch?

The mycelium-api Go client implements 12 RPC commands (0-11). Command 11 (`INIT_TENSOR`) is a patch that handles quantized tensors with `ne[0] % 512 != 0`. Without it, the server crashes on these tensors and the connection resets.

### Build scripts

Platform-specific build scripts are in `builds/`:

| Script | Platform | Target Devices |
|--------|----------|----------------|
| `builds/build-x86_64.sh` | linux/amd64 | Shepherd (Dell Latitude), any Intel/AMD Linux |
| `builds/build-arm64.sh` | linux/arm64 | Rhubarb (RPi5), Ember (HP DM1) |
| `builds/build-armhf.sh` | linux/armhf | Crow/Wren (RPi Zero 2W), Owl (RPi Model B) |
| `builds/build-termux.sh` | Android/Termux | Pixel 2 |

Each script:
1. Clones prima.cpp
2. Replaces `ggml-rpc.cpp` with the patched version (`builds/ggml-rpc.cpp.patched`)
3. Builds `rpc-server` CPU-only (no CUDA, no Metal)
4. Installs to `~/mycelium/rpc-server`

### Quick build (any Linux)

```bash
# From the repo root:
bash builds/build-x86_64.sh    # Intel/AMD
bash builds/build-arm64.sh     # RPi5 / arm64
bash builds/build-armhf.sh     # RPi Zero 2W / armhf (takes 2-4 hours)
```

### Termux (Android)

```bash
# Push the build kit via ADB first:
adb push builds/ggml-rpc.cpp.patched /sdcard/
adb push builds/build-termux.sh /sdcard/
# Then in Termux:
bash /sdcard/build-termux.sh
```

### RPC Protocol Specification

Wire format: `| cmd (1 byte) | request_size (8 bytes LE) | request_data |`  
Response: `| response_size (8 bytes LE) | response_data |`

| Code | Command | Purpose |
|------|---------|---------|
| 0 | ALLOC_BUFFER | Allocate memory on remote node |
| 1 | GET_ALIGNMENT | Query memory alignment requirement |
| 2 | GET_MAX_SIZE | Query max buffer allocation size |
| 3 | BUFFER_GET_BASE | Get buffer base pointer |
| 4 | FREE_BUFFER | Free a previously allocated buffer |
| 5 | BUFFER_CLEAR | Zero out a buffer region |
| 6 | SET_TENSOR | Send tensor data to remote |
| 7 | GET_TENSOR | Retrieve tensor data from remote |
| 8 | COPY_TENSOR | Copy tensor between buffers |
| 9 | GRAPH_COMPUTE | Execute computation graph |
| 10 | GET_DEVICE_MEMORY | Query free/total device memory |
| 11 | INIT_TENSOR | Initialize quantized tensor with padding (patched) |

RPCTensor struct (268 bytes): ID(u64), Type(u32), Buffer(u64), NE[4](u32), NB[4](u32), Op(u32), OpParams[16](i32), Flags(i32), Src[6](u64), ViewSrc(u64), ViewOffs(u64), Data(u64), Name[64](byte), Padding[4](byte).

## Cross-Compilation

The Makefile cross-compiles mycelium-api for all Coven architectures:

```bash
make build-all     # Build for amd64, arm64, armhf, windows
make deploy        # Create deployment packages in dist/
```

| Target | GOOS/GOARCH | Devices |
|--------|-------------|---------|
| `build-shepherd` | linux/amd64 | Dell Latitude |
| `build-rhubarb` | linux/arm64 | RPi5 |
| `build-crow-wren-owl` | linux/arm | RPi Zero 2W, RPi Model B |
| `build-windows` | windows/amd64 | TheTower |

All builds use `CGO_ENABLED=0` for static binaries with no C dependencies.

## Architecture

```
Request -> Mycelium API (port 11435)
  |
  +--> Classify request (Huginn/Muninn/Skald)
  |
  +--> Select healthy node from route's pools
  |
  +--> Proxy to node's Ollama API (if ollama protocol)
  |    or query via ggml-rpc binary protocol (if rpc protocol)
  |
  +--> Return response with routing headers
  |
  +--> Fallback to local Ollama if all nodes down
```

## The Three Ravens

Named after the Norse myth:

- **Huginn** (Thought) — Fast local queries. Small model on GPU. For quick answers, streaming, and low-latency needs.
- **Muninn** (Memory) — Deep remote processing. Large model, distributed across the mesh. For complex reasoning and long contexts.
- **Skald** (Sacred Words) — Precise vocabulary. Deterministic, local. For structured output and terminology.

## Coven Mesh

Current nodes (Tailscale network):

| Node | Device | Tailscale IP | Protocol | Role |
|------|--------|-------------|----------|------|
| Hearth | TheTower (GTX 1650) | 100.117.183.84 | ollama | GPU primary |
| Ember | HP DM1 (AMD E-350) | 100.90.116.1 | rpc | CPU worker |
| Pixel 2 | Android/Termux | 100.77.170.98 | rpc | ARM worker |
| Shepherd | Dell Latitude (i5) | 100.114.59.18 | ollama/rpc | API + CPU |
| Panther | RPi5 (Rhubarb) | 100.117.58.104 | ollama | ARM local |
| Crow | RPi Zero 2W | 100.97.71.98 | rpc | Edge compute |
| Wren | RPi Zero 2W | 100.83.89.53 | rpc | Edge compute |

## License

Public domain (CC0). See the Grove Architecture proposal for design philosophy.

