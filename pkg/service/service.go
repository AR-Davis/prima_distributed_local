// Package service handles installation as a system service
package service

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/AR-Davis/prima_distributed_local/pkg/config"
	"github.com/kardianos/service"
)

// Service manages the prima system service
type Service struct {
	cfg     *config.Config
	service service.Service
	logger  *Logger
}

// Logger implements service.Logger
type Logger struct {
	LogFile string
}

func (l *Logger) Infof(format string, args ...interface{}) {
	fmt.Printf("[INFO] "+format+"\n", args...)
}

func (l *Logger) Warnf(format string, args ...interface{}) {
	fmt.Printf("[WARN] "+format+"\n", args...)
}

func (l *Logger) Errorf(format string, args ...interface{}) {
	fmt.Printf("[ERROR] "+format+"\n", args...)
}

// New creates a new service manager
func New(cfg *config.Config) (*Service, error) {
	logger := &Logger{}

	// Get the current executable path
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	// Make path absolute
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	svcConfig := &service.Config{
		Name:        "prima-distributed",
		DisplayName: "Prima Distributed Local",
		Description: "Distributed LLM inference node for Prima.cpp cluster",
		Arguments:   []string{"run", "--config", config.ConfigPath()},
	}

	// Platform-specific configurations
	switch runtime.GOOS {
	case "linux":
		configureLinuxService(svcConfig, cfg)
	case "darwin":
		configureDarwinService(svcConfig, cfg)
	case "windows":
		configureWindowsService(svcConfig, cfg)
	}

	// Create the program
	prg := &program{cfg: cfg}

	svc, err := service.New(prg, svcConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	return &Service{
		cfg:     cfg,
		service: svc,
		logger:  logger,
	}, nil
}

// Install installs the service
func (s *Service) Install() error {
	// Check if already installed
	status, err := s.Status()
	if err == nil && status != StatusNotInstalled {
		return fmt.Errorf("service already installed (status: %s)", status)
	}

	if err := s.service.Install(); err != nil {
		return fmt.Errorf("failed to install service: %w", err)
	}

	// Start the service
	if err := s.service.Start(); err != nil {
		return fmt.Errorf("service installed but failed to start: %w", err)
	}

	return nil
}

// Uninstall removes the service
func (s *Service) Uninstall() error {
	// Stop if running
	s.service.Stop()

	if err := s.service.Uninstall(); err != nil {
		return fmt.Errorf("failed to uninstall service: %w", err)
	}

	return nil
}

// Start starts the service
func (s *Service) Start() error {
	return s.service.Start()
}

// Stop stops the service
func (s *Service) Stop() error {
	return s.service.Stop()
}

// Restart restarts the service
func (s *Service) Restart() error {
	return s.service.Restart()
}

// Status represents service status
type Status string

const (
	StatusUnknown       Status = "unknown"
	StatusRunning       Status = "running"
	StatusStopped       Status = "stopped"
	StatusNotInstalled  Status = "not_installed"
)

// Status returns the current service status
func (s *Service) Status() (Status, error) {
	status, err := s.service.Status()
	if err != nil {
		return StatusUnknown, err
	}

	switch status {
	case service.StatusRunning:
		return StatusRunning, nil
	case service.StatusStopped:
		return StatusStopped, nil
	default:
		return StatusUnknown, nil
	}
}

// Run runs the service (blocks)
func (s *Service) Run() error {
	return s.service.Run()
}

// program implements service.Interface
type program struct {
	cfg    *config.Config
	worker *Worker
}

func (p *program) Start(s service.Service) error {
	// Start should not block. Do the actual work async.
	go p.run()
	return nil
}

func (p *program) run() {
	// Create and start the worker
	worker, err := NewWorker(p.cfg)
	if err != nil {
		fmt.Printf("Failed to create worker: %v\n", err)
		return
	}

	p.worker = worker

	if err := worker.Start(); err != nil {
		fmt.Printf("Failed to start worker: %v\n", err)
		return
	}
}

func (p *program) Stop(s service.Service) error {
	if p.worker != nil {
		return p.worker.Stop()
	}
	return nil
}

// Platform-specific configuration

func configureLinuxService(cfg *service.Config, nodeCfg *config.Config) {
	// systemd-specific options
	cfg.Option = service.KeyValue{
		"SystemdScript": `[Unit]
Description=Prima Distributed Local
After=network.target

[Service]
Type=simple
User=%s
ExecStart=%s
Restart=on-failure
RestartSec=10

# Resource limits
CPUQuota=%d%%
MemoryMax=%dG

# Sandboxing
PrivateTmp=true
ProtectSystem=strict
ProtectHome=read-only

[Install]
WantedBy=multi-user.target
`,
	}
}

func configureDarwinService(cfg *service.Config, nodeCfg *config.Config) {
	// launchd-specific options
	cfg.Option = service.KeyValue{
		"LaunchdConfig": `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.prima.distributed</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>run</string>
        <string>--config</string>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s/Library/Logs/prima.log</string>
    <key>StandardErrorPath</key>
    <string>%s/Library/Logs/prima-error.log</string>
</dict>
</plist>
`,
	}
}

func configureWindowsService(cfg *service.Config, nodeCfg *config.Config) {
	// Windows-specific options
	cfg.Option = service.KeyValue{
		"DelayedAutoStart": true,
	}
}
