package protocol

import (
	"encoding/json"
	"time"
)

// LogLevel represents the severity of a log entry
type LogLevel string

const (
	LevelDebug LogLevel = "DEBUG"
	LevelInfo  LogLevel = "INFO"
	LevelWarn  LogLevel = "WARN"
	LevelError LogLevel = "ERROR"
)

// LogEntry represents a single log message
type LogEntry struct {
	ID        int64           `json:"id,omitempty"`
	Timestamp time.Time       `json:"ts"`
	Namespace string          `json:"namespace"`
	Service   string          `json:"service"`
	Level     LogLevel        `json:"level"`
	Message   string          `json:"message"`
	TraceID   string          `json:"trace_id,omitempty"`
	Extra     json.RawMessage `json:"extra,omitempty"`
	CreatedAt int64           `json:"created_at,omitempty"`
}

// Namespace represents a log namespace with its services
type Namespace struct {
	Name        string   `json:"name"`
	Services    []string `json:"services"`
	LogCount    int64    `json:"log_count"`
	ErrorCount  int64    `json:"error_count"`
}

// Filter for querying logs
type Filter struct {
	Namespace string     `json:"namespace,omitempty"`
	Service   string     `json:"service,omitempty"`
	Levels    []LogLevel `json:"levels,omitempty"`
	Search    string     `json:"search,omitempty"`
	From      time.Time  `json:"from,omitempty"`
	To        time.Time  `json:"to,omitempty"`
	Limit     int        `json:"limit,omitempty"`
	Offset    int        `json:"offset,omitempty"`
}

// Stats represents server statistics
type Stats struct {
	Namespaces   int   `json:"namespaces"`
	Services     int   `json:"services"`
	TotalLogs    int64 `json:"total_logs"`
	TodayLogs    int64 `json:"today_logs"`
	TodayErrors  int64 `json:"today_errors"`
	StorageBytes int64 `json:"storage_bytes"`
}
