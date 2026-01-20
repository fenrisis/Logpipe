package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	primaryColor   = lipgloss.Color("75")  // Blue
	secondaryColor = lipgloss.Color("242") // Gray
	errorColor     = lipgloss.Color("196") // Red
	warnColor      = lipgloss.Color("214") // Orange
	infoColor      = lipgloss.Color("75")  // Blue
	debugColor     = lipgloss.Color("242") // Gray
	selectedBg     = lipgloss.Color("237") // Dark gray
	borderColor    = lipgloss.Color("240") // Border gray

	// Styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Background(selectedBg).
			Foreground(lipgloss.Color("255"))

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	dimStyle = lipgloss.NewStyle().
			Foreground(secondaryColor)

	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor)

	warnStyle = lipgloss.NewStyle().
			Foreground(warnColor)

	infoStyle = lipgloss.NewStyle().
			Foreground(infoColor)

	debugStyle = lipgloss.NewStyle().
			Foreground(debugColor)

	borderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor)

	focusedBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(primaryColor)

	helpStyle = lipgloss.NewStyle().
			Foreground(secondaryColor).
			Padding(0, 1)

	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("252")).
			Padding(0, 1)
)

func getLevelStyle(level string) lipgloss.Style {
	switch level {
	case "ERROR":
		return errorStyle
	case "WARN":
		return warnStyle
	case "INFO":
		return infoStyle
	case "DEBUG":
		return debugStyle
	default:
		return normalStyle
	}
}
