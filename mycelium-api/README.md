# Mycelium API

Ollama-compatible HTTP gateway for distributed inference across heterogeneous nodes.

Routes inference requests using the **Three Ravens** strategy:
- **Huginn** → Fast local (GPU, low latency)
- **Muninn** → Deep remote (CPU, distributed compute)
- **Skald** → Precise local (deterministic vocabulary)

Any Ollama client can talk to it without modification.

## Quick Start

```bash
# Build
go build -o mycelium-api ./cmd/mycelium-api

# Run with default config
./mycelium-api

# Run with custom config
./mycelium-api -config /path/to/mycelium.yaml

# Custom port
./mycelium-api -port 8080
```

## Architecture

```
  Ollama Client ──→ :11435 (mycelium-api) ──→ Route (Huginn/Muninn/Skald)
                                                      │
                                          ┌───────────┼───────────┐
                                          │           │           │
                                     Hearth       Ember       Pixel 2
                                     (GPU)        (CPU)       (ARM)
                                     :11434       :50052      :50052
                                     local        remote      edge
```

## API Endpoints

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/` | GET | Server info |
| `/api/generate` | POST | Ollama-compatible generation |
| `/api/chat` | POST | Ollama-compatible chat |
| `/api/tags` | GET | Proxy to Ollama model list |
| `/api/status` | GET | Node health, routing config |
| `/api/routes` | GET | Three Ravens routing details |
| `/api/version` | GET | Version string |

### Response Headers

Every response includes:
- `X-Mycelium-Profile`: Which Raven handled it (huginn/muninn/skald)
- `X-Mycelium-Node`: Which node processed it
- `X-Mycelium-Model`: The model sent to the node

## Configuration

See `configs/mycelium.yaml` for a complete example. Key concepts:

- **Nodes** define hardware targets (GPU, CPU, ARM) with pools and weights
- **Routing** defines which pools each Raven uses and timeout values
- **Fallback** defines a local Ollama instance for when all nodes are down
- **Health checks** run every 30 seconds via TCP dial

### Node Types

| Type | Description | Protocol |
|------|------------|----------|
| `gpu` | GPU-accelerated node (Ollama) | HTTP on `api_port` |
| `cpu` | CPU worker (prima.cpp RPC) | TCP on `port` |
| `arm` | ARM edge device (prima.cpp RPC) | TCP on `port` |

## Three Ravens Routing

Routing selects **nodes**, not models. The user's requested model passes through unchanged. The Raven decides *where* to run it, not *what* to run.

| Raven | Pool | Purpose | Timeout |
|-------|------|---------|---------|
| **Huginn** | local | Fast local queries | 30s |
| **Muninn** | remote, edge, local | Deep remote compute | 300s |
| **Skald** | local | Precise vocabulary | 30s |

## Project Structure

```
mycelium-api/
├── cmd/mycelium-api/main.go    # Entry point
├── internal/api/api.go          # HTTP server and endpoints
├── internal/config/config.go    # YAML configuration
├── internal/node/node.go        # Node health and selection
├── internal/routing/routing.go  # Three Ravens routing
├── configs/mycelium.yaml        # Default configuration
└── README.md
```

## License

MIT
