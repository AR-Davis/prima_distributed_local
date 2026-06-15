// Package cmd contains command implementations
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/AR-Davis/prima_distributed_local/pkg/config"
	"github.com/AR-Davis/prima_distributed_local/pkg/detect"
	"github.com/AR-Davis/prima_distributed_local/pkg/service"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// runTUI implements the TUI command
func runTUI(cmd *cobra.Command, args []string) error {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}

// App states
type state int

const (
	stateMenu state = iota
	stateDetecting
	stateDetectDone
	stateInstalling
	stateInstallDone
	stateRunning
	stateConfiguring
	statePairing
	stateStatus
	stateExiting
)

// Menu items
type menuItem struct {
	title       string
	description string
	action      string
}

var menuItems = []menuItem{
	{"🔍 Detect Hardware", "Analyze this computer's capabilities", "detect"},
	{"📦 Install Service", "Install Prima as a background service", "install"},
	{"⚙️  Configure", "Edit cluster configuration", "config"},
	{"🔗 Pair Device", "Pair with head node via QR code", "pair"},
	{"▶️  Run Once", "Run worker in foreground (test)", "run"},
	{"📊 Status", "Check service status", "status"},
	{"🌐 Web Dashboard", "Open web dashboard", "web"},
	{"❌ Exit", "Quit without changes", "exit"},
}

// Model for Bubble Tea
type model struct {
	state       state
	cursor      int
	selected    int
	spinner     spinner.Model
	progress    progress.Model
	hwProfile   *detect.HardwareProfile
	installErr  error
	status      string
	width       int
	height      int
}

// Styles
var (
	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7D56F4")).
		MarginLeft(2)

	itemStyle = lipgloss.NewStyle().
		PaddingLeft(4)

	selectedItemStyle = lipgloss.NewStyle().
		PaddingLeft(2).
		Foreground(lipgloss.Color("#7D56F4")).
		Bold(true)

	descriptionStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666")).
		PaddingLeft(6)

	boxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Margin(1)

	successStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00FF00"))

	errorStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF0000"))
)

func initialModel() model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))

	return model{
		state:    stateMenu,
		cursor:   0,
		selected: -1,
		spinner:  s,
		progress: progress.New(progress.WithDefaultGradient()),
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.progress.Width = msg.Width - 10
		return m, nil

	case tea.KeyMsg:
		switch m.state {
		case stateMenu:
			return m.handleMenuKeys(msg)
		case stateDetectDone, stateInstallDone, stateStatus:
			if msg.String() == "enter" || msg.String() == " " || msg.String() == "q" {
				return m, func() tea.Msg {
					return returnToMenuMsg{}
				}
			}
		case stateDetecting, stateInstalling:
			if msg.String() == "q" || msg.String() == "esc" {
				return m, tea.Quit
			}
		default:
			if msg.String() == "q" || msg.String() == "esc" {
				return m, tea.Quit
			}
		}

	case detectionCompleteMsg:
		m.state = stateDetectDone
		m.hwProfile = msg.profile
		return m, nil

	case installCompleteMsg:
		m.state = stateInstallDone
		m.status = msg.status
		m.installErr = msg.err
		return m, nil

	case returnToMenuMsg:
		m.state = stateMenu
		m.selected = -1
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd
	}

	return m, nil
}

func (m model) handleMenuKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		m.state = stateExiting
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if m.cursor < len(menuItems)-1 {
			m.cursor++
		}

	case "enter", " ":
		m.selected = m.cursor
		return m.handleMenuSelection()
	}

	return m, nil
}

func (m model) handleMenuSelection() (tea.Model, tea.Cmd) {
	item := menuItems[m.selected]

	switch item.action {
	case "detect":
		m.state = stateDetecting
		return m, tea.Batch(
			m.spinner.Tick,
			func() tea.Msg {
				return runDetection()
			},
		)

	case "install":
		m.state = stateInstalling
		return m, tea.Batch(
			m.spinner.Tick,
			func() tea.Msg {
				return runInstall()
			},
		)

	case "config":
		// Open config in editor
		return m, tea.Batch(
			tea.ExecProcess(exec.Command(getEditor(), config.ConfigPath()), func(err error) tea.Msg {
				return returnToMenuMsg{}
			}),
		)

	case "run":
		m.state = stateRunning
		return m, func() tea.Msg {
			runWorker()
			return returnToMenuMsg{}
		}

	case "status":
		m.state = stateStatus
		return m, func() tea.Msg {
			return checkStatus()
		}

	case "pair":
		m.state = statePairing
		return m, func() tea.Msg {
			// TODO: Implement pairing
			return returnToMenuMsg{}
		}

	case "web":
		// Start web in background
		go func() {
			fmt.Println("Starting web dashboard on http://localhost:8080")
			time.Sleep(2 * time.Second)
		}()
		return m, func() tea.Msg {
			return returnToMenuMsg{}
		}

	case "exit":
		m.state = stateExiting
		return m, tea.Quit
	}

	return m, nil
}

func (m model) View() string {
	switch m.state {
	case stateMenu:
		return m.viewMenu()
	case stateDetecting:
		return m.viewDetecting()
	case stateDetectDone:
		return m.viewDetectDone()
	case stateInstalling:
		return m.viewInstalling()
	case stateInstallDone:
		return m.viewInstallDone()
	case stateRunning:
		return m.viewRunning()
	case stateStatus:
		return m.viewStatus()
	default:
		return "Unknown state"
	}
}

func (m model) viewMenu() string {
	s := "\n"
	s += titleStyle.Render("🔌 Prima Distributed Local") + "\n"
	s += itemStyle.Render("Portable LLM Cluster Node Setup") + "\n\n"

	for i, item := range menuItems {
		if m.cursor == i {
			s += selectedItemStyle.Render("▶ " + item.title) + "\n"
		} else {
			s += itemStyle.Render(item.title) + "\n"
		}
		s += descriptionStyle.Render(item.description) + "\n"
	}

	s += "\n" + descriptionStyle.Render("Use ↑/↓ to navigate, Enter to select, Q to quit") + "\n"

	return s
}

func (m model) viewDetecting() string {
	s := "\n"
	s += titleStyle.Render("🔍 Detecting Hardware") + "\n\n"
	s += m.spinner.View() + " Analyzing system capabilities...\n\n"
	s += descriptionStyle.Render("This may take a few seconds") + "\n"
	s += descriptionStyle.Render("Press Q to cancel") + "\n"
	return s
}

func (m model) viewDetectDone() string {
	if m.hwProfile == nil {
		return errorStyle.Render("Detection failed") + "\nPress Enter to return..."
	}

	s := "\n"
	s += titleStyle.Render("📊 Hardware Profile") + "\n\n"

	// CPU
	s += fmt.Sprintf("🖥️  CPU: %s\n", m.hwProfile.CPU.ModelName)
	s += fmt.Sprintf("   Cores: %d | Threads: %d | AVX2: %v\n",
		m.hwProfile.CPU.Cores, m.hwProfile.CPU.Threads, m.hwProfile.CPU.HasAVX2)
	s += fmt.Sprintf("   Score: %d/30\n\n", m.hwProfile.CPU.Score)

	// Memory
	s += fmt.Sprintf("💾 Memory: %.1f GB total, %.1f GB available\n",
		m.hwProfile.Memory.TotalGB, m.hwProfile.Memory.AvailableGB)
	s += fmt.Sprintf("   Score: %d/40\n\n", m.hwProfile.Memory.Score)

	// GPU
	if len(m.hwProfile.GPUs) > 0 {
		s += "🎮 GPUs:\n"
		for _, gpu := range m.hwProfile.GPUs {
			s += fmt.Sprintf("   %s: %.1f GB VRAM | Score: %d/50\n",
				gpu.Model, gpu.VRAMGB, gpu.Score)
		}
		s += "\n"
	}

	// Summary
	tierColor := "#FFFF00"
	switch m.hwProfile.Tier {
	case "full":
		tierColor = "#00FF00"
	case "lightweight":
		tierColor = "#00FFFF"
	}

	tierStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tierColor)).Bold(true)
	s += fmt.Sprintf("Total Score: %d/135\n", m.hwProfile.Score)
	s += fmt.Sprintf("Recommended Tier: %s\n\n", tierStyle.Render(m.hwProfile.Tier))

	s += descriptionStyle.Render("Press Enter to return to menu") + "\n"

	return boxStyle.Render(s)
}

func (m model) viewInstalling() string {
	s := "\n"
	s += titleStyle.Render("📦 Installing Service") + "\n\n"
	s += m.spinner.View() + " Installing Prima system service...\n\n"
	s += descriptionStyle.Render("This may require administrator privileges") + "\n"
	s += descriptionStyle.Render("Press Q to cancel") + "\n"
	return s
}

func (m model) viewInstallDone() string {
	s := "\n"
	s += titleStyle.Render("📦 Installation Complete") + "\n\n"

	if m.installErr != nil {
		s += errorStyle.Render(fmt.Sprintf("Error: %v", m.installErr)) + "\n\n"
	} else {
		s += successStyle.Render("✅ Service installed successfully!") + "\n\n"
		s += m.status + "\n\n"
	}

	s += descriptionStyle.Render("Press Enter to return to menu") + "\n"
	return s
}

func (m model) viewRunning() string {
	s := "\n"
	s += titleStyle.Render("▶️  Running Worker") + "\n\n"
	s += "Worker is running in foreground mode.\n"
	s += "Press Ctrl+C to stop.\n\n"
	return s
}

func (m model) viewStatus() string {
	s := "\n"
	s += titleStyle.Render("📊 Service Status") + "\n\n"
	s += m.status + "\n\n"
	s += descriptionStyle.Render("Press Enter to return") + "\n"
	return s
}

// Message types
type detectionCompleteMsg struct {
	profile *detect.HardwareProfile
}

type installCompleteMsg struct {
	status string
	err    error
}
type returnToMenuMsg struct{}

// Action functions
func runDetection() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	profile, err := detect.Detect(ctx, "")
	if err != nil {
		return detectionCompleteMsg{profile: nil}
	}

	return detectionCompleteMsg{profile: profile}
}

func runInstall() tea.Msg {
	// Load or create config
	cfgPath := config.ConfigPath()
	var cfg *config.Config
	var err error

	if config.Exists(cfgPath) {
		cfg, err = config.Load(cfgPath)
		if err != nil {
			return installCompleteMsg{err: err}
		}
	} else {
		cfg = config.DefaultConfig()

		// Auto-detect hardware for tier
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		profile, err := detect.Detect(ctx, "")
		if err == nil && profile != nil {
			cfg.Node.Tier = config.NodeTier(profile.Tier)
		}

		if err := cfg.Save(cfgPath); err != nil {
			return installCompleteMsg{err: err}
		}
	}

	// Create and install service
	svc, err := service.New(cfg)
	if err != nil {
		return installCompleteMsg{err: err}
	}

	if err := svc.Install(); err != nil {
		return installCompleteMsg{err: err}
	}

	status, _ := svc.Status()
	return installCompleteMsg{
		status: fmt.Sprintf("Service status: %s", status),
	}
}

func checkStatus() tea.Msg {
	cfgPath := config.ConfigPath()
	if !config.Exists(cfgPath) {
		return statusMsg{status: "Not installed - no configuration found"}
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return statusMsg{status: fmt.Sprintf("Error loading config: %v", err)}
	}

	svc, err := service.New(cfg)
	if err != nil {
		return statusMsg{status: fmt.Sprintf("Error: %v", err)}
	}

	status, err := svc.Status()
	if err != nil {
		return statusMsg{status: fmt.Sprintf("Error checking status: %v", err)}
	}

	return statusMsg{status: fmt.Sprintf("Service status: %s", status)}
}

type statusMsg struct {
	status string
}

func runWorker() {
	// This is a simplified version - actual implementation would block
	fmt.Println("Running worker...")
}

func getEditor() string {
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	return "nano"
}