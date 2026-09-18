# Worker Nodes & Compute Clustering

Packets allows any secondary computer (whether a high-performance desktop in your office, a cloud VPS, or a peer developer's laptop) to contribute compute capacity to the cluster.

---

## `packets worker join`

Connects the local computer to a remote `packetsd` scheduler daemon as an active peer worker node.

```bash
packets worker join [flags]
```

### Options & Flags

| Flag | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `--scheduler` | `string` | `127.0.0.1:50051` | Target `packetsd` gRPC server address. |
| `--worker-id` | `string` | *hostname* | Unique worker identifier. |
| `--labels` | `string` | *none* | Comma-separated key=value labels (e.g. `gpu=true,arch=arm64`). |
| `--max-jobs` | `int` | `5` | Maximum concurrent compilation jobs allowed. |

### How It Works

1. **Hardware & Capability Probe**: On startup, the worker inspects its CPU cores, RAM, and looks for installed compilers (`rustc`, `go`, `javac`, `gradle`, `cmake`, etc.).
2. **Bidirectional gRPC Stream**: The worker opens `Scheduler.RegisterWorkerStream` and sends a `WorkerHello` handshake.
3. **Heartbeat Loop**: The worker pulses periodic `WorkerHeartbeat` messages carrying current CPU/memory load and active job counts.
4. **Execution Dispatch**: When the scheduler assigns a job, the worker executes it, streams back stdout/stderr chunks, and submits the final `WorkerJobResult`.

```bash
# Register a peer workstation as a compute worker
packets worker join --scheduler vps.internal:50051 --labels arch=amd64,highmem=true
```

---

## `packets worker status`

Displays local machine capabilities, CPU count, total RAM, and detected compilers:

```bash
packets worker status
```

Example output:

```
packets :: worker node specifications
  ID:          mbp-m3-pro
  OS/Arch:     darwin/arm64
  CPUs:        12
  Memory:      36 GB
  Docker:      online
  Toolchains:  go, rust, node, python, swift
```
