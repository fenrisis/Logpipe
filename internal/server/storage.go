package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/logpipe/logpipe/internal/logger"
	"github.com/logpipe/logpipe/internal/protocol"
	_ "github.com/mattn/go-sqlite3"
)

const schema = `
CREATE TABLE IF NOT EXISTS logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts TEXT NOT NULL,
    namespace TEXT NOT NULL,
    service TEXT NOT NULL,
    level TEXT NOT NULL,
    message TEXT NOT NULL,
    trace_id TEXT,
    extra TEXT,
    created_at INTEGER DEFAULT (unixepoch())
);

CREATE INDEX IF NOT EXISTS idx_ts ON logs(ts DESC);
CREATE INDEX IF NOT EXISTS idx_ns ON logs(namespace, service);
CREATE INDEX IF NOT EXISTS idx_level ON logs(level) WHERE level IN ('ERROR', 'WARN');
`

const ftsSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS logs_fts USING fts5(
    message, namespace, service,
    content='logs',
    content_rowid='id'
);

CREATE TRIGGER IF NOT EXISTS logs_fts_ai AFTER INSERT ON logs BEGIN
    INSERT INTO logs_fts(rowid, message, namespace, service)
    VALUES (new.id, new.message, new.namespace, new.service);
END;

CREATE TRIGGER IF NOT EXISTS logs_fts_ad AFTER DELETE ON logs BEGIN
    INSERT INTO logs_fts(logs_fts, rowid, message, namespace, service)
    VALUES ('delete', old.id, old.message, old.namespace, old.service);
END;
`

type Storage struct {
	db          *sql.DB
	subscribers []chan protocol.LogEntry
	subMu       sync.RWMutex
}

func NewStorage(dataDir string) (*Storage, error) {
	dbPath := filepath.Join(dataDir, "logpipe.db")
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Apply schema
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to apply schema: %w", err)
	}

	// Apply FTS5 schema
	if _, err := db.Exec(ftsSchema); err != nil {
		// FTS5 may not be available in all SQLite builds, fall back to LIKE
		logger.Warn("FTS5 not available, falling back to LIKE search", "error", err)
	}

	s := &Storage{
		db:          db,
		subscribers: make([]chan protocol.LogEntry, 0),
	}

	// Rebuild FTS index if empty but logs exist (first run on existing DB)
	s.rebuildFTSIfNeeded()

	return s, nil
}

func (s *Storage) Insert(entry protocol.LogEntry) error {
	extra := ""
	if entry.Extra != nil {
		extra = string(entry.Extra)
	}

	result, err := s.db.Exec(`
		INSERT INTO logs (ts, namespace, service, level, message, trace_id, extra)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.Timestamp.Format(time.RFC3339Nano),
		entry.Namespace,
		entry.Service,
		entry.Level,
		entry.Message,
		entry.TraceID,
		extra,
	)
	if err != nil {
		return fmt.Errorf("failed to insert log: %w", err)
	}

	id, _ := result.LastInsertId()
	entry.ID = id

	// Notify subscribers
	s.subMu.RLock()
	for _, ch := range s.subscribers {
		select {
		case ch <- entry:
		default:
			// Skip slow subscribers
		}
	}
	s.subMu.RUnlock()

	return nil
}

func (s *Storage) Query(filter protocol.Filter) ([]protocol.LogEntry, error) {
	query := `SELECT id, ts, namespace, service, level, message, trace_id, extra, created_at FROM logs WHERE 1=1`
	args := []interface{}{}

	if filter.Namespace != "" {
		query += " AND namespace = ?"
		args = append(args, filter.Namespace)
	}
	if filter.Service != "" {
		query += " AND service = ?"
		args = append(args, filter.Service)
	}
	if len(filter.Levels) > 0 {
		query += " AND level IN (?" + repeatString(",?", len(filter.Levels)-1) + ")"
		for _, l := range filter.Levels {
			args = append(args, l)
		}
	}
	if filter.Search != "" {
		if s.hasFTS() {
			query += " AND id IN (SELECT rowid FROM logs_fts WHERE logs_fts MATCH ?)"
			args = append(args, ftsQuery(filter.Search))
		} else {
			query += " AND message LIKE ?"
			args = append(args, "%"+filter.Search+"%")
		}
	}
	if !filter.From.IsZero() {
		query += " AND ts >= ?"
		args = append(args, filter.From.Format(time.RFC3339Nano))
	}
	if !filter.To.IsZero() {
		query += " AND ts <= ?"
		args = append(args, filter.To.Format(time.RFC3339Nano))
	}

	query += " ORDER BY ts DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query logs: %w", err)
	}
	defer rows.Close()

	var entries []protocol.LogEntry
	for rows.Next() {
		var entry protocol.LogEntry
		var tsStr, extra string
		var traceID sql.NullString

		err := rows.Scan(&entry.ID, &tsStr, &entry.Namespace, &entry.Service,
			&entry.Level, &entry.Message, &traceID, &extra, &entry.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		entry.Timestamp, _ = time.Parse(time.RFC3339Nano, tsStr)
		if traceID.Valid {
			entry.TraceID = traceID.String
		}
		if extra != "" {
			entry.Extra = json.RawMessage(extra)
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

func (s *Storage) GetNamespaces() ([]protocol.Namespace, error) {
	query := `
		SELECT
			namespace,
			COUNT(*) as log_count,
			SUM(CASE WHEN level = 'ERROR' THEN 1 ELSE 0 END) as error_count
		FROM logs
		WHERE created_at >= unixepoch() - 86400
		GROUP BY namespace
		ORDER BY namespace`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query namespaces: %w", err)
	}
	defer rows.Close()

	var namespaces []protocol.Namespace
	for rows.Next() {
		var ns protocol.Namespace
		if err := rows.Scan(&ns.Name, &ns.LogCount, &ns.ErrorCount); err != nil {
			return nil, fmt.Errorf("failed to scan namespace: %w", err)
		}

		// Get services for this namespace
		services, err := s.getServices(ns.Name)
		if err != nil {
			return nil, err
		}
		ns.Services = services

		namespaces = append(namespaces, ns)
	}

	return namespaces, nil
}

func (s *Storage) getServices(namespace string) ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT service FROM logs WHERE namespace = ? ORDER BY service`, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []string
	for rows.Next() {
		var svc string
		if err := rows.Scan(&svc); err != nil {
			return nil, err
		}
		services = append(services, svc)
	}
	return services, nil
}

func (s *Storage) Subscribe() <-chan protocol.LogEntry {
	ch := make(chan protocol.LogEntry, 1000)
	s.subMu.Lock()
	s.subscribers = append(s.subscribers, ch)
	s.subMu.Unlock()
	return ch
}

func (s *Storage) Unsubscribe(ch <-chan protocol.LogEntry) {
	s.subMu.Lock()
	defer s.subMu.Unlock()

	for i, sub := range s.subscribers {
		if sub == ch {
			s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
			close(sub)
			break
		}
	}
}

func (s *Storage) GetStats() (protocol.Stats, error) {
	var stats protocol.Stats

	// Count namespaces and services
	s.db.QueryRow(`SELECT COUNT(DISTINCT namespace), COUNT(DISTINCT namespace || '/' || service) FROM logs`).
		Scan(&stats.Namespaces, &stats.Services)

	// Total logs
	s.db.QueryRow(`SELECT COUNT(*) FROM logs`).Scan(&stats.TotalLogs)

	// Today's logs and errors
	s.db.QueryRow(`SELECT COUNT(*), SUM(CASE WHEN level = 'ERROR' THEN 1 ELSE 0 END) FROM logs WHERE created_at >= unixepoch() - 86400`).
		Scan(&stats.TodayLogs, &stats.TodayErrors)

	return stats, nil
}

func (s *Storage) Cleanup(retention time.Duration) (int64, error) {
	cutoff := time.Now().Add(-retention).Unix()
	result, err := s.db.Exec(`DELETE FROM logs WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	deleted, _ := result.RowsAffected()
	return deleted, nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

// hasFTS checks if the FTS5 table exists
func (s *Storage) hasFTS() bool {
	var name string
	err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='logs_fts'`).Scan(&name)
	return err == nil
}

// rebuildFTSIfNeeded rebuilds the FTS index if the FTS table is empty but logs exist
func (s *Storage) rebuildFTSIfNeeded() {
	if !s.hasFTS() {
		return
	}

	var ftsCount, logsCount int64
	s.db.QueryRow(`SELECT COUNT(*) FROM logs_fts`).Scan(&ftsCount)
	s.db.QueryRow(`SELECT COUNT(*) FROM logs`).Scan(&logsCount)

	if logsCount > 0 && ftsCount == 0 {
		logger.Info("rebuilding FTS index", "logs", logsCount)
		if _, err := s.db.Exec(`INSERT INTO logs_fts(logs_fts) VALUES ('rebuild')`); err != nil {
			logger.Error("failed to rebuild FTS index", "error", err)
		} else {
			logger.Info("FTS index rebuilt")
		}
	}
}

// ftsQuery converts user input into a safe FTS5 query with prefix matching.
// "connection timeo" → "connection* timeo*"
func ftsQuery(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return input
	}

	words := strings.Fields(input)
	var parts []string
	for _, w := range words {
		// Strip FTS5 special characters
		clean := strings.Map(func(r rune) rune {
			if strings.ContainsRune(`"*(){}[]^~:`, r) {
				return -1
			}
			return r
		}, w)
		if clean != "" {
			parts = append(parts, clean+"*")
		}
	}

	if len(parts) == 0 {
		return input
	}
	return strings.Join(parts, " ")
}

func repeatString(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
