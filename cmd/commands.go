// Package cmd contains command implementations
package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/AR-Davis/prima_distributed_local/pkg/config"
	"github.com/AR-Davis/prima_distributed_local/pkg/detect"
	"github.com/spf13/cobra"
)

// runDetect implements the detect command
func runDetect(cmd *cobra.Command, args []string) error {
	fmt.Println("🔍 Detecting hardware...")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Detect hardware
	profile, err := detect.Detect(ctx, "")
	if err != nil {
		return fmt.Errorf("hardware detection failed: %w", err)
	}

	// Pretty print results
	printHardwareProfile(profile)

	return nil
}

// printHardwareProfile displays hardware info in a readable format
func printHardwareProfile(p *detect.HardwareProfile) {
	fmt.Println("╔════════════════════════════════════════════════════════╗")
	fmt.Println("║          Hardware Detection Results                    ║")
	fmt.Println("╚════════════════════════════════════════════════════════╝")
	fmt.Println()

	// CPU
	fmt.Printf("🖥️  CPU: %s\n", p.CPU.ModelName)
	fmt.Printf("   Cores: %d | Threads: %d\n", p.CPU.Cores, p.CPU.Threads)
	fmt.Printf("   AVX2: %v | AVX-512: %v\n", p.CPU.HasAVX2, p.CPU.HasAVX512)
	fmt.Printf("   Score: %d/30\n", p.CPU.Score)
	fmt.Println()

	// Memory
	fmt.Printf("💾 Memory: %.1f GB total, %.1f GB available\n",
		p.Memory.TotalGB, p.Memory.AvailableGB)
	fmt.Printf("   Score: %d/40\n", p.Memory.Score)
	fmt.Println()

	// GPUs
	if len(p.GPUs) > 0 {
		fmt.Println("🎮 GPUs:")
		for _, gpu := range p.GPUs {
			fmt.Printf("   %s (%s): %.1f GB VRAM | Score: %d/50\n",
				gpu.Model, gpu.Vendor, gpu.VRAMGB, gpu.Score)
		}
	} else {
		fmt.Println("🎮 GPU: None detected")
	}
	fmt.Println()

	// Disk
	fmt.Printf("💿 Disk: Type=%s, Free=%.1f GB\n", p.Disk.Type, p.Disk.FreeGB)
	fmt.Printf("   Score: %d/15\n", p.Disk.Score)
	fmt.Println()

	// Network
	fmt.Printf("🌐 Network: Hostname=%s\n", p.Network.Hostname)
	fmt.Printf("   Ethernet: %v | WiFi: %v\n", p.Network.HasEthernet, p.Network.HasWiFi)
	fmt.Println()

	// OS
	fmt.Printf("🖥️  OS: %s %s (%s)\n", p.OS.Name, p.OS.Version, p.OS.Arch)
	fmt.Println()

	// Summary
	fmt.Println("╔════════════════════════════════════════════════════════╗")
	fmt.Printf("║  TOTAL SCORE: %d/135                                    ║\n", p.Score)
	fmt.Printf("║  RECOMMENDED TIER: %s%-15s%s                          ║\n",
		getTierColor(p.Tier), p.Tier, "\033[0m")
	fmt.Println("╚════════════════════════════════════════════════════════╝")
}

// getTierColor returns ANSI color code for tier
func getTierColor(tier string) string {
	switch tier {
	case "full":
		return "\033[32m" // Green
	case "middle":
		return "\033[33m" // Yellow
	case "lightweight":
		return "\033[34m" // Blue
	default:
		return "\033[0m"
	}
}

// runInstall implements the install command
func runInstall(cmd *cobra.Command, args []string) error {
	fmt.Println("📦 Installing Prima Distributed Local...")
	fmt.Println()

	// Determine config path
	cfgPath := configPath
	if cfgPath == "" {
		cfgPath = config.ConfigPath()
	}

	// Check if config exists
	var cfg *config.Config
	var err error

	if config.Exists(cfgPath) {
		fmt.Printf("📄 Loading existing config: %s\n", cfgPath)
		cfg, err = config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	} else {
		fmt.Println("🆕 Creating new configuration...")
		cfg = config.DefaultConfig()

		// Detect hardware
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		profile, err := detect.Detect(ctx, "")
		if err != nil {
			fmt.Printf("⚠️  Hardware detection warning: %v\n", err)
		} else {
			// Auto-set tier based on hardware
			cfg.Node.Tier = config.NodeTier(profile.Tier)
			cfg.Node.Name = profile.Network.Hostname

			// Set GPU layers if GPU detected
			if len(profile.GPUs) > 0 {
				bestGPU := profile.GPUs[0]
				for _, gpu := range profile.GPUs {
					if gpu.Score > bestGPU.Score {
						bestGPU = gpu
					}
				}
				cfg.Node.Resources.GPULayers = int(bestGPU.VRAMGB * 2) // Rough estimate
			}
		}
	}

	// Save config
	if err := cfg.Save(cfgPath); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("✅ Config saved to: %s\n", cfgPath)
	fmt.Println()

	// Show installation summary
	fmt.Println("📋 Installation Summary:")
	fmt.Printf("   Node name: %s\n", cfg.Node.Name)
	fmt.Printf("   Tier: %s\n", cfg.Node.Tier)
	fmt.Printf("   Resources: %d%% CPU, %d GB RAM, %d GPU layers\n",
		cfg.Node.Resources.CPUPercent,
		cfg.Node.Resources.MemoryGB,
		cfg.Node.Resources.GPULayers)
	fmt.Printf("   Head node: %s\n", cfg.Cluster.HeadNode)
	fmt.Printf("   Access model: %s\n", cfg.Cluster.AccessModel)
	fmt.Println()

	// TODO: Actually install service
	fmt.Println("⚠️  Service installation not yet implemented")
	fmt.Println("   Run 'prima-installer run' to test in foreground mode")

	return nil
}

// runUninstall implements the uninstall command
func runUninstall(cmd *cobra.Command, args []string) error {
	fmt.Println("🗑️  Uninstalling Prima...")
	
	// TODO: Stop and remove service
	
	// Remove config
	cfgPath := configPath
	if cfgPath == "" {
		cfgPath = config.ConfigPath()
	}
	
	if config.Exists(cfgPath) {
		if err := os.Remove(cfgPath); err != nil {
			return fmt.Errorf("failed to remove config: %w", err)
		}
		fmt.Printf("🗑️  Removed config: %s\n", cfgPath)
	}
	
	fmt.Println("✅ Uninstalled successfully")
	return nil
}

// runRun implements the run command
func runRun(cmd *cobra.Command, args []string) error {
	fmt.Println("🚀 Starting Prima worker...")
	
	// Load config
	cfgPath := configPath
	if cfgPath == "" {
		cfgPath = config.ConfigPath()
	}
	
	if !config.Exists(cfgPath) {
		return fmt.Errorf("config not found at %s, run 'prima-installer install' first", cfgPath)
	}
	
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	
	fmt.Printf("📄 Config: %s\n", cfgPath)
	fmt.Printf("🔗 Connecting to head node: %s\n", cfg.Cluster.HeadNode)
	fmt.Printf("⚙️  Tier: %s\n", cfg.Node.Tier)
	fmt.Println()
	
	// TODO: Start worker process
	fmt.Println("⏳ Worker process would start here...")
	fmt.Println("   (Prima binary management not yet implemented)")
	
	return nil
}

// runStatus implements the status command
func runStatus(cmd *cobra.Command, args []string) error {
	fmt.Println("📊 Prima Status")
	fmt.Println()
	
	// Load config
	cfgPath := configPath
	if cfgPath == "" {
		cfgPath = config.ConfigPath()
	}
	
	if !config.Exists(cfgPath) {
		fmt.Println("❌ Not installed")
		fmt.Printf("   Config not found at: %s\n", cfgPath)
		return nil
	}
	
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	
	fmt.Println("Configuration:")
	fmt.Printf("   Config path: %s\n", cfgPath)
	fmt.Printf("   Node name: %s\n", cfg.Node.Name)
	fmt.Printf("   Tier: %s\n", cfg.Node.Tier)
	fmt.Printf("   Head node: %s\n", cfg.Cluster.HeadNode)
	fmt.Println()
	
	// TODO: Check service status
	fmt.Println("Service: Not implemented")
	
	return nil
}

// runPair implements the pair command
func runPair(cmd *cobra.Command, args []string) error {
	fmt.Println("🔗 Pairing with head node...")
	fmt.Println()
	
	// TODO: Implement Noise XX handshake with QR code
	fmt.Println("⚠️  Pairing not yet implemented")
	fmt.Println("   Use 'prima-installer install' with manual config for now")
	
	return nil
}

// runWeb implements the web command
func runWeb(cmd *cobra.Command, args []string) error {
	// Load config
	cfgPath := configPath
	if cfgPath == "" {
		cfgPath = config.ConfigPath()
	}
	
	var cfg *config.Config
	var err error
	
	if config.Exists(cfgPath) {
		cfg, err = config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	} else {
		cfg = config.DefaultConfig()
	}
	
	addr := fmt.Sprintf("%s:%d", cfg.WebDashboard.Host, cfg.WebDashboard.Port)
	
	fmt.Printf("🌐 Starting web dashboard on http://%s\n", addr)
	fmt.Println()
	
	// TODO: Start web server
	fmt.Println("⚠️  Web dashboard not yet implemented")
	
	return nil
}