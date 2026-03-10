# Changelog

## [0.1.2] - 2026-03-10

### Fixed
- **Verbose flag actually works now** — `-v` / `--verbose` was ignored because logger initialized before cobra parsed flags; moved to `PersistentPreRunE`
- **Scanner buffer overflow** — API and client scanners used default 64KB buffer, causing crashes with ~40-55 logs; increased to 10MB max
- **Subscriber channel overflow** — increased from 100 to 1000 entries to handle high-volume namespaces (20+ pods)
- **Logger reinitialization** — removed `initialized` guard that prevented changing log level at runtime

### Changed
- **XDG Base Directory compliance** — paths moved per freedesktop.org spec:
  - Config: `~/.config/logpipe/config.yaml` (was `~/.logpipe/config.yaml`)
  - Data/DB/socket: `~/.local/share/logpipe/` (was `~/.logpipe/`)
  - Logs: `~/.local/state/logpipe/logpipe.log` (was `~/.logpipe/logpipe.log`)
  - Respects `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME` env vars
  - Legacy `~/.logpipe/config.yaml` is still read as fallback

## [0.1.1] - 2025-02-12

### Added
- **Internal logging system** — application logs to `~/.logpipe/logpipe.log` with automatic rotation at 10MB
- **Verbose mode** — `-v` / `--verbose` flag enables DEBUG level logging for troubleshooting
- **Separate log level filters** — `e`/`w`/`i`/`d` keys to filter by ERROR/WARN/INFO/DEBUG (can combine multiple)
- **Multiline log grouping** — stack traces are now merged into single log entries instead of separate lines
- **Panic recovery** — crashes in goroutines are caught and logged with stack traces
- **Unit tests** — tests for `isContinuation()`, `parseLogLevel()`, `extractServiceName()`

### Fixed
- **Column alignment** — fixed service name column jumping due to variable length names
- **Mouse selection** — disabled mouse capture to allow text selection for copying

### Security
- **Socket permissions** — changed from 0666 to 0600 (owner-only access)

## [0.1.0] - 2024-12-22

### Added
- Initial release
- K8s log streaming via kubectl
- Interactive TUI with namespace/service navigation
- SQLite storage with retention policy
- Real-time log streaming
- Text search
- Error-only filter
- Log detail view
