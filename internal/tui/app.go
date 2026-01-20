package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

func Run() error {
	model := NewModel()

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("failed to run TUI: %w", err)
	}

	return nil
}
