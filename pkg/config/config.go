// Package config handles loading, saving, and validating cluster configuration
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/BurntSushi/toml"
)

// AccessModel defines how the cluster is accessed
type AccessModel string

const (
	AccessPersonal AccessModel = "personal"
	AccessFriend   AccessModel = "friend"
	AccessDiscord  AccessModel = "discord"
)

// NodeTier defines the resource commitment level
type NodeTier string

const (
	TierAuto        NodeTier = "auto"
	TierLightweight NodeTier = "lightweight"
	TierMiddle      NodeTier = "middle"
	TierFull        NodeTier = "full"
)

// Cluster config - shared settings for the cluster
type Cluster struct {
	Name        string      `toml:"name" validate:"required"`
	HeadNode    string      `toml:"head_node" validate:"required"`
	AccessModel AccessModel `toml:"access_model" validate:"required,oneof=personal friend discord"`
	AuthToken   string      `toml:"auth_token,omitempty"` // For friend/discord mode
	NetworkKey  string      `toml:"network_key,omitempty"` // For personal LAN mode
}

// ResourceLimits controls hardware allocation
type ResourceLimits struct {
	CPUPercent  int  `toml:"cpu_percent" validate:"min=1,max=100"`
	MemoryGB    int  `toml:"memory_gb" validate:"min=1"`
	GPULayers   int  `toml:"gpu_layers" validate:"min=0"`
	DiskCacheGB int  `toml:"disk_cache_gb,omitempty"`
}

// Schedule controls when the node runs
type Schedule struct {
	Enabled        bool     `toml:"enabled"`
	IdleDetection  bool     `toml:"idle_detection"`
	IdleMinutes    int      `toml:"idle_minutes" validate:"min=1"`
	ActiveHours    string   `toml:"active_hours,omitempty"` // e.g., "22:00-08:00"
	DaysOfWeek     []string `toml:"days_of_week,omitempty"` // e.g., ["Mon", "Tue", "Wed", "Thu", "Fri"]
}

// Node config - specific to this machine
type Node struct {
	Name      string         `toml:"name"`
	Tier      NodeTier       `toml:"tier" validate:"oneof=auto lightweight middle full"`
	Resources ResourceLimits `toml:"resources"`
	Schedule  Schedule       `toml:"schedule"`
}

// WebDashboard config
type WebDashboard struct {
	Enabled bool   `toml:"enabled"`
	Host    string `toml:"host"`
	Port    int    `toml:"port" validate:"min=1024,max=65535"`
}

// Update config
type Update struct {
	Channel    string `toml:"channel" validate:"oneof=stable beta nightly"`
	AutoUpdate bool   `toml:"auto_update"`
	CheckInterval string `toml:"check_interval"` // e.g., "24h"
}

// Config is the top-level configuration
type Config struct {
	Cluster      Cluster      `toml:"cluster"`
	Node         Node         `toml:"node"`
	WebDashboard WebDashboard `toml:"web_dashboard"`
	Update       Update       `toml:"update"`
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() *Config {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "prima-node"
	}

	return &Config{
		Cluster: Cluster{
			Name:        "my-prima-cluster",
			HeadNode:    "192.168.1.10:50052",
			AccessModel: AccessPersonal,
		},
		Node: Node{
			Name: hostname,
			Tier: TierAuto,
			Resources: ResourceLimits{
				CPUPercent: 50,
				MemoryGB:   4,
				GPULayers:  0, // Auto-detect
			},
			Schedule: Schedule{
				Enabled:       true,
				IdleDetection: true,
				IdleMinutes:   5,
				DaysOfWeek:    []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"},
			},
		},
		WebDashboard: WebDashboard{
			Enabled: true,
			Host:    "127.0.0.1",
			Port:    8080,
		},
		Update: Update{
			Channel:       "stable",
			AutoUpdate:    true,
			CheckInterval: "24h",
		},
	}
}

// Load reads configuration from a TOML file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// Save writes configuration to a TOML file
func (c *Config) Save(path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal to TOML
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Add header comment
	header := "# Prima Distributed Local - Cluster Configuration\n"
	header += "# Generated: " + time.Now().Format(time.RFC3339) + "\n\n"

	if err := os.WriteFile(path, []byte(header+string(data)), 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// Validate checks configuration for errors
func (c *Config) Validate() error {
	if c.Cluster.Name == "" {
		return fmt.Errorf("cluster.name is required")
	}
	if c.Cluster.HeadNode == "" {
		return fmt.Errorf("cluster.head_node is required")
	}
	if c.Cluster.AccessModel == "" {
		c.Cluster.AccessModel = AccessPersonal
	}
	if c.Node.Tier == "" {
		c.Node.Tier = TierAuto
	}
	if c.Node.Resources.CPUPercent == 0 {
		c.Node.Resources.CPUPercent = 50
	}
	if c.Node.Resources.MemoryGB == 0 {
		c.Node.Resources.MemoryGB = 4
	}
	return nil
}

// ConfigPath returns the default configuration file path
func ConfigPath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Prima", "cluster.toml")
	case "darwin":
		return filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "Prima", "cluster.toml")
	default: // Linux and others
		return filepath.Join(os.Getenv("HOME"), ".config", "prima", "cluster.toml")
	}
}

// DataDir returns the default data directory
func DataDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "Prima")
	case "darwin":
		return filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "Prima")
	default:
		return filepath.Join(os.Getenv("HOME"), ".local", "share", "prima")
	}
}

// CacheDir returns the default cache directory
func CacheDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "Prima", "Cache")
	case "darwin":
		return filepath.Join(os.Getenv("HOME"), "Library", "Caches", "Prima")
	default:
		return filepath.Join(os.Getenv("HOME"), ".cache", "prima")
	}
}

// Exists checks if a config file exists at the given path
func Exists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}