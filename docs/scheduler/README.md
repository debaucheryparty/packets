# Scheduler Daemon (`packetsd`)

The `packetsd` daemon is the central coordination service of a Packets cluster. It hosts the gRPC and HTTP APIs, manages the job dispatch queue, coordinates worker nodes, enforces security policies, and indexes content-addressable storage.

---

## Bootstrapping `packetsd`

### Basic Launch (Development / Single-Node)

```bash
# Run with automatic job approvals on all interfaces
packetsd --auto-approve --listen-grpc :50051 --listen-http :9090
```

### Production Launch (Systemd / Container)

```bash
export PACKETS_STORAGE_DIR="/var/lib/packets/storage"
export PACKETS_WORKSPACE_ROOT_DIR="/var/lib/packets/workspaces"
export PACKETS_DB_PATH="/var/lib/packets/packets.db"

packetsd \
  --listen-grpc 0.0.0.0:50051 \
  --listen-http 0.0.0.0:9090 \
  --auto-approve
```

---

## CLI Flags & Environment Variables

| Flag / Env Var | Default | Description |
| :--- | :--- | :--- |
| `--listen-grpc` / `PACKETS_GRPC_ADDR` | `:50051` | Address and port for the gRPC control plane. |
| `--listen-http` / `PACKETS_HTTP_ADDR` | `:9090` | Address and port for Prometheus metrics and HTTP health. |
| `--storage-dir` / `PACKETS_STORAGE_DIR` | `~/.packets/storage` | Content-addressable object store directory for chunk files. |
| `--db-path` / `PACKETS_DB_PATH` | `~/.packets/packets.db` | SQLite database path storing cluster metadata and jobs. |
| `--auto-approve` / `PACKETS_AUTO_APPROVE` | `false` | Disable human approval prompts for incoming commands. |
| `--friend` / `PACKETS_ROLE=friend` | `false` | Enable interactive console approvals on peer nodes. |

---

## HTTP Endpoints & Observability

`packetsd` serves an HTTP server on port 9090 (configurable via `--listen-http`):

- **`/healthz`**: Liveness probe returning HTTP 200 OK.
- **`/readyz`**: Readiness check verifying SQLite connectivity and storage availability.
- **`/metrics`**: Prometheus-formatted metrics tracking:
  - `packets_jobs_submitted_total{toolchain, cache_hit}`
  - `packets_jobs_failed_total{toolchain, runner}`
  - `packets_active_workers`
  - `packets_subspaces_total{state}`
