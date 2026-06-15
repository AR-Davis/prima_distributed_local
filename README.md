# Prima Distributed Local

A portable installer for distributed LLM inference clusters. Run from a USB drive to set up any PC as a node in a Prima.cpp cluster.

## Features

- 🔌 **Portable**: Runs from USB flash drive
- 🖥️ **Auto-detect**: Hardware detection with scoring (CPU, RAM, GPU, Disk, Network)
- ⚙️ **Three Tiers**: 
  - **Lightweight** (<50 pts): Background service, minimal resources
  - **Middle** (50-99 pts): User-controlled, moderate resources
  - **Full** (100+ pts): Dedicated node, maximum resources
- 🔗 **Access Models**:
  - **Personal**: Local LAN with mDNS discovery
  - **Friend**: QR pairing with Noise XX handshake
  - **Discord**: Public bot with rate limiting
- 🪟 **Cross-platform**: Linux, macOS, Windows
- 🎨 **Interfaces**: CLI, TUI (bubbletea), Web dashboard

## Quick Start

### From USB Drive

```bash
# Plug in USB and run (any platform)
./prima-installer detect    # See your hardware score
./prima-installer install   # Install with auto-detected tier
./prima-installer run         # Run worker in foreground (test)
```

### Install as Service

```bash
# Linux (systemd)
sudo ./prima-installer install
sudo systemctl status prima

# macOS (launchd)
./prima-installer install
launchctl list | grep prima

# Windows (Administrator)
prima-installer.exe install
# Service auto-starts
```

## Hardware Scoring

| Component | Points | Criteria |
|-----------|--------|----------|
| **CPU** | 0-30 | AVX2=10, AVX-512=15, +2/core |
| **Memory** | 0-40 | 4GB=15, 8GB=25, 16GB=40 |
| **GPU** | 0-50 | 4GB=20, 8GB=35, 12GB=50 |
| **Disk** | 0-15 | SSD=10, NVMe=15 |
| **Total** | **135** | |

**Tiers:**
- ≥100 points → Full tier
- 50-99 points → Middle tier
- <50 points → Lightweight tier

## Configuration

Config file location:
- **Linux**: `~/.config/prima/cluster.toml`
- **macOS**: `~/Library/Application Support/Prima/cluster.toml`
- **Windows**: `%APPDATA%\Prima\cluster.toml`

Example configuration:

```toml
[cluster]
name = "my-cluster"
head_node = "192.168.1.10:50052"
access_model = "personal"  # or "friend", "discord"

[node]
tier = "auto"  # or "lightweight", "middle", "full"

[node.resources]
cpu_percent = 50
memory_gb = 4
gpu_layers = 10

[node.schedule]
enabled = true
idle_detection = true
idle_minutes = 5
```

## Commands

| Command | Description |
|---------|-------------|
| `detect` | Detect and display hardware profile |
| `install` | Install as system service |
| `uninstall` | Remove system service |
| `run` | Run worker in foreground |
| `status` | Show service status |
| `pair` | Pair with head node via QR |
| `web` | Start web dashboard |
| `tui` | Start interactive terminal UI |

## Building

### Requirements

- Go 1.22+
- make

### Build Current Platform

```bash
make build
```

### Build All Platforms

```bash
make build-all
```

### Platform-Specific

```bash
make build-linux-amd64
make build-linux-arm64
make build-darwin-amd64
make build-darwin-arm64
make build-windows
```

## Architecture

```
prima_distributed_local/
├── cmd/prima-installer/     # CLI entry point
├── pkg/
│   ├── config/              # TOML configuration
│   ├── detect/              # Hardware detection
│   ├── service/             # systemd/launchd/Windows Service
│   ├── pairing/             # Noise XX crypto
│   ├── web/                 # Dashboard server
│   ├── worker/              # Prima process wrapper
│   └── update/              # Self-updater
├── config/
│   ├── cluster.default.toml
│   └── profiles/            # Pre-configured templates
└── assets/web/            # Embedded dashboard UI
```

## Security

### Personal Mode
- mDNS discovery on local network
- Optional pre-shared key

### Friend Mode
- QR code pairing with ephemeral X25519 keys
- Noise XX handshake for mutual authentication
- Forward secrecy

### Discord Mode
- Rate limiting (10 req/min default)
- Request sandboxing (timeout, memory limits)
- Input validation (4K char limit)

## Roadmap

- [x] Hardware detection
- [x] Configuration management
- [x] CLI framework
- [ ] Service installation (systemd/launchd/Windows)
- [ ] Noise XX pairing
- [ ] QR code display/scan
- [ ] TUI with Bubble Tea
- [ ] Web dashboard
- [ ] Prima binary management
- [ ] Auto-updater
- [ ] Code signing (Windows)

## License

MIT License - See LICENSE file

## Contributing

This project is part of the Prima.cpp ecosystem for distributed LLM inference on consumer hardware.

## Acknowledgments

- [Prima.cpp](https://github.com/fengwenjiao/prima.cpp) - The underlying inference engine
- [llama.cpp](https://github.com/ggml-org/llama.cpp) - Foundation for local LLMs
- [Go](https://golang.org/) - The language that makes portable tooling possible
