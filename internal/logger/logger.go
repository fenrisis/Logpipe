package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// Config holds logger configuration
type Config struct {
	LogPath   string     // Full path to log file
	Level     slog.Level // Minimum log level
	MaxSizeMB int        // Max file size before rotation in MB
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		LogPath:   filepath.Join(home, ".logpipe", "logpipe.log"),
		Level:     slog.LevelInfo,
		MaxSizeMB: 10,
	}
}

// Global state
var (
	globalLogger atomic.Pointer[slog.Logger]
	globalWriter *rotatingWriter
	globalMu     sync.Mutex
	initialized  bool
)

func init() {
	// Set a no-op logger until Init is called
	globalLogger.Store(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// rotatingWriter handles file rotation by size
type rotatingWriter struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	maxBytes int64
	curBytes int64
}

func newRotatingWriter(path string, maxSizeMB int) (*rotatingWriter, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	w := &rotatingWriter{
		path:     path,
		maxBytes: int64(maxSizeMB) * 1024 * 1024,
	}

	if err := w.openFile(); err != nil {
		return nil, err
	}

	return w, nil
}

func (w *rotatingWriter) openFile() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	// Get current file size
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("failed to stat log file: %w", err)
	}

	w.file = f
	w.curBytes = info.Size()
	return nil
}

func (w *rotatingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check if rotation needed
	if w.curBytes+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			// If rotation fails, try to continue writing anyway
			fmt.Fprintf(os.Stderr, "log rotation failed: %v\n", err)
		}
	}

	n, err = w.file.Write(p)
	w.curBytes += int64(n)
	return
}

func (w *rotatingWriter) rotate() error {
	// Close current file
	if w.file != nil {
		w.file.Close()
	}

	// Rename current to .log.1 (overwrite if exists)
	backupPath := w.path + ".1"
	os.Remove(backupPath)
	if err := os.Rename(w.path, backupPath); err != nil && !os.IsNotExist(err) {
		// If rename fails, try to continue with new file anyway
		fmt.Fprintf(os.Stderr, "failed to rotate log file: %v\n", err)
	}

	// Open new file
	return w.openFile()
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// textHandler implements slog.Handler with human-readable format
type textHandler struct {
	level  slog.Level
	writer io.Writer
	mu     *sync.Mutex
}

func newTextHandler(w io.Writer, level slog.Level) *textHandler {
	return &textHandler{
		level:  level,
		writer: w,
		mu:     &sync.Mutex{},
	}
}

func (h *textHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *textHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Format: 2024-01-15 10:30:45 [ERROR] message key=value
	timestamp := r.Time.Format("2006-01-02 15:04:05")
	level := levelString(r.Level)

	line := fmt.Sprintf("%s [%s] %s", timestamp, level, r.Message)

	// Append any additional attributes
	r.Attrs(func(a slog.Attr) bool {
		line += fmt.Sprintf(" %s=%v", a.Key, a.Value)
		return true
	})

	line += "\n"

	_, err := h.writer.Write([]byte(line))
	return err
}

func (h *textHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// For simplicity, return the same handler
	// A full implementation would store attrs for later use
	return h
}

func (h *textHandler) WithGroup(name string) slog.Handler {
	// For simplicity, return the same handler
	return h
}

func levelString(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return "DEBUG"
	case l < slog.LevelWarn:
		return "INFO"
	case l < slog.LevelError:
		return "WARN"
	default:
		return "ERROR"
	}
}

// Init initializes the global logger with the given configuration
func Init(cfg Config) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	if initialized {
		return nil
	}

	w, err := newRotatingWriter(cfg.LogPath, cfg.MaxSizeMB)
	if err != nil {
		return err
	}

	globalWriter = w
	handler := newTextHandler(w, cfg.Level)
	globalLogger.Store(slog.New(handler))
	initialized = true

	return nil
}

// Close closes the log file
func Close() error {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalWriter != nil {
		err := globalWriter.Close()
		globalWriter = nil
		initialized = false
		return err
	}
	return nil
}

// Debug logs at DEBUG level
func Debug(msg string, args ...any) {
	globalLogger.Load().Debug(msg, args...)
}

// Info logs at INFO level
func Info(msg string, args ...any) {
	globalLogger.Load().Info(msg, args...)
}

// Warn logs at WARN level
func Warn(msg string, args ...any) {
	globalLogger.Load().Warn(msg, args...)
}

// Error logs at ERROR level
func Error(msg string, args ...any) {
	globalLogger.Load().Error(msg, args...)
}

// RecoverAndLog recovers from panic and logs with stack trace.
// Usage: defer logger.RecoverAndLog("goroutine name")
func RecoverAndLog(ctx string) {
	if r := recover(); r != nil {
		stack := debug.Stack()
		Error("panic recovered",
			"context", ctx,
			"panic", fmt.Sprintf("%v", r),
			"stack", string(stack),
		)
	}
}

// SetLevel changes the minimum log level at runtime
func SetLevel(level slog.Level) {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalWriter != nil {
		handler := newTextHandler(globalWriter, level)
		globalLogger.Store(slog.New(handler))
	}
}
