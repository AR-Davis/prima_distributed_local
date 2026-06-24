// Package config defines the Mycelium API configuration.
// Nodes, routing rules, and Ollama-compatible settings.
package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for mycelium-api.
type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Routing      RoutingConfig      `yaml:"routing"`
	Nodes        []NodeConfig       `yaml:"nodes"`
	Logging      LoggingConfig      `yaml:"logging"`
	LlamaServer  LlamaServerConfig  `yaml:"llama_server"`
}

// LlamaServerConfig controls the llama-server subprocess used for
// RPC-distributed inference. When RPC nodes are healthy, mycelium-api
// launches llama-server with --rpc to offload compute.
type LlamaServerConfig struct {
	Enabled    bool     `yaml:"enabled"`
	BinaryPath string   `yaml:"binary_path"`  // path in WSL, e.g. ~/prima.cpp/llama-server
	ModelPath  string   `yaml:"model_path"`   // path in WSL, e.g. ~/prima.cpp/models/qwen2.5-3b-instruct-q4_k_m.gguf
	Port       int      `yaml:"port"`         // localhost port for llama-server (default 8090)
	WSL        bool     `yaml:"wsl"`          // launch via wsl -e bash -c
	NGL        int      `yaml:"ngl"`          // GPU layers (0 for RPC-only, 99 for full GPU)
	ExtraArgs  []string `yaml:"extra_args"`   // additional CLI args
}

// ServerConfig controls the HTTP listener.
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// RoutingConfig maps request profiles to node pools.
// Named after the Three Ravens of DOMM: fast, deep, sacred.
type RoutingConfig struct {
	Huginn        RouteRule `yaml:"huginn"`
	Muninn        RouteRule `yaml:"muninn"`
	Skald         RouteRule `yaml:"skald"`
	Default       string    `yaml:"default"`
	FallbackLocal string    `yaml:"fallback_local"`
}

// RouteRule selects which nodes handle a routing profile.
type RouteRule struct {
	Pools   []string      `yaml:"pools"`
	Model   string        `yaml:"model,omitempty"`
	Timeout time.Duration `yaml:"timeout,omitempty"`
}

// NodeConfig describes a single Mycelium inference node.
type NodeConfig struct {
	Name     string        `yaml:"name"`
	Host     string        `yaml:"host"`
	Port     int           `yaml:"port"`
	APIPort  int           `yaml:"api_port"`
	Type     NodeType      `yaml:"type"`
	Protocol ProtocolType  `yaml:"protocol"`
	Pool     string        `yaml:"pool"`
	Weight   int           `yaml:"weight"`
	Timeout  time.Duration `yaml:"timeout,omitempty"`
}

// NodeType categorizes node hardware.
type NodeType string

const (
	NodeTypeGPU NodeType = "gpu"
	NodeTypeCPU NodeType = "cpu"
	NodeTypeARM NodeType = "arm"
)

// ProtocolType represents the communication protocol for a node.
type ProtocolType string

const (
	ProtocolOllama ProtocolType = "ollama"
	ProtocolRPC    ProtocolType = "rpc"
)

// LoggingConfig controls log output.
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// DefaultConfig returns a working configuration for TheTower.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 11435,
		},
		Routing: RoutingConfig{
			Huginn: RouteRule{
				Pools:   []string{"local"},
				Timeout: 30 * time.Second,
			},
			Muninn: RouteRule{
				Pools:   []string{"remote", "edge", "local"},
				Timeout: 300 * time.Second,
			},
			Skald: RouteRule{
				Pools:   []string{"local"},
				Timeout: 30 * time.Second,
			},
			Default:       "huginn",
			FallbackLocal: "localhost:11434",
		},
		Nodes: []NodeConfig{
			{
				Name:     "hearth",
				Host:     "localhost",
				APIPort:  11434,
				Type:     NodeTypeGPU,
				Protocol: ProtocolOllama,
				Pool:     "local",
				Weight:   10,
			},
			{
				Name:     "ember",
				Host:     "100.90.116.1",
				Port:     50052,
				Type:     NodeTypeCPU,
				Protocol: ProtocolRPC,
				Pool:     "remote",
				Weight:   1,
				Timeout:  300 * time.Second,
			},
			{
				Name:     "pixel-2",
				Host:     "100.77.170.98",
				Port:     50052,
				Type:     NodeTypeARM,
				Protocol: ProtocolRPC,
				Pool:     "edge",
				Weight:   3,
				Timeout:  60 * time.Second,
			},
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		LlamaServer: LlamaServerConfig{
			Enabled:    false,
			BinaryPath:  "~/prima.cpp/llama-server",
			ModelPath:   "~/prima.cpp/models/qwen2.5-3b-instruct-q4_k_m.gguf",
			Port:        8090,
			WSL:         true,
			NGL:         0,
			ExtraArgs:   []string{},
		},
	}
}

// Load reads configuration from a YAML file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
