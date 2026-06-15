// Package detect handles hardware detection without external dependencies
package detect

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// HardwareProfile contains detected hardware information
type HardwareProfile struct {
	CPU     CPUInfo     `json:"cpu"`
	Memory  MemoryInfo  `json:"memory"`
	GPUs    []GPUInfo   `json:"gpus"`
	Disk    DiskInfo    `json:"disk"`
	Network NetworkInfo `json:"network"`
	OS      OSInfo      `json:"os"`
	Score   int         `json:"score"`
	Tier    string      `json:"tier"`
}

// CPUInfo contains CPU specifications
type CPUInfo struct {
	ModelName string `json:"model_name"`
	Cores     int    `json:"cores"`
	Threads   int    `json:"threads"`
	HasAVX2   bool   `json:"has_avx2"`
	HasAVX512 bool   `json:"has_avx512"`
	Score     int    `json:"score"`
}

// MemoryInfo contains memory specifications
type MemoryInfo struct {
	TotalGB     float64 `json:"total_gb"`
	AvailableGB float64 `json:"available_gb"`
	Score       int     `json:"score"`
}

// GPUInfo contains GPU specifications
type GPUInfo struct {
	Model  string  `json:"model"`
	Vendor string  `json:"vendor"`
	VRAMGB float64 `json:"vram_gb"`
	Score  int     `json:"score"`
}

// DiskInfo contains disk specifications
type DiskInfo struct {
	Type    string  `json:"type"`
	TotalGB float64 `json:"total_gb"`
	FreeGB  float64 `json:"free_gb"`
	Score   int     `json:"score"`
}

// NetworkInfo contains network specifications
type NetworkInfo struct {
	Hostname   string   `json:"hostname"`
	HasEthernet bool    `json:"has_ethernet"`
	HasWiFi     bool    `json:"has_wifi"`
}

// OSInfo contains operating system information
type OSInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Arch    string `json:"arch"`
}

// Detect performs comprehensive hardware detection
func Detect(ctx context.Context, headNode string) (*HardwareProfile, error) {
	profile := &HardwareProfile{}

	switch runtime.GOOS {
	case "linux":
		if err := profile.detectLinux(ctx); err != nil {
			return nil, err
		}
	case "darwin":
		if err := profile.detectDarwin(ctx); err != nil {
			return nil, err
		}
	case "windows":
		if err := profile.detectWindows(ctx); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	profile.calculateScore()
	profile.determineTier()

	return profile, nil
}

// detectLinux detects hardware on Linux
func (p *HardwareProfile) detectLinux(ctx context.Context) error {
	// CPU detection
	if err := p.detectLinuxCPU(); err != nil {
		return err
	}

	// Memory detection
	if err := p.detectLinuxMemory(); err != nil {
		return err
	}

	// GPU detection
	p.detectLinuxGPU()

	// Disk detection
	if err := p.detectLinuxDisk(); err != nil {
		return err
	}

	// Network detection
	if err := p.detectLinuxNetwork(); err != nil {
		return err
	}

	// OS detection
	p.detectLinuxOS()

	return nil
}

// detectDarwin detects hardware on macOS
func (p *HardwareProfile) detectDarwin(ctx context.Context) error {
	// CPU detection
	if err := p.detectDarwinCPU(); err != nil {
		return err
	}

	// Memory detection
	if err := p.detectDarwinMemory(); err != nil {
		return err
	}

	// GPU detection (Apple Silicon or AMD)
	p.detectDarwinGPU()

	// Disk detection
	if err := p.detectDarwinDisk(); err != nil {
		return err
	}

	// Network detection
	if err := p.detectDarwinNetwork(); err != nil {
		return err
	}

	// OS detection
	p.detectDarwinOS()

	return nil
}

// detectWindows detects hardware on Windows
func (p *HardwareProfile) detectWindows(ctx context.Context) error {
	// For Windows, use WMI and other Windows-specific APIs
	// This is a simplified version
	
	// CPU detection via wmic
	p.detectWindowsCPU()
	
	// Memory via wmic
	p.detectWindowsMemory()
	
	// OS info
	p.OS.Name = "Windows"
	p.OS.Arch = runtime.GOARCH
	
	return nil
}

// detectLinuxCPU detects CPU info on Linux
func (p *HardwareProfile) detectLinuxCPU() error {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return err
	}

	content := string(data)
	
	// Extract model name
	if match := regexp.MustCompile(`model name\s*:\s*(.+)`).FindStringSubmatch(content); len(match) > 1 {
		p.CPU.ModelName = strings.TrimSpace(match[1])
	}
	
	// Count cores (physical)
	if match := regexp.MustCompile(`cpu cores\s*:\s*(\d+)`).FindStringSubmatch(content); len(match) > 1 {
		p.CPU.Cores, _ = strconv.Atoi(match[1])
	}
	
	// Count threads (processors)
	p.CPU.Threads = len(regexp.MustCompile(`processor\s*:`).FindAllString(content, -1))
	if p.CPU.Cores == 0 && p.CPU.Threads > 0 {
		p.CPU.Cores = p.CPU.Threads
	}
	
	// Check for AVX flags
	if match := regexp.MustCompile(`flags\s*:\s*(.+)`).FindStringSubmatch(content); len(match) > 1 {
		flags := match[1]
		p.CPU.HasAVX2 = strings.Contains(flags, "avx2")
		p.CPU.HasAVX512 = strings.Contains(flags, "avx512")
	}
	
	p.CPU.Score = calculateCPUScore(p.CPU.Cores, p.CPU.HasAVX2, p.CPU.HasAVX512)
	return nil
}

// detectLinuxMemory detects memory info on Linux
func (p *HardwareProfile) detectLinuxMemory() error {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return err
	}

	content := string(data)
	
	var totalKB, availableKB int64
	
	if match := regexp.MustCompile(`MemTotal:\s*(\d+)\s*kB`).FindStringSubmatch(content); len(match) > 1 {
		totalKB, _ = strconv.ParseInt(match[1], 10, 64)
	}
	
	if match := regexp.MustCompile(`MemAvailable:\s*(\d+)\s*kB`).FindStringSubmatch(content); len(match) > 1 {
		availableKB, _ = strconv.ParseInt(match[1], 10, 64)
	}
	
	p.Memory.TotalGB = float64(totalKB) / (1024 * 1024)
	p.Memory.AvailableGB = float64(availableKB) / (1024 * 1024)
	p.Memory.Score = calculateMemoryScore(p.Memory.AvailableGB)
	
	return nil
}

// detectLinuxGPU detects GPU info on Linux
func (p *HardwareProfile) detectLinuxGPU() {
	// Try nvidia-smi
	cmd := exec.Command("nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader,nounits")
	output, err := cmd.Output()
	if err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(output)))
		for scanner.Scan() {
			parts := strings.Split(scanner.Text(), ", ")
			if len(parts) >= 2 {
				memMB, _ := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				p.GPUs = append(p.GPUs, GPUInfo{
					Model:  strings.TrimSpace(parts[0]),
					Vendor: "nvidia",
					VRAMGB: memMB / 1024,
					Score:  calculateGPUScore(memMB / 1024),
				})
			}
		}
	}
}

// detectLinuxDisk detects disk info on Linux
func (p *HardwareProfile) detectLinuxDisk() error {
	cmd := exec.Command("df", "-BG", ".")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.Contains(fields[0], "/") {
			// Parse total (e.g., "50G")
			totalStr := strings.TrimSuffix(fields[1], "G")
			p.Disk.TotalGB, _ = strconv.ParseFloat(totalStr, 64)
			
			// Parse available
			availStr := strings.TrimSuffix(fields[3], "G")
			p.Disk.FreeGB, _ = strconv.ParseFloat(availStr, 64)
			
			// Detect disk type
			p.Disk.Type = "unknown"
			if fields[0] == "/dev/nvme" || strings.HasPrefix(fields[0], "/dev/nvme") {
				p.Disk.Type = "nvme"
			} else if strings.HasPrefix(fields[0], "/dev/sd") {
				// Check if rotational
				rotationalPath := fmt.Sprintf("/sys/block/%s/queue/rotational", filepath.Base(fields[0]))
				if data, err := os.ReadFile(rotationalPath); err == nil {
					if strings.TrimSpace(string(data)) == "0" {
						p.Disk.Type = "ssd"
					} else {
						p.Disk.Type = "hdd"
					}
				}
			}
			break
		}
	}
	
	p.Disk.Score = calculateDiskScore(p.Disk.Type)
	return nil
}

// detectLinuxNetwork detects network info on Linux
func (p *HardwareProfile) detectLinuxNetwork() error {
	p.Network.Hostname, _ = os.Hostname()
	
	// Check interfaces
	data, err := os.ReadFile("/proc/net/dev")
	if err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if idx := strings.Index(line, ":"); idx != -1 {
				iface := strings.TrimSpace(line[:idx])
				if iface != "lo" {
					if strings.HasPrefix(iface, "eth") || strings.HasPrefix(iface, "en") {
						p.Network.HasEthernet = true
					}
					if strings.HasPrefix(iface, "wlan") || strings.HasPrefix(iface, "wl") {
						p.Network.HasWiFi = true
					}
				}
			}
		}
	}
	
	return err
}

// detectLinuxOS detects OS info on Linux
func (p *HardwareProfile) detectLinuxOS() {
	p.OS.Name = "Linux"
	p.OS.Arch = runtime.GOARCH
	
	// Try to get version from /etc/os-release
	data, err := os.ReadFile("/etc/os-release")
	if err == nil {
		content := string(data)
		if match := regexp.MustCompile(`PRETTY_NAME="([^"]+)"`).FindStringSubmatch(content); len(match) > 1 {
			p.OS.Name = match[1]
		}
	}
}

// detectDarwinCPU detects CPU on macOS
func (p *HardwareProfile) detectDarwinCPU() error {
	// Get CPU info
	cmd := exec.Command("sysctl", "-n", "machdep.cpu.brand_string")
	output, err := cmd.Output()
	if err == nil {
		p.CPU.ModelName = strings.TrimSpace(string(output))
	}
	
	// Get core count
	cmd = exec.Command("sysctl", "-n", "hw.physicalcpu")
	output, err = cmd.Output()
	if err == nil {
		p.CPU.Cores, _ = strconv.Atoi(strings.TrimSpace(string(output)))
	}
	
	// Get thread count
	cmd = exec.Command("sysctl", "-n", "hw.logicalcpu")
	output, err = cmd.Output()
	if err == nil {
		p.CPU.Threads, _ = strconv.Atoi(strings.TrimSpace(string(output)))
	}
	
	// Check for AVX
	cmd = exec.Command("sysctl", "-n", "machdep.cpu.features")
	output, err = cmd.Output()
	if err == nil {
		features := string(output)
		p.CPU.HasAVX2 = strings.Contains(features, "AVX2")
		p.CPU.HasAVX512 = strings.Contains(features, "AVX512")
	}
	
	p.CPU.Score = calculateCPUScore(p.CPU.Cores, p.CPU.HasAVX2, p.CPU.HasAVX512)
	return nil
}

// detectDarwinMemory detects memory on macOS
func (p *HardwareProfile) detectDarwinMemory() error {
	cmd := exec.Command("sysctl", "-n", "hw.memsize")
	output, err := cmd.Output()
	if err != nil {
		return err
	}
	
	memBytes, _ := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	p.Memory.TotalGB = float64(memBytes) / (1024 * 1024 * 1024)
	
	// Get available memory
	cmd = exec.Command("vm_stat")
	output, err = cmd.Output()
	if err == nil {
		// Parse vm_stat output for free pages
		content := string(output)
		if match := regexp.MustCompile(`Pages free:\s*(\d+)`).FindStringSubmatch(content); len(match) > 1 {
			freePages, _ := strconv.ParseInt(match[1], 10, 64)
			// Pages are 4KB on macOS
			p.Memory.AvailableGB = float64(freePages * 4096) / (1024 * 1024 * 1024)
		}
	}
	
	if p.Memory.AvailableGB == 0 {
		p.Memory.AvailableGB = p.Memory.TotalGB * 0.3 // Estimate
	}
	
	p.Memory.Score = calculateMemoryScore(p.Memory.AvailableGB)
	return nil
}

// detectDarwinGPU detects GPU on macOS
func (p *HardwareProfile) detectDarwinGPU() {
	cmd := exec.Command("system_profiler", "SPDisplaysDataType")
	output, err := cmd.Output()
	if err != nil {
		return
	}
	
	content := string(output)
	if strings.Contains(content, "Apple M") {
		// Apple Silicon
		var vramGB float64 = 8 // Default
		
		// Try to extract memory from output
		if match := regexp.MustCompile(`\((\d+) GB\)`).FindStringSubmatch(content); len(match) > 1 {
			vramGB, _ = strconv.ParseFloat(match[1], 64)
		}
		
		p.GPUs = append(p.GPUs, GPUInfo{
			Model:  "Apple Silicon GPU",
			Vendor: "apple",
			VRAMGB: vramGB,
			Score:  calculateGPUScore(vramGB),
		})
	}
}

// detectDarwinDisk detects disk on macOS
func (p *HardwareProfile) detectDarwinDisk() error {
	cmd := exec.Command("df", "-g", "/")
	output, err := cmd.Output()
	if err != nil {
		return err
	}
	
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.HasPrefix(fields[0], "/dev/") {
			p.Disk.TotalGB, _ = strconv.ParseFloat(fields[1], 64)
			p.Disk.FreeGB, _ = strconv.ParseFloat(fields[3], 64)
			
			// Detect SSD vs HDD
			// Check if APFS (usually SSD) or HFS
			cmd2 := exec.Command("diskutil", "info", fields[0])
			output2, _ := cmd2.Output()
			if strings.Contains(string(output2), "Solid State") {
				p.Disk.Type = "ssd"
			}
			break
		}
	}
	
	p.Disk.Score = calculateDiskScore(p.Disk.Type)
	return nil
}

// detectDarwinNetwork detects network on macOS
func (p *HardwareProfile) detectDarwinNetwork() error {
	p.Network.Hostname, _ = os.Hostname()
	
	// Check for WiFi
	cmd := exec.Command("ifconfig", "en0")
	output, _ := cmd.Output()
	if len(output) > 0 {
		p.Network.HasWiFi = true
	}
	
	// Check for Ethernet
	cmd = exec.Command("ifconfig", "en1")
	output, _ = cmd.Output()
	if len(output) > 0 {
		p.Network.HasEthernet = true
	}
	
	return nil
}

// detectDarwinOS detects OS on macOS
func (p *HardwareProfile) detectDarwinOS() {
	p.OS.Name = "macOS"
	p.OS.Arch = runtime.GOARCH
	
	// Get version
	cmd := exec.Command("sw_vers", "-productVersion")
	output, err := cmd.Output()
	if err == nil {
		p.OS.Version = strings.TrimSpace(string(output))
	}
}

// detectWindowsCPU detects CPU on Windows
func (p *HardwareProfile) detectWindowsCPU() {
	p.CPU.ModelName = "Unknown"
	p.CPU.Cores = 4 // Default
	p.CPU.Threads = 4
	
	// Try wmic
	cmd := exec.Command("wmic", "cpu", "get", "Name,NumberOfCores,NumberOfLogicalProcessors", "/format:csv")
	output, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			fields := strings.Split(line, ",")
			if len(fields) >= 4 {
				p.CPU.ModelName = strings.TrimSpace(fields[2])
				p.CPU.Cores, _ = strconv.Atoi(strings.TrimSpace(fields[3]))
				if len(fields) > 4 {
					p.CPU.Threads, _ = strconv.Atoi(strings.TrimSpace(fields[4]))
				}
				break
			}
		}
	}
	
	p.CPU.Score = calculateCPUScore(p.CPU.Cores, false, false)
}

// detectWindowsMemory detects memory on Windows
func (p *HardwareProfile) detectWindowsMemory() {
	cmd := exec.Command("wmic", "ComputerSystem", "get", "TotalPhysicalMemory", "/format:value")
	output, err := cmd.Output()
	if err == nil {
		content := string(output)
		if match := regexp.MustCompile(`TotalPhysicalMemory=(\d+)`).FindStringSubmatch(content); len(match) > 1 {
			memBytes, _ := strconv.ParseInt(match[1], 10, 64)
			p.Memory.TotalGB = float64(memBytes) / (1024 * 1024 * 1024)
			p.Memory.AvailableGB = p.Memory.TotalGB * 0.3 // Estimate
		}
	}
	
	p.Memory.Score = calculateMemoryScore(p.Memory.AvailableGB)
}

// calculateScore computes the total hardware score
func (p *HardwareProfile) calculateScore() {
	p.Score = p.CPU.Score + p.Memory.Score + p.Disk.Score
	
	// Add GPU scores (use best GPU)
	bestGPUScore := 0
	for _, gpu := range p.GPUs {
		if gpu.Score > bestGPUScore {
			bestGPUScore = gpu.Score
		}
	}
	p.Score += bestGPUScore
}

// determineTier sets the recommended tier
func (p *HardwareProfile) determineTier() {
	switch {
	case p.Score >= 100:
		p.Tier = "full"
	case p.Score >= 50:
		p.Tier = "middle"
	default:
		p.Tier = "lightweight"
	}
}

// calculateCPUScore computes CPU score
func calculateCPUScore(cores int, hasAVX2, hasAVX512 bool) int {
	score := 0
	if hasAVX2 {
		score += 10
	}
	if hasAVX512 {
		score += 15
	}
	score += cores * 2
	
	if score > 30 {
		return 30
	}
	return score
}

// calculateMemoryScore computes memory score
func calculateMemoryScore(availableGB float64) int {
	switch {
	case availableGB >= 16:
		return 40
	case availableGB >= 8:
		return 25
	case availableGB >= 4:
		return 15
	default:
		return int(availableGB * 3)
	}
}

// calculateGPUScore computes GPU score
func calculateGPUScore(vramGB float64) int {
	switch {
	case vramGB >= 12:
		return 50
	case vramGB >= 8:
		return 35
	case vramGB >= 4:
		return 20
	case vramGB >= 2:
		return 10
	default:
		return 0
	}
}

// calculateDiskScore computes disk score
func calculateDiskScore(diskType string) int {
	switch diskType {
	case "nvme":
		return 15
	case "ssd":
		return 10
	case "hdd":
		return 3
	default:
		return 5
	}
}
