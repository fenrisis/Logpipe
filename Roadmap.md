# Logpipe Roadmap

> CLI Grafana for Kubernetes — a terminal-based log viewer with k9s-style interface

## Vision

Logpipe is a local-first, zero-dependency log aggregator designed for developers who work with Kubernetes. Think of it as Grafana Loki, but:
- Runs entirely on your machine
- No cloud infrastructure needed
- Works directly with `kubectl` — no agents to deploy
- Interactive TUI like k9s

## Current Features (v0.1)

- **K8s Integration**: Stream logs from multiple namespaces via kubectl
- **TUI Interface**: k9s-style two-panel layout with namespace tree and log viewer
- **Filtering**: By namespace, service, log level, and text search
- **Error Focus**: Quick toggle to show only ERROR/WARN logs
- **Log Detail**: Full message view with metadata
- **Local Storage**: SQLite with automatic cleanup

## Roadmap

### Phase 1: Core Improvements

| Feature | Description | Status |
|---------|-------------|--------|
| Time range filter | Last hour/day/week selector | Planned |
| Regex search | Full regex support in search | Planned |
| JSON pretty-print | Format JSON in log detail view | Planned |
| Copy to clipboard | `y` to copy log message | Planned |
| Bookmarks | Save frequently used filters | Planned |

### Phase 2: Better K8s Experience

| Feature | Description | Status |
|---------|-------------|--------|
| Context switching | Switch kubectl contexts from TUI | Planned |
| Multi-cluster | View logs from multiple clusters | Planned |
| Pod selector | Interactive pod picker | Planned |
| Container logs | Select specific container in pod | Planned |
| Previous logs | View logs from crashed containers | Planned |

### Phase 3: Visualization

| Feature | Description | Status |
|---------|-------------|--------|
| Log rate sparkline | Mini graph showing log frequency | Planned |
| Error histogram | Visual error distribution over time | Planned |
| Service topology | Show service relationships | Planned |

### Phase 4: Advanced Features

| Feature | Description | Status |
|---------|-------------|--------|
| Trace correlation | Group logs by trace_id | Planned |
| Stack trace parsing | Collapse/expand stack traces | Planned |
| Custom parsers | YAML config for log format parsing | Planned |
| Loki integration | Pull logs from Grafana Loki | Planned |
| Export | Save filtered logs to file | Planned |

### Phase 5: Collaboration

| Feature | Description | Status |
|---------|-------------|--------|
| Share snippets | Generate shareable log excerpts | Planned |
| Team presets | Shared filter configurations | Planned |
| Slack integration | Send log context to Slack | Planned |

## Non-Goals

- **Not a log shipper**: We don't push logs anywhere, we pull them
- **Not a monitoring system**: No alerting, metrics, or dashboards
- **Not multi-user**: This is a personal developer tool

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT
