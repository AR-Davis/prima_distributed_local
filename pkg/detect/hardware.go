// Package detect handles hardware detection and scoring
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

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
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
	ModelName   string  `json:"model_name"`
	Cores       int     `json:"cores"`
	Threads     int     `json:"threads"`
	BaseFreqMHz float64 `json:"base_freq_mhz"`
	HasAVX2     bool    `json:"has_avx2"`
	HasAVX512   bool    `json:"has_avx512"`
	Score       int     `json:"score"`
}

// MemoryInfo contains memory specifications
type MemoryInfo struct {
	TotalGB     float64 `json:"total_gb"`
	AvailableGB float64 `json:"available_gb"`
	UsedPercent float64 `json:"used_percent"`
	Score       int     `json:"score"`
}

// GPUInfo contains GPU specifications
type GPUInfo struct {
	Model      string `json:"model"`
	Vendor     string `json:"vendor"` // "nvidia", "amd", "intel", "apple"
	VRAMGB     float64 `json:"vram_gb"`
	IsDiscrete bool   `json:"is_discrete"`
	Score      int    `json:"score"`
}

// DiskInfo contains disk specifications
type DiskInfo struct {
	Type         string  `json:"type"` // "nvme", "ssd", "hdd", "unknown"
	TotalGB      float64 `json:"total_gb"`
	FreeGB       float64 `json:"free_gb"`
	ReadSpeedMBs float64 `json:"read_speed_mbs"`
	Score        int     `json:"score"`
}

// NetworkInfo contains network specifications
type NetworkInfo struct {
	Hostname      string   `json:"hostname"`
	Interfaces    []string `json:"interfaces"`
	HasEthernet   bool     `json:"has_ethernet"`
	HasWiFi       bool     `json:"has_wifi"`
	LatencyToHead float64  `json:"latency_to_head_ms,omitempty"`
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

	// Detect each component
	if err := profile.detectCPU(ctx); err != nil {
		return nil, fmt.Errorf("CPU detection failed: %w", err)
	}

	if err := profile.detectMemory(ctx); err != nil {
		return nil, fmt.Errorf("memory detection failed: %w", err)
	}

	if err := profile.detectGPUs(ctx); err != nil {
		// GPU detection is optional
		fmt.Printf("Warning: GPU detection failed: %v\n", err)
	}

	if err := profile.detectDisk(ctx); err != nil {
		return nil, fmt.Errorf("disk detection failed: %w", err)
	}

	if err := profile.detectNetwork(ctx, headNode); err != nil {
		// Network detection is optional
		fmt.Printf("Warning: network detection failed: %v\n", err)
	}

	if err := profile.detectOS(ctx); err != nil {
		return nil, fmt.Errorf("OS detection failed: %w", err)
	}

	// Calculate score and tier
	profile.calculateScore()
	profile.determineTier()

	return profile, nil
}

// detectCPU detects CPU information
func (p *HardwareProfile) detectCPU(ctx context.Context) error {
	info, err := cpu.InfoWithContext(ctx)
	if err != nil {
		return err
	}

	if len(info) == 0 {
		return fmt.Errorf("no CPU info available")
	}

	p.CPU.ModelName = info[0].ModelName
	p.CPU.Cores = int(info[0].Cores)

	// Count threads
	p.CPU.Threads = len(info)

	// Check for AVX2/AVX-512
	p.CPU.HasAVX2 = checkAVX2()
	p.CPU.HasAVX512 = checkAVX512()

	// Score calculation
	p.CPU.Score = calculateCPUScore(&p.CPU)

	return nil
}

// detectMemory detects memory information
func (p *HardwareProfile) detectMemory(ctx context.Context) error {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return err
	}

	p.Memory.TotalGB = float64(vm.Total) / (1024 * 1024 * 1024)
	p.Memory.AvailableGB = float64(vm.Available) / (1024 * 1024 * 1024)
	p.Memory.UsedPercent = vm.UsedPercent

	p.Memory.Score = calculateMemoryScore(&p.Memory)

	return nil
}

// detectGPUs detects GPU information
func (p *HardwareProfile) detectGPUs(ctx context.Context) error {
	switch runtime.GOOS {
	case "linux":
		return p.detectGPUsLinux(ctx)
	case "windows":
		return p.detectGPUsWindows(ctx)
	case "darwin":
		return p.detectGPUsDarwin(ctx)
	default:
		return fmt.Errorf("GPU detection not supported on %s", runtime.GOOS)
	}
}

// detectGPUsLinux detects GPUs on Linux
func (p *HardwareProfile) detectGPUsLinux(ctx context.Context) error {
	// Try nvidia-smi for NVIDIA GPUs
	if nvidiaGPUs, err := p.detectNVIDIA(ctx); err == nil {
		p.GPUs = append(p.GPUs, nvidiaGPUs...)
	}

	// Try lspci for other GPUs
	if otherGPUs, err := p.detectPCI(ctx); err == nil {
		p.GPUs = append(p.GPUs, otherGPUs...)
	}

	return nil
}

// detectNVIDIA detects NVIDIA GPUs using nvidia-smi
func (p *HardwareProfile) detectNVIDIA(ctx context.Context) ([]GPUInfo, error) {
	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=name,memory.total,driver_version",
		"--format=csv,noheader,nounits")

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var gpus []GPUInfo
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Split(line, ", ")
		if len(parts) >= 2 {
			memMB, _ := strconv.ParseFloat(parts[1], 64)
			gpu := GPUInfo{
				Model:      strings.TrimSpace(parts[0]),
				Vendor:     "nvidia",
				VRAMGB:     memMB / 1024,
				IsDiscrete: true,
				Score:      calculateGPUScore(memMB / 1024),
			}
			gpus = append(gpus, gpu)
		}
	}

	return gpus, nil
}

// detectPCI detects GPUs using lspci
func (p *HardwareProfile) detectPCI(ctx context.Context) ([]GPUInfo, error) {
	// This is a simplified detection - full implementation would parse lspci output
	cmd := exec.CommandContext(ctx, "lspci")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var gpus []GPUInfo
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "VGA") || strings.Contains(line, "3D controller") {
			gpu := GPUInfo{
				Model:  extractGPUModel(line),
				Vendor: detectGPUVendor(line),
			}
			gpus = append(gpus, gpu)
		}
	}

	return gpus, nil
}

// detectGPUsWindows detects GPUs on Windows
func (p *HardwareProfile) detectGPUsWindows(ctx context.Context) error {
	// Try nvidia-smi first
	if nvidiaGPUs, err := p.detectNVIDIA(ctx); err == nil {
		p.GPUs = append(p.GPUs, nvidiaGPUs...)
	}

	// TODO: Add WMI-based detection for AMD/Intel GPUs
	return nil
}

// detectGPUsDarwin detects GPUs on macOS
func (p *HardwareProfile) detectGPUsDarwin(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "system_profiler", "SPDisplaysDataType")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	// Parse system_profiler output for GPU info
	content := string(output)
	if strings.Contains(content, "Apple M") {
		// Apple Silicon - integrated GPU
		re := regexp.MustCompile(`Apple M[\dProMax]+`)
		matches := re.FindStringSubmatch(content)
		model := "Apple Silicon GPU"
		if len(matches) > 0 {
			model = matches[0] + " GPU"
		}

		// Try to get memory info
		var vramGB float64 = 8 // Default assumption

		gpu := GPUInfo{
			Model:      model,
			Vendor:     "apple",
			VRAMGB:     vramGB,
			IsDiscrete: false,
			Score:      calculateGPUScore(vramGB),
		}
		p.GPUs = append(p.GPUs, gpu)
	}

	return nil
}

// detectDisk detects disk information
func (p *HardwareProfile) detectDisk(ctx context.Context) error {
	// Get the disk where the executable or home directory is
	dir := DataDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		dir = os.Getenv("HOME")
		if dir == "" {
			dir = "."
		}
	}

	usage, err := disk.UsageWithContext(ctx, dir)
	if err != nil {
		return err
	}

	p.Disk.TotalGB = float64(usage.Total) / (1024 * 1024 * 1024)
	p.Disk.FreeGB = float64(usage.Free) / (1024 * 1024 * 1024)

	// Detect disk type and speed
	p.Disk.Type = detectDiskType(dir)
	p.Disk.ReadSpeedMBs = benchmarkDiskRead(ctx, dir)

	p.Disk.Score = calculateDiskScore(&p.Disk)

	return nil
}

// detectNetwork detects network information
func (p *HardwareProfile) detectNetwork(ctx context.Context, headNode string) error {
	hostname, _ := os.Hostname()
	p.Network.Hostname = hostname

	// Get network interfaces
	interfaces, err := net.InterfacesWithContext(ctx)
	if err != nil {
		return err
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 {
			p.Network.Interfaces = append(p.Network.Interfaces, iface.Name)

			// Check interface type
			if strings.HasPrefix(iface.Name, "eth") || strings.HasPrefix(iface.Name, "en") {
				p.Network.HasEthernet = true
			}
			if strings.HasPrefix(iface.Name, "wlan") || strings.HasPrefix(iface.Name, "wl") ||
				strings.HasPrefix(iface.Name, "Wi-Fi") {
				p.Network.HasWiFi = true
			}
		}
	}

	// Test latency to head node if provided
	if headNode != "" {
		p.Network.LatencyToHead = testLatency(ctx, headNode)
	}

	return nil
}

// detectOS detects operating system information
func (p *HardwareProfile) detectOS(ctx context.Context) error {
	info, err := host.InfoWithContext(ctx)
	if err != nil {
		return err
	}

	p.OS.Name = info.Platform
	p.OS.Version = info.PlatformVersion
	p.OS.Arch = info.KernelArch

	return nil
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

// determineTier sets the recommended tier based on score
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
func calculateCPUScore(cpu *CPUInfo) int {
	score := 0

	// AVX support
	if cpu.HasAVX2 {
		score += 10
	}
	if cpu.HasAVX512 {
		score += 15
	}

	// Core count
	score += cpu.Cores * 2

	return min(score, 30)
}

// calculateMemoryScore computes memory score
func calculateMemoryScore(mem *MemoryInfo) int {
	available := mem.AvailableGB

	switch {
	case available >= 16:
		return 40
	case available >= 8:
		return 25
	case available >= 4:
		return 15
	default:
		return max(0, int(available*3))
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
func calculateDiskScore(disk *DiskInfo) int {
	score := 0

	// Disk type
	switch disk.Type {
	case "nvme":
		score += 15
	case "ssd":
		score += 10
	case "hdd":
		score += 3
	}

	// Free space
	if disk.FreeGB < 10 {
		score -= 5 // Penalty for low space
	}

	return max(0, score)
}

// checkAVX2 checks if CPU supports AVX2
func checkAVX2() bool {
	// This is a simplified check - full implementation would use CPUID
	// For now, check /proc/cpuinfo on Linux
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile("/proc/cpuinfo")
		if err == nil {
			return strings.Contains(string(data), "avx2")
		}
	}
	return false
}

// checkAVX512 checks if CPU supports AVX-512
func checkAVX512() bool {
	// Simplified check
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile("/proc/cpuinfo")
		if err == nil {
			return strings.Contains(string(data), "avx512")
		}
	}
	return false
}

// detectDiskType attempts to determine disk type
func detectDiskType(dir string) string {
	// On Linux, check if mounted on NVMe
	if runtime.GOOS == "linux" {
		// Find mount point
		mnt := findMountPoint(dir)
		if strings.Contains(mnt, "nvme") {
			return "nvme"
		}
		if strings.Contains(mnt, "sd") || strings.Contains(mnt, "hd") {
			// Check rotational flag
			rotational := "/sys/block/" + filepath.Base(mnt) + "/queue/rotational"
			data, err := os.ReadFile(rotational)
			if err == nil && strings.TrimSpace(string(data)) == "0" {
				return "ssd"
			}
			return "hdd"
		}
	}

	// Try to infer from speed
	return "unknown"
}

// benchmarkDiskRead benchmarks disk read speed
func benchmarkDiskRead(ctx context.Context, dir string) float64 {
	// Create temporary file
	tmpfile := filepath.Join(dir, ".prima_disk_test")
	defer os.Remove(tmpfile)

	// Write test data (100MB)
	testSize := int64(100 * 1024 * 1024)
	data := make([]byte, 1024*1024) // 1MB chunks

	f, err := os.Create(tmpfile)
	if err != nil {
		return 0
	}

	written := int64(0)
	for written < testSize {
		n, _ := f.Write(data)
		written += int64(n)
	}
	f.Close()

	// Sync to ensure data is written
	exec.CommandContext(ctx, "sync").Run()

	// Read and measure
	f, err = os.Open(tmpfile)
	if err != nil {
		return 0
	}
	defer f.Close()

	buf := make([]byte, 1024*1024)
	start := time.Now()
	read := int64(0)

	for read < testSize && ctx.Err() == nil {
		n, _ := f.Read(buf)
		read += int64(n)
	}

	elapsed := time.Since(start).Seconds()
	if elapsed == 0 {
		return 0
	}

	return float64(read) / (1024 * 1024) / elapsed // MB/s
}

// findMountPoint finds the mount point for a directory
func findMountPoint(dir string) string {
	// Simplified implementation
	abs, _ := filepath.Abs(dir)
	for abs != "/" && abs != "" {
		if _, err := os.Stat(filepath.Join(abs, ".")); err == nil {
			return abs
		}
		abs = filepath.Dir(abs)
	}
	return "/"
}

// testLatency tests latency to a host
func testLatency(ctx context.Context, host string) float64 {
	// Extract hostname from host:port
	hostname := strings.Split(host, ":")[0]

	// Run ping
	cmd := exec.CommandContext(ctx, "ping", "-c", "3", "-W", "2", hostname)
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "ping", "-n", "3", "-w", "2000", hostname)
	}

	output, err := cmd.Output()
	if err != nil {
		return -1 // Unable to reach
	}

	// Parse average latency
	content := string(output)
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		// Look for "avg" in ping output
		if idx := strings.Index(content, "avg"); idx != -1 {
			// Extract number after avg
			re := regexp.MustCompile(`(\d+\.?\d*)`)
			matches := re.FindAllString(content[idx:idx+50], -1)
			if len(matches) >= 2 {
				ms, _ := strconv.ParseFloat(matches[1], 64)
				return ms
			}
		}
	}

	return -1
}

// extractGPUModel extracts GPU model from lspci output
func extractGPUModel(line string) string {
	// Remove standard prefixes
	line = strings.TrimSpace(line)
	if idx := strings.Index(line, ": "); idx != -1 {
		return line[idx+2:]
	}
	return line
}

// detectGPUVendor detects GPU vendor from string
func detectGPUVendor(line string) string {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "nvidia") {
		return "nvidia"
	}
	if strings.Contains(lower, "amd") || strings.Contains(lower, "ati") {
		return "amd"
	}
	if strings.Contains(lower, "intel") {
		return "intel"
	}
	return "unknown"
}

// Helper functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}