# Logpipe

**CLI Grafana for Kubernetes** — a terminal-based log viewer with k9s-style interface.

```
┌─ Logpipe ─────────────────────────────────────────────────────────────┐
│ NAMESPACES      │ LOGS                                   ● streaming  │
│                 │                                                     │
│ ▸ payments      │ 16:30:01 ERROR [payments/gateway] Connection timeout│
│   users         │ 16:30:00 INFO  [payments/gateway] Request processed │
│   notifications │ 16:29:58 DEBUG [payments/worker]  Job completed     │
│                 │ 16:29:55 WARN  [users/api]        Rate limit hit    │
├─────────────────┴─────────────────────────────────────────────────────┤
│ payments │ 156 logs │ 12 errors today     ↑/↓ navigate  q quit  ? help│
└───────────────────────────────────────────────────────────────────────┘
```

## Features

- **Zero setup** — works directly with your kubectl config
- **Multi-namespace** — view logs from multiple namespaces at once
- **Real-time streaming** — logs appear as they happen
- **Smart filtering** — by namespace, service, log level, or text search
- **Error focus** — one key to show only errors and warnings
- **Local storage** — logs are saved to SQLite for searching history
- **Offline capable** — view previously collected logs without cluster access

## Installation

### Download binary

```bash
# macOS Apple Silicon
curl -L https://github.com/YOUR_USER/logpipe/releases/latest/download/logpipe-darwin-arm64 -o logpipe

# macOS Intel
curl -L https://github.com/YOUR_USER/logpipe/releases/latest/download/logpipe-darwin-amd64 -o logpipe

# Linux
curl -L https://github.com/YOUR_USER/logpipe/releases/latest/download/logpipe-linux-amd64 -o logpipe

chmod +x logpipe
./logpipe k8s -n your-namespace
```

### Build from source

```bash
git clone https://github.com/YOUR_USER/logpipe.git
cd logpipe
make build
./bin/logpipe k8s -n your-namespace
```

## Usage

### Stream logs from Kubernetes

```bash
# Single namespace
logpipe k8s -n payments

# Multiple namespaces
logpipe k8s -n payments -n users -n notifications

# Filter by pod name
logpipe k8s -n payments -p gateway -p worker

# List pods without starting TUI
logpipe k8s -n payments --list
```

### Key Bindings

| Key | Action |
|-----|--------|
| `j` / `↓` | Move down |
| `k` / `↑` | Move up |
| `Tab` | Switch panel (namespaces ↔ logs) |
| `Enter` | Expand namespace / View log detail |
| `/` | Search logs |
| `e` | Toggle errors only |
| `f` | Toggle follow mode |
| `c` | Clear filters |
| `q` | Quit |

## Configuration

Config file: `~/.logpipe/config.yaml`

```yaml
server:
  port: 5555
  data_dir: ~/.logpipe/data
  retention: 168h  # 7 days

ui:
  timestamps: relative  # relative | absolute
  page_size: 100
```

## How It Works

Logpipe uses `kubectl logs` to stream logs from your cluster. No agents or sidecars needed — if you can run `kubectl logs`, you can use Logpipe.

```
┌─────────────────┐     kubectl logs      ┌─────────────────┐
│  K8s Cluster    │ ──────────────────▶  │    Logpipe      │
│                 │                       │                 │
│  Pod A ──┐      │                       │  SQLite Storage │
│  Pod B ──┼──────│                       │  TUI Viewer     │
│  Pod C ──┘      │                       │                 │
└─────────────────┘                       └─────────────────┘
```

## Requirements

- `kubectl` configured with cluster access
- Go 1.21+ (for building from source)

## License

MIT

## See Also

- [k9s](https://k9scli.io/) — Kubernetes CLI management
- [stern](https://github.com/stern/stern) — Multi-pod log tailing
- [Grafana Loki](https://grafana.com/oss/loki/) — Log aggregation system
