package k8s

import (
	"testing"

	"github.com/logpipe/logpipe/internal/protocol"
)

func TestIsContinuation(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		// Whitespace prefixes
		{"space prefix", "  indented line", true},
		{"tab prefix", "\tindented line", true},
		{"multiple spaces", "    deeply indented", true},

		// Python stack traces
		{"python traceback", "Traceback (most recent call last):", true},
		{"python file line", "File \"/app/main.py\", line 42, in func", true},
		{"python file with spaces", "  File \"/app/main.py\", line 42", true},
		{"python caret", "    ^^^^^^^^^^^", true},
		{"python caret 2", "           ^^^^^^^^^^^^^^^^^^^^^^^^^^", true},

		// Go stack traces
		{"go goroutine", "goroutine 1 [running]:", true},
		{"go panic", "panic: runtime error: index out of range", true},

		// Java stack traces
		{"java at", "    at com.example.MyClass.method(MyClass.java:42)", true},
		{"java caused by", "Caused by: java.lang.NullPointerException", true},
		{"java more", "... 15 more", true},
		{"java exception", "Exception in thread \"main\" java.lang.Error", true},

		// Not continuations (new log lines)
		{"timestamp prefix", "2024-01-15 10:30:45 INFO message", false},
		{"level prefix", "INFO: some message", false},
		{"json log", "{\"level\":\"info\",\"message\":\"test\"}", false},
		{"empty line", "", false},
		{"regular message", "This is a normal log message", false},
		{"error message", "ERROR: something went wrong", false},
		{"bracket prefix", "[INFO] message", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isContinuation(tt.line)
			if result != tt.expected {
				t.Errorf("isContinuation(%q) = %v, want %v", tt.line, result, tt.expected)
			}
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected protocol.LogLevel
	}{
		// Bracket format [LEVEL]
		{"bracket error", "[ERROR] something failed", protocol.LevelError},
		{"bracket warn", "[WARN] something suspicious", protocol.LevelWarn},
		{"bracket warning", "[WARNING] something suspicious", protocol.LevelWarn},
		{"bracket info", "[INFO] normal message", protocol.LevelInfo},
		{"bracket debug", "[DEBUG] verbose message", protocol.LevelDebug},

		// Colon format LEVEL:
		{"colon error", "ERROR: something failed", protocol.LevelError},
		{"colon warn", "WARN: something suspicious", protocol.LevelWarn},
		{"colon info", "INFO: normal message", protocol.LevelInfo},

		// JSON format
		{"json error", "{\"level\":\"error\",\"message\":\"test\"}", protocol.LevelError},
		{"json warn", "{\"level\":\"warn\",\"message\":\"test\"}", protocol.LevelWarn},
		{"json info", "{\"level\":\"info\",\"message\":\"test\"}", protocol.LevelInfo},
		{"json debug", "{\"level\":\"debug\",\"message\":\"test\"}", protocol.LevelDebug},

		// Case insensitive
		{"lowercase error", "error: something failed", protocol.LevelError},
		{"mixed case", "Error: something failed", protocol.LevelError},

		// Error indicators without explicit level
		{"exception keyword", "java.lang.NullPointerException: null", protocol.LevelError},
		{"traceback keyword", "Traceback (most recent call last):", protocol.LevelError},
		{"panic keyword", "panic: runtime error", protocol.LevelError},
		{"fatal keyword", "fatal: cannot connect", protocol.LevelError},

		// Python format
		{"python error", "2024-01-15 10:30:45 - myapp - ERROR - message", protocol.LevelError},
		{"python info", "2024-01-15 10:30:45 - myapp - INFO - message", protocol.LevelInfo},

		// Default to INFO
		{"no level", "Just a regular message", protocol.LevelInfo},
		{"http log", "GET /api/users 200 OK", protocol.LevelInfo},

		// Edge cases
		{"err shorthand", "ERR something failed", protocol.LevelError},
		{"critical", "CRITICAL: database down", protocol.LevelError},
		{"fatal level", "FATAL: cannot start", protocol.LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLogLevel(tt.message)
			if result != tt.expected {
				t.Errorf("parseLogLevel(%q) = %v, want %v", tt.message, result, tt.expected)
			}
		})
	}
}

func TestExtractServiceName(t *testing.T) {
	tests := []struct {
		name     string
		podName  string
		expected string
	}{
		// Standard K8s deployment pods
		{"simple deployment", "gateway-7f8b9c6d5-x2j4k", "gateway"},
		{"hyphenated name", "api-gateway-7f8b9c6d5-x2j4k", "api-gateway"},
		{"multi-hyphen", "my-cool-service-7f8b9c6d5-x2j4k", "my-cool-service"},

		// StatefulSet pods (note: current logic treats short suffixes as K8s generated)
		{"statefulset", "redis-0", "redis-0"},
		{"statefulset multi", "postgres-master-0", "postgres"}, // "master" + "0" look like K8s suffixes

		// Short names
		{"short name", "nginx-abc12", "nginx-abc12"},
		{"two parts", "app-xyz", "app-xyz"},

		// Edge cases
		{"single word", "standalone", "standalone"},
		{"long suffix", "myapp-deployment-7f8b9c6d5-abcde", "myapp-deployment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractServiceName(tt.podName)
			if result != tt.expected {
				t.Errorf("extractServiceName(%q) = %v, want %v", tt.podName, result, tt.expected)
			}
		})
	}
}
