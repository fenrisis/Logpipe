package server

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/fenrisis/logpipe/internal/protocol"
)

func TestSourceInventoryAndFilteringThroughAPI(t *testing.T) {
	s, err := NewStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, row := range []struct{ ns, service, message, extra string }{
		{"prod", "api", "first application", `{"pod":"api-one","container":"app"}`},
		{"prod", "api", "second application", `{"pod":"api-two","container":"app"}`},
		{"prod", "api", "sidecar metrics", `{"pod":"api-one","container":"metrics"}`},
		{"prod", "database", "database ready", `{"pod":"postgres-zero","container":"db"}`},
		{"staging", "api", "staging application", `{"pod":"api-one","container":"app"}`},
		{"prod", "legacy", "old application", `{"pod":"legacy-pod"}`},
		{"prod", "local", "local entry", ""},
	} {
		entry := protocol.LogEntry{Timestamp: time.Now(), Namespace: row.ns, Service: row.service,
			Level: protocol.LevelInfo, Message: row.message, Extra: json.RawMessage(row.extra)}
		if err := s.Insert(entry); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate metadata retained from an older producer. Invalid JSON must not
	// prevent Kubernetes inventory or filtering for otherwise healthy records.
	if _, err := s.db.Exec(`INSERT INTO logs(ts, namespace, service, level, message, extra)
		VALUES ('2026-01-01T00:00:00Z', 'prod', 'old', 'INFO', 'malformed', 'not json')`); err != nil {
		t.Fatal(err)
	}

	a := NewAPI(t.TempDir(), s)
	response := a.handleRequest(Request{Method: "getNamespaces", ID: 1})
	if response.Error != "" {
		t.Fatal(response.Error)
	}
	var namespaces []protocol.Namespace
	if err := json.Unmarshal(response.Result, &namespaces); err != nil {
		t.Fatal(err)
	}
	wantPods := []protocol.Pod{
		{Name: "api-one", Containers: []string{"app", "metrics"}},
		{Name: "api-two", Containers: []string{"app"}},
		{Name: "legacy-pod"},
		{Name: "postgres-zero", Containers: []string{"db"}},
	}
	if len(namespaces) != 2 || namespaces[0].Name != "prod" || !reflect.DeepEqual(namespaces[0].Pods, wantPods) {
		t.Fatalf("unexpected inventory: %+v", namespaces)
	}

	for _, tc := range []struct {
		name   string
		filter protocol.Filter
		want   []string
	}{
		{"one pod", protocol.Filter{Namespace: "prod", Pod: "api-one"}, []string{"sidecar metrics", "first application"}},
		{"one container", protocol.Filter{Namespace: "prod", Pod: "api-one", Container: "app"}, []string{"first application"}},
		{"sibling same service", protocol.Filter{Namespace: "prod", Pod: "api-two"}, []string{"second application"}},
		{"namespace isolation", protocol.Filter{Namespace: "staging", Pod: "api-one", Container: "app"}, []string{"staging application"}},
		{"old metadata", protocol.Filter{Namespace: "prod", Pod: "legacy-pod"}, []string{"old application"}},
		{"search and source", protocol.Filter{Namespace: "prod", Pod: "api-one", Search: "application"}, []string{"first application"}},
		{"unknown source", protocol.Filter{Namespace: "prod", Pod: "missing"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params, err := json.Marshal(tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			response := a.handleRequest(Request{Method: "getLogs", Params: params, ID: 2})
			if response.Error != "" {
				t.Fatal(response.Error)
			}
			var entries []protocol.LogEntry
			if err := json.Unmarshal(response.Result, &entries); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, entry := range entries {
				got = append(got, entry.Message)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("messages = %v, want %v", got, tc.want)
			}
		})
	}
}
