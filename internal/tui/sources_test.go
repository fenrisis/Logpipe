package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fenrisis/logpipe/internal/protocol"
)

func sourceTestModel() Model {
	m := NewModel()
	m.width, m.height = 150, 30
	m.namespaces = []protocol.Namespace{
		{Name: "prod", Pods: []protocol.Pod{
			{Name: "api-one", Containers: []string{"app", "metrics"}},
			{Name: "api-two", Containers: []string{"app"}},
			{Name: "postgres-zero", Containers: []string{"db"}},
		}},
		{Name: "staging", Pods: []protocol.Pod{{Name: "worker", Containers: []string{"app"}}}},
	}
	m.restoreSourceSelection()
	return m
}

func pressSourceKey(m Model, key tea.KeyType) (Model, tea.Cmd) {
	updated, cmd := m.Update(tea.KeyMsg{Type: key})
	return updated.(Model), cmd
}

func TestKeyboardSelectsPodContainerAndParent(t *testing.T) {
	m := sourceTestModel()
	m.streaming = false // navigation must work even when follow is paused
	if view := m.renderSidebar(48, 26); !strings.Contains(view, "api-one") || !strings.Contains(view, "postgres-zero") {
		t.Fatalf("pods not visible before pressing Enter: %s", view)
	}
	var cmd tea.Cmd
	m, cmd = pressSourceKey(m, tea.KeyEnter)
	if filter := m.buildFilter(); filter.Namespace != "prod" || filter.Pod != "api-one" || filter.Container != "" || cmd == nil {
		t.Fatalf("Enter did not select/fetch first pod: %+v", filter)
	}
	m, cmd = pressSourceKey(m, tea.KeyRight)
	if filter := m.buildFilter(); filter.Pod != "api-one" || filter.Container != "app" || cmd == nil {
		t.Fatalf("right did not select/fetch first container: %+v", filter)
	}
	m, _ = pressSourceKey(m, tea.KeyDown)
	if got := m.buildFilter().Container; got != "metrics" {
		t.Fatalf("container = %q", got)
	}
	m, _ = pressSourceKey(m, tea.KeyEsc)
	if filter := m.buildFilter(); filter.Pod != "api-one" || filter.Container != "" {
		t.Fatalf("Esc parent = %+v", filter)
	}
	m, _ = pressSourceKey(m, tea.KeyLeft)
	if filter := m.buildFilter(); filter.Namespace != "prod" || filter.Pod != "" {
		t.Fatalf("left parent = %+v", filter)
	}
	m, _ = pressSourceKey(m, tea.KeyDown)
	m, _ = pressSourceKey(m, tea.KeyDown)
	if got := m.buildFilter().Pod; got != "api-two" {
		t.Fatalf("cannot move to sibling pod: %q", got)
	}
	m, _ = pressSourceKey(m, tea.KeyTab)
	if m.focus != FocusLogs || m.buildFilter().Pod != "api-two" {
		t.Fatal("Tab lost the pod filter")
	}
}

func TestRefreshPreservesSourceAndRejectsDelayedLogs(t *testing.T) {
	m := sourceTestModel()
	m, _ = pressSourceKey(m, tea.KeyEnter)
	oldFilter := m.buildFilter()
	m, _ = pressSourceKey(m, tea.KeyDown)
	selected := m.selectedSource
	// Insert new pod and namespace before the selected source.
	updated := append([]protocol.Namespace{{Name: "aaa"}}, m.namespaces...)
	updated[1].Pods = append([]protocol.Pod{{Name: "aaa-pod"}}, updated[1].Pods...)
	model, _ := m.Update(namespacesMsg(updated))
	m = model.(Model)
	if m.selectedSource != selected {
		t.Fatalf("refresh changed source from %+v to %+v", selected, m.selectedSource)
	}
	current := protocol.LogEntry{Message: "selected pod"}
	model, _ = m.Update(logsMsg{filter: m.buildFilter(), entries: []protocol.LogEntry{current}})
	m = model.(Model)
	model, _ = m.Update(logsMsg{filter: oldFilter, entries: []protocol.LogEntry{{Message: "wrong pod"}}})
	m = model.(Model)
	if len(m.logs) != 1 || m.logs[0].Message != current.Message {
		t.Fatalf("stale reply replaced selected logs: %+v", m.logs)
	}
	m, _ = pressSourceKey(m, tea.KeyDown)
	if len(m.logs) != 0 {
		t.Fatal("source change displayed previous pod logs while waiting for reply")
	}
}

func TestMissingSourceFallsBackAndClearsLogs(t *testing.T) {
	m := sourceTestModel()
	m, _ = pressSourceKey(m, tea.KeyEnter)
	m.logs = []protocol.LogEntry{{Message: "removed pod"}}
	model, cmd := m.Update(namespacesMsg([]protocol.Namespace{{Name: "prod"}}))
	m = model.(Model)
	if m.buildFilter().Pod != "" || m.buildFilter().Namespace != "prod" || len(m.logs) != 0 || cmd == nil {
		t.Fatalf("missing pod fallback failed: %+v", m.buildFilter())
	}
	model, _ = m.Update(namespacesMsg(nil))
	m = model.(Model)
	if m.selectedSource != (logSource{}) {
		t.Fatal("selection retained an absent namespace")
	}
	m, _ = pressSourceKey(m, tea.KeyDown)
	m, _ = pressSourceKey(m, tea.KeyEnter)
}

func TestSidebarScrollKeepsSelectedPodVisible(t *testing.T) {
	m := NewModel()
	m.namespaces = []protocol.Namespace{{Name: "prod"}}
	for i := 0; i < 60; i++ {
		m.namespaces[0].Pods = append(m.namespaces[0].Pods, protocol.Pod{Name: fmt.Sprintf("pod-%02d", i)})
	}
	m.selectSource(logSource{namespace: "prod", pod: "pod-59"})
	view := m.renderSidebar(48, 14)
	if !strings.Contains(view, "pod-59") || strings.Contains(view, "pod-00") {
		t.Fatalf("sidebar failed to scroll: %s", view)
	}
	if lines := strings.Count(view, "\n"); lines > 14 {
		t.Fatalf("sidebar overflow: %d lines", lines)
	}
}

func TestServiceFallbackForLogsWithoutPodMetadata(t *testing.T) {
	m := NewModel()
	m.namespaces = []protocol.Namespace{{Name: "local", Services: []string{"api", "db"}}}
	m.restoreSourceSelection()
	m, _ = pressSourceKey(m, tea.KeyEnter)
	if m.buildFilter().Service != "api" {
		t.Fatal("legacy service selection broken")
	}
	m, _ = pressSourceKey(m, tea.KeyDown)
	if m.buildFilter().Service != "db" {
		t.Fatal("legacy service navigation broken")
	}
}

func TestSourceViewFitsTerminalAndShowsContainer(t *testing.T) {
	for _, width := range []int{80, 120, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := sourceTestModel()
			m.width = width
			m.help.Width = width
			m, _ = pressSourceKey(m, tea.KeyEnter)
			m, _ = pressSourceKey(m, tea.KeyEnter)
			m.logs = []protocol.LogEntry{{Service: "api", Level: protocol.LevelInfo,
				Message: "application ready", Extra: []byte(`{"pod":"api-one","container":"app"}`)}}
			view := m.View()
			if !strings.Contains(view, "api-one") || !strings.Contains(view, "app") || !strings.Contains(view, "metrics") {
				t.Fatalf("source tree incomplete: %s", view)
			}
			if got := lipgloss.Width(view); got > width {
				t.Fatalf("view width = %d, terminal = %d", got, width)
			}
			if got := lipgloss.Height(view); got > m.height {
				t.Fatalf("view height = %d, terminal = %d", got, m.height)
			}
			if width == 120 {
				t.Log("\n" + view)
			}
		})
	}
}
