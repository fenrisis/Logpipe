package tui

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/fenrisis/logpipe/internal/client"
	"github.com/fenrisis/logpipe/internal/protocol"
)

type Focus int

const (
	FocusNamespaces Focus = iota
	FocusLogs
	FocusSearch
)

type Model struct {
	// Data
	namespaces []protocol.Namespace
	logs       []protocol.LogEntry
	stats      protocol.Stats

	// UI State
	focus               Focus
	selectedSource      logSource
	collapsedNamespaces map[string]bool
	expandedPods        map[logSource]bool
	logSelected         int
	logOffset           int
	streaming           bool
	searchActive        bool
	searchInput         textinput.Model
	searchQuery         string
	lastLogID           int64
	err                 error
	filterError         bool               // Show ERROR level
	filterWarn          bool               // Show WARN level
	filterInfo          bool               // Show INFO level
	filterDebug         bool               // Show DEBUG level
	showDetail          bool               // Show log detail modal
	detailLog           *protocol.LogEntry // Currently viewed log

	// Dimensions
	width  int
	height int

	// Components
	help   help.Model
	client *client.Client
}

// Messages
type tickMsg time.Time
type logsMsg struct {
	filter  protocol.Filter
	entries []protocol.LogEntry
}
type namespacesMsg []protocol.Namespace
type statsMsg protocol.Stats
type errMsg error

func NewModel() Model {
	ti := textinput.New()
	ti.Placeholder = "Search logs..."
	ti.CharLimit = 100

	return Model{
		focus:       FocusNamespaces,
		help:        help.New(),
		searchInput: ti,
		streaming:   true, // Start with streaming enabled
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.connectCmd(),
		tea.SetWindowTitle("Logpipe"),
	)
}

func (m *Model) connectCmd() tea.Cmd {
	return func() tea.Msg {
		c, err := client.New()
		if err != nil {
			return errMsg(err)
		}
		return connectedMsg{client: c}
	}
}

type connectedMsg struct {
	client *client.Client
}

func (m Model) fetchAllCmd() tea.Cmd {
	return tea.Batch(
		m.fetchNamespacesCmd(),
		m.fetchLogsCmd(),
		m.fetchStatsCmd(),
	)
}

func (m Model) fetchNamespacesCmd() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		ns, err := m.client.GetNamespaces()
		if err != nil {
			return errMsg(err)
		}
		return namespacesMsg(ns)
	}
}

func (m Model) fetchStatsCmd() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		stats, err := m.client.GetStats()
		if err != nil {
			return errMsg(err)
		}
		return statsMsg(stats)
	}
}

func (m Model) buildFilter() protocol.Filter {
	filter := protocol.Filter{
		Limit: 100,
	}

	filter.Namespace = m.selectedSource.namespace
	filter.Pod = m.selectedSource.pod
	filter.Container = m.selectedSource.container
	filter.Service = m.selectedSource.service

	if m.searchQuery != "" {
		filter.Search = m.searchQuery
	}

	// Build level filter if any level is selected
	if m.filterError || m.filterWarn || m.filterInfo || m.filterDebug {
		var levels []protocol.LogLevel
		if m.filterError {
			levels = append(levels, protocol.LevelError)
		}
		if m.filterWarn {
			levels = append(levels, protocol.LevelWarn)
		}
		if m.filterInfo {
			levels = append(levels, protocol.LevelInfo)
		}
		if m.filterDebug {
			levels = append(levels, protocol.LevelDebug)
		}
		filter.Levels = levels
	}

	return filter
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = msg.Width

	case tea.KeyMsg:
		// Handle detail view mode
		if m.showDetail {
			switch msg.String() {
			case "esc", "enter", "q":
				m.showDetail = false
				m.detailLog = nil
				return m, nil
			}
			return m, nil
		}

		// Handle search input mode
		if m.searchActive {
			switch msg.String() {
			case "enter":
				m.searchQuery = m.searchInput.Value()
				m.searchActive = false
				m.focus = FocusLogs
				return m, m.fetchLogsCmd()
			case "esc":
				m.searchActive = false
				m.searchInput.SetValue(m.searchQuery)
				return m, nil
			default:
				var cmd tea.Cmd
				m.searchInput, cmd = m.searchInput.Update(msg)
				return m, cmd
			}
		}

		// Normal key handling
		switch {
		case msg.String() == "q" || msg.String() == "ctrl+c":
			if m.client != nil {
				m.client.Close()
			}
			return m, tea.Quit

		case msg.String() == "tab":
			if m.focus == FocusNamespaces {
				m.focus = FocusLogs
			} else {
				m.focus = FocusNamespaces
			}

		case msg.String() == "/":
			m.searchActive = true
			m.focus = FocusSearch
			m.searchInput.Focus()
			return m, textinput.Blink

		case msg.String() == "f":
			m.streaming = !m.streaming

		case msg.String() == "e":
			// Toggle ERROR filter
			m.filterError = !m.filterError
			m.logOffset = 0
			m.logSelected = 0
			return m, m.fetchLogsCmd()

		case msg.String() == "w":
			// Toggle WARN filter
			m.filterWarn = !m.filterWarn
			m.logOffset = 0
			m.logSelected = 0
			return m, m.fetchLogsCmd()

		case msg.String() == "i":
			// Toggle INFO filter
			m.filterInfo = !m.filterInfo
			m.logOffset = 0
			m.logSelected = 0
			return m, m.fetchLogsCmd()

		case msg.String() == "d":
			// Toggle DEBUG filter
			m.filterDebug = !m.filterDebug
			m.logOffset = 0
			m.logSelected = 0
			return m, m.fetchLogsCmd()

		case msg.String() == "c":
			// Clear all filters
			m.searchQuery = ""
			m.searchInput.SetValue("")
			m.filterError = false
			m.filterWarn = false
			m.filterInfo = false
			m.filterDebug = false
			m.logOffset = 0
			m.logSelected = 0
			return m, m.fetchLogsCmd()

		case msg.String() == "j" || msg.String() == "down":
			m.handleDown()
			if m.focus == FocusNamespaces {
				return m, m.fetchLogsCmd()
			}

		case msg.String() == "k" || msg.String() == "up":
			m.handleUp()
			if m.focus == FocusNamespaces {
				return m, m.fetchLogsCmd()
			}

		case msg.String() == "enter":
			if m.focus == FocusNamespaces {
				m.enterSource()
				return m, m.fetchLogsCmd()
			} else if m.focus == FocusLogs && len(m.logs) > 0 {
				// Show log detail
				if m.logSelected < len(m.logs) {
					m.detailLog = &m.logs[m.logSelected]
					m.showDetail = true
				}
			}

		case msg.String() == "right" || msg.String() == "l":
			if m.focus == FocusNamespaces {
				m.enterSource()
				return m, m.fetchLogsCmd()
			}

		case msg.String() == "left" || msg.String() == "h" || msg.String() == "esc":
			if m.focus == FocusNamespaces {
				m.leaveSource()
				return m, m.fetchLogsCmd()
			}

		case msg.String() == "pgdown" || msg.String() == "ctrl+d":
			if m.focus == FocusLogs {
				m.logOffset += 10
				if m.logOffset > len(m.logs)-1 {
					m.logOffset = max(0, len(m.logs)-1)
				}
			}

		case msg.String() == "pgup" || msg.String() == "ctrl+u":
			if m.focus == FocusLogs {
				m.logOffset -= 10
				if m.logOffset < 0 {
					m.logOffset = 0
				}
			}
		}

	case namespacesMsg:
		m.namespaces = msg
		if m.restoreSourceSelection() {
			return m, m.fetchLogsCmd()
		}

	case logsMsg:
		// A response for the previous source may arrive after navigation.
		if !reflect.DeepEqual(msg.filter, m.buildFilter()) {
			return m, nil
		}
		m.logs = msg.entries
		m.logSelected = min(m.logSelected, max(0, len(m.logs)-1))
		if len(m.logs) > 0 {
			m.lastLogID = m.logs[0].ID
		}

	case statsMsg:
		m.stats = protocol.Stats(msg)

	case connectedMsg:
		m.client = msg.client
		cmds = append(cmds, m.fetchAllCmd(), m.tickCmd())

	case errMsg:
		m.err = msg

	case tickMsg:
		if m.streaming {
			cmds = append(cmds, m.fetchLogsCmd(), m.fetchNamespacesCmd(), m.fetchStatsCmd())
		}
		cmds = append(cmds, m.tickCmd())
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) handleDown() {
	if m.focus == FocusNamespaces {
		m.moveSource(1)
	} else if m.focus == FocusLogs {
		// Move cursor down
		if m.logSelected < len(m.logs)-1 {
			m.logSelected++
		}
	}
}

func (m *Model) handleUp() {
	if m.focus == FocusNamespaces {
		m.moveSource(-1)
	} else if m.focus == FocusLogs {
		// Move cursor up
		if m.logSelected > 0 {
			m.logSelected--
		}
	}
}

func (m Model) fetchLogsCmd() tea.Cmd {
	filter := m.buildFilter()
	return func() tea.Msg {
		if m.client == nil || filter.Namespace == "" {
			return nil
		}
		logs, err := m.client.GetLogs(filter)
		if err != nil {
			return errMsg(err)
		}
		return logsMsg{filter: filter, entries: logs}
	}
}

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress q to quit.", m.err)
	}

	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	// Show detail modal if active
	if m.showDetail && m.detailLog != nil {
		return m.renderDetailView()
	}

	// Layout: sidebar (namespaces) | main (logs)
	sidebarWidth := min(48, max(25, m.width/3))
	mainWidth := m.width - sidebarWidth - 4 // two bordered panels

	sidebar := m.renderSidebar(sidebarWidth, m.height-4)
	main := m.renderLogs(mainWidth, m.height-4)

	// Apply borders based on focus
	if m.focus == FocusNamespaces {
		sidebar = focusedBorderStyle.Width(sidebarWidth).Height(m.height - 4).Render(sidebar)
		main = borderStyle.Width(mainWidth).Height(m.height - 4).Render(main)
	} else {
		sidebar = borderStyle.Width(sidebarWidth).Height(m.height - 4).Render(sidebar)
		main = focusedBorderStyle.Width(mainWidth).Height(m.height - 4).Render(main)
	}

	content := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main)

	// Status bar
	statusBar := m.renderStatusBar()

	// Help
	helpView := m.help.ShortHelpView(keys.ShortHelp())

	return lipgloss.JoinVertical(lipgloss.Left,
		content,
		statusBar,
		helpView,
	)
}

func (m Model) renderDetailView() string {
	log := m.detailLog

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("LOG DETAIL"))
	sb.WriteString("\n\n")

	// Metadata
	sb.WriteString(dimStyle.Render("Time:      "))
	sb.WriteString(log.Timestamp.Format("2006-01-02 15:04:05.000"))
	sb.WriteString("\n")

	sb.WriteString(dimStyle.Render("Namespace: "))
	sb.WriteString(log.Namespace)
	sb.WriteString("\n")

	sb.WriteString(dimStyle.Render("Service:   "))
	sb.WriteString(log.Service)
	sb.WriteString("\n")

	sb.WriteString(dimStyle.Render("Level:     "))
	levelStyle := getLevelStyle(string(log.Level))
	sb.WriteString(levelStyle.Render(string(log.Level)))
	sb.WriteString("\n")

	if log.TraceID != "" {
		sb.WriteString(dimStyle.Render("Trace ID:  "))
		sb.WriteString(log.TraceID)
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render("Message:"))
	sb.WriteString("\n")
	sb.WriteString("─────────────────────────────────────────\n")

	// Word wrap message
	message := log.Message
	maxWidth := m.width - 4
	for len(message) > maxWidth {
		sb.WriteString(message[:maxWidth])
		sb.WriteString("\n")
		message = message[maxWidth:]
	}
	sb.WriteString(message)
	sb.WriteString("\n")

	if len(log.Extra) > 0 {
		sb.WriteString("\n")
		sb.WriteString(dimStyle.Render("Extra:"))
		sb.WriteString("\n")
		sb.WriteString("─────────────────────────────────────────\n")
		sb.WriteString(string(log.Extra))
		sb.WriteString("\n")
	}

	sb.WriteString("\n\n")
	sb.WriteString(dimStyle.Render("Press ESC or Enter to close"))

	return focusedBorderStyle.Width(m.width - 4).Height(m.height - 2).Render(sb.String())
}

func (m Model) renderSidebar(width, height int) string {
	return m.renderSourceTree(width, height)
}

func (m Model) renderLogs(width, height int) string {
	var sb strings.Builder

	// Header
	header := titleStyle.Render("LOGS")
	if m.selectedSource.namespace != "" {
		header += " " + dimStyle.Render(m.selectedSource.label())
	}

	// Show active level filters
	if m.filterError || m.filterWarn || m.filterInfo || m.filterDebug {
		header += " "
		if m.filterError {
			header += errorStyle.Render("[E]")
		}
		if m.filterWarn {
			header += warnStyle.Render("[W]")
		}
		if m.filterInfo {
			header += infoStyle.Render("[I]")
		}
		if m.filterDebug {
			header += debugStyle.Render("[D]")
		}
	}

	if m.searchQuery != "" {
		header += dimStyle.Render(fmt.Sprintf(" (search: %s)", m.searchQuery))
	}
	if m.streaming {
		header += infoStyle.Render(" ● streaming")
	}
	sb.WriteString(ansi.Truncate(header, max(1, width-2), "…"))
	sb.WriteString("\n\n")

	// Search input
	if m.searchActive {
		sb.WriteString(m.searchInput.View())
		sb.WriteString("\n\n")
	}

	// Logs
	visibleLogs := height - 5
	if visibleLogs < 1 {
		visibleLogs = 1
	}

	// Calculate scroll offset to keep selected log visible
	start := 0
	if m.logSelected >= visibleLogs {
		start = m.logSelected - visibleLogs + 1
	}
	if m.logSelected < start {
		start = m.logSelected
	}

	end := start + visibleLogs
	if end > len(m.logs) {
		end = len(m.logs)
	}

	// Fixed width for source column (service name only, namespace shown in sidebar)
	const sourceWidth = 20

	for i := start; i < end; i++ {
		logEntry := m.logs[i]

		ts := logEntry.Timestamp.Format("15:04:05")
		level := fmt.Sprintf("%-5s", logEntry.Level)

		// Identify the actual container in multi-container pod views.
		source := logEntry.Service
		var metadata struct {
			Pod       string `json:"pod"`
			Container string `json:"container"`
		}
		if json.Unmarshal(logEntry.Extra, &metadata) == nil && metadata.Pod != "" {
			source = metadata.Pod
			if m.selectedSource.pod != "" && metadata.Container != "" {
				source = metadata.Container
			}
		}
		source = ansi.Truncate(source, sourceWidth, "…")
		source = fmt.Sprintf("%-*s", sourceWidth, source)

		levelStyle := getLevelStyle(string(logEntry.Level))

		// Selection indicator
		prefix := "  "
		if i == m.logSelected && m.focus == FocusLogs {
			prefix = "▸ "
		}

		line := fmt.Sprintf("%s%s %s %s %s",
			prefix,
			dimStyle.Render(ts),
			levelStyle.Render(level),
			dimStyle.Render(source),
			logEntry.Message,
		)

		// Truncate if too long
		line = ansi.Truncate(line, max(1, width-4), "…")

		// Highlight selected row
		if i == m.logSelected && m.focus == FocusLogs {
			line = selectedStyle.Render(line)
		}

		sb.WriteString(line)
		sb.WriteString("\n")
	}

	return sb.String()
}

func (m Model) renderStatusBar() string {
	var parts []string

	if m.selectedSource.namespace != "" {
		parts = append(parts, m.selectedSource.label())
	}

	parts = append(parts, fmt.Sprintf("%d logs", len(m.logs)))
	parts = append(parts, fmt.Sprintf("%d total", m.stats.TotalLogs))

	if m.stats.TodayErrors > 0 {
		parts = append(parts, errorStyle.Render(fmt.Sprintf("%d errors today", m.stats.TodayErrors)))
	}

	return statusBarStyle.Width(m.width).Render(ansi.Truncate(strings.Join(parts, " │ "), max(1, m.width-2), "…"))
}
