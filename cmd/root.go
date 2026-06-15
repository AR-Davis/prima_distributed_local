// Package cmd provides the CLI commands for prima-installer
package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	// Version is set during build
	Version = "dev"
	// ConfigPath override
	configPath string
)

// rootCmd is the base command
var rootCmd = &cobra.Command{
	Use:   "prima-installer",
	Short: "Prima Distributed Local - Portable cluster installer",
	Long: `Prima Installer is a portable tool that runs from a USB drive to
set up and configure machines as part of a distributed LLM inference cluster.

Supports three tiers:
  - Lightweight: Background service, minimal resources
  - Middle: User-controlled, moderate resources  
  - Full: Dedicated node, maximum resources

Access models:
  - Personal: Local LAN only
  - Friend: QR pairing for friend groups
  - Discord: Public bot with rate limiting`,
	Version: Version,
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "Config file path (default: auto-detect)")
	
	// Add subcommands
	rootCmd.AddCommand(detectCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(uninstallCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(pairCmd)
	rootCmd.AddCommand(webCmd)
	rootCmd.AddCommand(tuiCmd)
}

// detectCmd detects hardware
var detectCmd = &cobra.Command{
	Use:   "detect",
	Short: "Detect hardware and display profile",
	RunE:  runDetect,
}

// installCmd installs the service
var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Prima as a system service",
	Long:  `Installs Prima as a background service with the appropriate tier for this hardware.`,
	RunE:  runInstall,
}

// uninstallCmd removes the service
var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove Prima system service",
	RunE:  runUninstall,
}

// runCmd runs Prima directly (foreground)
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run Prima worker in foreground",
	Long:  `Runs the Prima worker process directly without installing as a service. Useful for testing.`,
	RunE:  runRun,
}

// statusCmd shows service status
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Prima service status",
	RunE:  runStatus,
}

// pairCmd handles QR pairing
var pairCmd = &cobra.Command{
	Use:   "pair",
	Short: "Pair with head node via QR code",
	Long:  `Displays or scans a QR code to securely pair with a cluster head node.`,
	RunE:  runPair,
}

// webCmd starts web dashboard
var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Start web dashboard",
	RunE:  runWeb,
}

// tuiCmd starts TUI
var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Start interactive terminal UI",
	RunE:  runTUI,
}