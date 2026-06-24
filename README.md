# Mycelium API

Ollama-compatible API gateway for the Mycelium distributed inference network. Routes requests through the **Three Ravens**, with both synchronous (real-time) and asynchronous (background) inference modes.

## Architecture

```
Client Request
  |
  +--> /api/generate (synchronous — Huginn/Skald)
  |      |
  |      +--> llama-server (if healthy) --> RPC nodes (distributed compute)
  |      +--> Ollama (fallback) --> local GPU
  |
  +--> /api/submit (asynchronous — Muninn)
         |
         +--> Job queue --> background worker --> llama-server --> RPC nodes
         |
         +--> Returns job ID immediately
         +--> Poll /api/job/<id> for results
```

## The Three Ravens

| Raven | Mode | Endpoint | Backend | Use Case |
|-------|------|----------|---------|----------|
| **Huginn** | Sync | `/api/generate` | llama-server (RPC) or Ollama (GPU) | Fast real-time answers |
| **Muninn** | Async | `/api/submit` + `/api/job/<id>` | llama-server + RPC nodes | Deep slow work, no deadline |
| **Skald** | Sync | `/api/generate` | Ollama (local) | Precise deterministic output |

**Huginn** (Thought) — Fast queries. Routes to local GPU via Ollama, or to llama-server with RPC offload if healthy. Low latency, streaming capable.

**Muninn** (Memory) — Deep background work. Submit a job, get an ID, come back later. The queue worker processes through llama-server with `--rpc` offload to all healthy RPC nodes. 10-minute timeout (vs 300s sync). Slow nodes (RPi Zero, old CPUs) become useful — they grind through work at their own pace.

**Skald** (Sacred Words) — Precise output. Routes to local Ollama for deterministic results.

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

### Synchronous (Ollama-Compatible)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/generate` | POST | Text generation (real-time) |
| `/api/chat` | POST | Chat completion (real-time) |
| `/api/tags` | GET | List models |
| `/api/show` | POST | Model info |
| `/api/version` | GET | Version info |

### Asynchronous (Muninn Pattern)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/submit` | POST | Submit a background inference job |
| `/api/job/<id>` | GET | Check job status / get result |
| `/api/jobs` | GET | List all jobs and queue depth |

**Submit a job:**
```bash
curl -X POST http://localhost:11435/api/submit \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen2.5-3b","prompt":"Explain mycelium networks in 3 sentences","profile":"muninn"}'

# Returns: {"id":"job-1","status":"queued",...}
```

**Check result:**
```bash
curl http://localhost:11435/api/job/job-1

# Returns: {"id":"job-1","status":"complete","response":"...","tokens":86,"tok_per_sec":5.8,...}
```

### Mycelium Extensions

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/status` | GET | Node health and routing status |
| `/api/routes` | GET | Current routing configuration |
| `/api/rpc/probe` | GET | Probe RPC nodes for device memory |

### Routing Headers

Every response includes:
- `X-Mycelium-Profile`: Which Raven handled it
- `X-Mycelium-Node`: Which node served it
- `X-Mycelium-Model`: Which model was used

## Configuration

See `configs/mycelium.yaml` for the default config.
See `configs/mycelium-unified.yaml` for a full 7-node example.

### Llama-Server Section

```yaml
llama_server:
  enabled: true
  binary_path: "~/prima.cpp/llama-server"     # path in WSL
  model_path: "~/prima.cpp/models/model.gguf"  # .gguf model file
  port: 8090                                    # llama-server listen port
  wsl: true                                     # launch via wsl -e
  ngl: 0                                        # GPU layers (0=RPC-only, 99=full GPU)
  extra_args: []
```

When `enabled: true` and RPC nodes are healthy, mycelium-api launches llama-server with `--rpc <healthy_nodes>` on startup. All generate requests route through llama-server (distributed via RPC). Falls back to local Ollama if llama-server is down.

## Building RPC Servers

RPC compute nodes need a `rpc-server` binary built from [prima.cpp](https://github.com/AR-Davis/prima.cpp) with the INIT_TENSOR patch. Platform-specific build scripts are in `builds/`:

| Script | Platform | Devices |
|--------|----------|---------|
| `builds/build-x86_64.sh` | linux/amd64 | Shepherd, any Intel/AMD |
| `builds/build-arm64.sh` | linux/arm64 | Rhubarb (RPi5), Ember |
| `builds/build-armhf.sh` | linux/armhf | Crow/Wren (RPi Zero 2W) |
| `builds/build-termux.sh` | Android/Termux | Pixel 2 |

**Important:** Build from `github.com/AR-Davis/prima.cpp` (commit 8b69f20a), NOT from upstream ggml-org. The upstream added a mandatory HELLO handshake that our Go client doesn't implement. See `builds/README.md` for details.

## Cross-Compilation

```bash
make build-all     # amd64, arm64, armhf, windows
make deploy        # Create deployment packages
```

## Coven Mesh

| Node | Device | Protocol | Role | Status |
|------|--------|----------|------|--------|
| Hearth | TheTower (GTX 1650) | ollama | GPU primary | ✅ |
| Ember | HP DM1 (AMD E-350) | rpc | CPU worker | ✅ |
| Pixel 2 | Android/Termux | rpc | ARM worker | ✅ |
| Crow | RPi Zero 2W (armhf) | rpc | Edge compute | ✅ |
| Wren | RPi Zero 2W (armhf) | rpc | Edge compute | ✅ |
| Shepherd | Dell Latitude (i5) | ollama | API gateway | ✅ |
| Panther | RPi5 (Rhubarb) | ollama | ARM local | ✅ |

## RPC Protocol

Wire format: `| cmd (1 byte) | request_size (8 bytes LE) | request_data |`
Response: `| response_size (8 bytes LE) | response_data |`

| Code | Command | Purpose |
|------|---------|---------|
| 0 | ALLOC_BUFFER | Allocate memory on remote node |
| 1 | GET_ALIGNMENT | Query memory alignment |
| 2 | GET_MAX_SIZE | Query max buffer size |
| 3 | BUFFER_GET_BASE | Get buffer base pointer |
| 4 | FREE_BUFFER | Free a buffer |
| 5 | BUFFER_CLEAR | Zero out a buffer region |
| 6 | SET_TENSOR | Send tensor data to remote |
| 7 | GET_TENSOR | Retrieve tensor data |
| 8 | COPY_TENSOR | Copy tensor between buffers |
| 9 | GRAPH_COMPUTE | Execute computation graph |
| 10 | GET_DEVICE_MEMORY | Query free/total device memory |
| 11 | INIT_TENSOR | Initialize quantized tensor with padding (patched) |

## Packages

| Package | Purpose |
|---------|---------|
| `internal/api` | HTTP server, Ollama-compatible endpoints, request translation |
| `internal/config` | YAML configuration, node and routing definitions |
| `internal/node` | Node health checks, pool management, load-weighted selection |
| `internal/routing` | Three Ravens classifier and node router |
| `internal/rpc` | ggml-rpc binary protocol client (11 commands, pure Go) |
| `internal/llamaserver` | llama-server subprocess manager (launch, health, shutdown) |
| `internal/queue` | Async job queue with background worker (Muninn pattern) |

## License

Public domain (CC0).