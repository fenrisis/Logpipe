//go:build sqlite_fts5 || fts5

package server

import (
	"testing"
	"time"

	"github.com/fenrisis/logpipe/internal/protocol"
)

func TestFTS5BuildSupportsFullTextSearch(t *testing.T) {
	storage, err := NewStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}
	defer storage.Close()

	if !storage.hasFTS() {
		t.Fatal("FTS5 table was not created; build must use the sqlite_fts5 tag")
	}

	entry := protocol.LogEntry{
		Timestamp: time.Now(),
		Namespace: "test",
		Service:   "search",
		Level:     protocol.LevelInfo,
		Message:   "distinctive searchable message",
	}
	if err := storage.Insert(entry); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	logs, err := storage.Query(protocol.Filter{Search: "distinctive", Limit: 10})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(logs) != 1 || logs[0].Message != entry.Message {
		t.Fatalf("FTS query returned %#v, want inserted log", logs)
	}
}
