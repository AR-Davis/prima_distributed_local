// Package cmd contains command implementations
package cmd

import (
	"fmt"

	"github.com/charmbracelet/bubbletea"
)

// runTUI implements the TUI command
func runTUI(cmd *cobra.Command, args []string) error {
	fmt.Println("🖥️  Starting interactive terminal UI...")
	fmt.Println()
	
	// TODO: Implement Bubble Tea TUI
	fmt.Println("⚠️  TUI not yet implemented")
	fmt.Println("   Use 'prima-installer detect' or 'prima-installer install' for now")
	
	return nil
}

// TUI model placeholder
type model struct {
	choices  []string
	cursor   int
	selected map[int]struct{}
}

func initialModel() model {
	return model{
		choices:  []string{"Detect hardware", "Install service", "Configure cluster", "Exit"},
		selected: make(map[int]struct{}),
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case "enter", " ":
			_, ok := m.selected[m.cursor]
			if ok {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = struct{}{}
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	// Simple placeholder view
	return "TUI Placeholder\n"
}