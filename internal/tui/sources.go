package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Keep selection by identity: discovery can insert or reorder pods every tick.
type logSource struct {
	namespace string
	pod       string
	container string
	service   string
}

func (s logSource) label() string {
	label := s.namespace
	if s.pod != "" {
		label += "/" + s.pod
		if s.container != "" {
			label += "/" + s.container
		} else {
			label += " (all containers)"
		}
	} else if s.service != "" {
		label += "/" + s.service
	} else {
		label += " (all pods)"
	}
	return label
}

type sourceRow struct {
	source logSource
	text   string
}

func (m Model) sourceRows() []sourceRow {
	var rows []sourceRow
	for _, ns := range m.namespaces {
		source := logSource{namespace: ns.Name}
		marker := "▾ "
		if m.collapsedNamespaces[ns.Name] {
			marker = "▸ "
		}
		label := marker + ns.Name + " (all pods)"
		if ns.ErrorCount > 0 {
			label += fmt.Sprintf(" (%d err)", ns.ErrorCount)
		}
		rows = append(rows, sourceRow{source, label})
		if m.collapsedNamespaces[ns.Name] {
			continue
		}
		for _, pod := range ns.Pods {
			source := logSource{namespace: ns.Name, pod: pod.Name}
			marker := "▸ "
			if m.expandedPods[source] {
				marker = "▾ "
			}
			rows = append(rows, sourceRow{source, "  " + marker + pod.Name})
			if m.expandedPods[source] {
				for _, container := range pod.Containers {
					child := source
					child.container = container
					rows = append(rows, sourceRow{child, "      " + container})
				}
			}
		}
		// Non-Kubernetes logs (and older producers) still support service selection.
		if len(ns.Pods) == 0 {
			for _, service := range ns.Services {
				rows = append(rows, sourceRow{
					logSource{namespace: ns.Name, service: service}, "    " + service,
				})
			}
		}
	}
	return rows
}

func (m *Model) selectSource(source logSource) {
	if m.selectedSource != source {
		m.selectedSource = source
		m.logs = nil
		m.logSelected = 0
		m.logOffset = 0
	}
}

func (m *Model) moveSource(delta int) {
	rows := m.sourceRows()
	for i, row := range rows {
		if row.source == m.selectedSource {
			next := max(0, min(len(rows)-1, i+delta))
			m.selectSource(rows[next].source)
			return
		}
	}
}

// Enter/right opens a node and selects its first child. Left/Esc returns to
// the parent, whose row explicitly represents all pods or all containers.
func (m *Model) enterSource() {
	source := m.selectedSource
	if source.namespace == "" || source.container != "" || source.service != "" {
		return
	}
	if source.pod == "" {
		if m.collapsedNamespaces == nil {
			m.collapsedNamespaces = make(map[string]bool)
		}
		m.collapsedNamespaces[source.namespace] = false
	} else {
		if m.expandedPods == nil {
			m.expandedPods = make(map[logSource]bool)
		}
		m.expandedPods[source] = true
	}
	rows := m.sourceRows()
	for i, row := range rows {
		if row.source == source && i+1 < len(rows) {
			child := rows[i+1].source
			if child.namespace == source.namespace && (source.pod == "" || child.pod == source.pod) {
				m.selectSource(child)
			}
			return
		}
	}
}

func (m *Model) leaveSource() {
	source := m.selectedSource
	if source.container != "" {
		source.container = ""
	} else if source.pod != "" || source.service != "" {
		delete(m.expandedPods, source)
		source.pod, source.service = "", ""
	} else if source.namespace != "" {
		if m.collapsedNamespaces == nil {
			m.collapsedNamespaces = make(map[string]bool)
		}
		m.collapsedNamespaces[source.namespace] = true
	}
	m.selectSource(source)
}

func (m *Model) restoreSourceSelection() bool {
	rows := m.sourceRows()
	for _, row := range rows {
		if row.source == m.selectedSource {
			return false
		}
	}
	// A removed source falls back to its closest remaining parent.
	fallback := logSource{}
	for _, row := range rows {
		if row.source.namespace == m.selectedSource.namespace && row.source.container == "" && row.source.service == "" {
			if row.source.pod == "" || row.source.pod == m.selectedSource.pod {
				fallback = row.source
			}
		}
	}
	if fallback.namespace == "" && len(rows) > 0 {
		fallback = rows[0].source
	}
	changed := fallback != m.selectedSource
	m.selectSource(fallback)
	return changed
}

func (m Model) renderSourceTree(width, height int) string {
	var sb strings.Builder
	sb.WriteString(ansi.Truncate(titleStyle.Render("NAMESPACE / POD / CONTAINER"), max(1, width), "…"))
	sb.WriteString("\n\n")
	rows := m.sourceRows()
	selected := 0
	for i, row := range rows {
		if row.source == m.selectedSource {
			selected = i
			break
		}
	}
	visible := max(1, height-5)
	start := max(0, selected-visible+1)
	end := min(len(rows), start+visible)
	for _, row := range rows[start:end] {
		prefix := "  "
		style := normalStyle
		if row.source == m.selectedSource {
			prefix = "› "
			if m.focus == FocusNamespaces {
				style = selectedStyle
			}
		}
		sb.WriteString(style.Render(ansi.Truncate(prefix+row.text, max(1, width-2), "…")))
		sb.WriteByte('\n')
	}
	sb.WriteString("\n")
	sb.WriteString(dimStyle.Render(ansi.Truncate("Enter/→ open · ←/Esc back", max(1, width), "…")))
	return sb.String()
}
