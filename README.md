# Logpipe

[![CI](https://github.com/fenrisis/Logpipe/actions/workflows/ci.yml/badge.svg)](https://github.com/fenrisis/Logpipe/actions/workflows/ci.yml)

Logpipe is a local-first terminal log explorer for Kubernetes. It tails logs through `kubectl`, stores them in SQLite, and provides a k9s-style interface for searching and inspecting logs without deploying another component into the cluster.

> Project status: alpha. Logpipe is usable for local development and debugging, but its storage and ingestion protocols are not intended for untrusted or internet-facing environments.

```text
┌─ namespaces/services ──┬─ live logs ──────────────────────────────────┐
│ production             │ 12:04:18 INFO request completed             │
│   └─ api               │ 12:04:19 WARN retrying job                  │
│ staging                │ 12:04:20 ERROR payment failed               │
└────────────────────────┴──────────────────────────────────────────────┘
```

## Why Logpipe

- No in-cluster installation: collection uses your existing `kubectl` context.
- Multi-namespace, multi-pod, and multi-container log collection.
- Persistent local storage backed by SQLite and FTS5 search.
- Filtering by namespace, derived service name, log level, and text query.
- Multiline log grouping for stack traces and structured output.
- XDG-compatible configuration and data paths.

## Architecture

```text
Kubernetes API
      │
      ▼
kubectl logs -f ──► Kubernetes collector ──┐
                                          ├──► SQLite + FTS5 ──► Unix socket ──► Go TUI
Optional JSON lines ──► TCP collector ─────┘
```

`logpipe k8s` starts the Kubernetes collector, local API, and TUI in one process. `logpipe server` starts the optional local TCP ingestion server; run `logpipe` separately to connect the TUI to it.

The TCP collector listens on `127.0.0.1` by default. Binding it to a non-loopback address exposes an unauthenticated ingestion endpoint, so only do that on a trusted network or behind an authenticated proxy.

## Requirements

- Go 1.24 or newer.
- A C compiler, because the SQLite driver uses CGO.
- `kubectl` configured for the target cluster when using Kubernetes mode.

## Installation

Download a binary from [GitHub Releases](https://github.com/fenrisis/Logpipe/releases), or build from source:

```bash
git clone https://github.com/fenrisis/Logpipe.git
cd Logpipe
make build
```

## Kubernetes usage

```bash
# Collect from one namespace using the current kubectl context.
./bin/logpipe k8s --namespace production

# Restrict collection to selected namespaces.
./bin/logpipe k8s --namespace production --namespace staging

# Restrict collection to pods whose names contain a substring.
./bin/logpipe k8s --namespace production --pod gateway
```

Main controls:

| Key | Action |
| --- | --- |
| `↑` / `↓`, `j` / `k` | Move selection |
| `Enter` | Expand a namespace or open the selected log entry |
| `Tab` | Change panel |
| `/` | Search logs |
| `f` | Pause or resume the one-second UI refresh |
| `c` | Clear filters |
| `q` | Quit |

Run `logpipe k8s --help` for all collection and storage options.

Logpipe does not store Kubernetes credentials or implement a separate login flow. Every cluster operation is executed through the local `kubectl` command and therefore uses its selected context, kubeconfig, authentication provider, and Kubernetes RBAC permissions.

## Optional TCP ingestion

The local server also accepts newline-delimited JSON logs on `127.0.0.1:5555`. This is a low-level optional interface rather than a supported language SDK.

```bash
./bin/logpipe server
```

Example message:

```bash
printf '%s\n' '{"namespace":"local","service":"demo","level":"INFO","message":"hello"}' | nc 127.0.0.1 5555
```

## Configuration

The default configuration file is `$XDG_CONFIG_HOME/logpipe/config.yaml` (usually `~/.config/logpipe/config.yaml`). Generate it with:

```bash
logpipe config init
```

Example:

```yaml
server:
  host: 127.0.0.1
  port: 5555
  data_dir: ~/.local/share/logpipe
  retention: 7d
  cleanup_interval: 1

ui:
  theme: dark
  timestamps: relative
  page_size: 100
```

Command-line flags override configuration values.

## Development

```bash
go test -tags sqlite_fts5 ./...
go test -race -tags sqlite_fts5 ./...
go vet -tags sqlite_fts5 ./...
```

GitHub Actions runs formatting checks, Go tests with the race detector, `go vet`, and a production build for every pull request and push to `main`.

## Current limitations

- Regular pod containers are discovered every 10 seconds; init and ephemeral containers are not streamed.
- The TUI queries local storage once per second rather than receiving pushed updates.
- TCP ingestion has no built-in authentication or TLS.
- SQLite makes Logpipe a single-host tool rather than a centralized logging platform.
- Multiline and log-level parsing are heuristic and may need tuning for unusual formats.

See the [roadmap](ROADMAP.md) for planned work.

## License

[MIT](LICENSE)
