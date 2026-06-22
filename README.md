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
| `/api/status` | GET | Node health & routing status |
| `/api/routes` | GET | Current routing configuration |

### Routing Headers

Every response includes routing metadata:
- `X-Mycelium-Profile`: Which Raven handled it (huginn/muninn/skald)
- `X-Mycelium-Node`: Which node served it
- `X-Mycelium-Model`: Which model was used

### Request Extensions

Pass `"profile": "huginn"`, `"profile": "muninn"`, or `"profile": "skald"` in any request to explicitly select a Raven.

## Configuration

See `configs/mycelium.yaml` for the full config format.

Key concepts:
- **Nodes** have a `pool` (local/remote/edge) and `weight` for load balancing
- **Routes** map Raven profiles to pools, with optional model overrides
- **Fallback** sends requests to local Ollama when all Mycelium nodes are down

## Architecture

```
Request -> Mycelium API (port 11435)
  |
  +--> Classify request (Huginn/Muninn/Skald)
  |
  +--> Select healthy node from route's pools
  |
  +--> Proxy to node's Ollama API (if available)
  |    or proxy to local Ollama with RPC backend
  |
  +--> Return response with routing headers
```

## The Three Ravens

Named after the Norse myth, borrowed from DOMM Terminal's Solo Room:

- **Huginn** (Thought) — Fast local queries. Small model on GPU. For quick answers, streaming, and low-latency needs.
- **Muninn** (Memory) — Deep remote processing. Large model, distributed across the mesh. For complex reasoning and long contexts.
- **Skald** (Sacred Words) — Precise vocabulary. Deterministic, local. For structured output and terminology.
