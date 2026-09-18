# Architecture

Packets splits local development from remote execution: you edit code on your workstation, while compilation, tests, and heavy processes run on remote servers, dedicated VPS boxes, or peer worker nodes.

Rather than treating remote machines as raw SSH targets or re-uploading the entire repository on every change, Packets uses content-addressable storage (CAS) to transfer only modified byte chunks and maintains persistent, warm workspaces called **Subspaces**.

---

## System Topology

Packets operates across three distinct tiers:

### 1. The Workstation (Local Client)
- **`packets` CLI**: Developer entry point for commands (`build`, `test`, `exec`, `subspace`).
- **CAS Merkle Scanner**: Hashes workspace files with SHA-256 and computes delta changes against the remote cache.
- **MCP Server**: Local Model Context Protocol adapter enabling coding assistants (Claude Desktop, Cursor) to manage cluster resources.

### 2. The Control Plane (`packetsd`)
- **gRPC Server (`:50051`)**: Multiplexed HTTP/2 endpoint handling job dispatching, chunk uploads, and worker streams.
- **HTTP Server (`:9090`)**: Exposes health probes (`/healthz`, `/readyz`) and Prometheus metrics (`/metrics`).
- **Scheduler & Worker Pool**: Tracks worker heartbeats and routes tasks to the least-loaded node.
- **Subspace Manager**: Coordinates persistent workspaces, environment profiles, and worker assignments.
- **Content-Addressable Storage (CAS)**: Deduplicated chunk store backed by local disk and SQLite metadata.

### 3. Compute Workers
- **Host Runner**: Executes directly on bare metal for fast, incremental builds using persistent caches (`.gradle`, `target/`).
- **Docker Runner**: Runs inside container images (`golang`, `rust`, `react-native-android`) for clean, isolated environments.
- **Peer Workers**: Secondary developer laptops or desktops joined to the cluster via `packets worker join`.

---

## Data Flow

```
Local Client (packets) ──gRPC :50051──> Scheduler (packetsd) ──Streams──> Compute Workers (Host / Docker)
         │                                       │
    Merkle Scan                             Object Store
   (Delta Chunks)                           (CAS Chunks)
```

1. The client scans the workspace, calculates file hashes, and asks `packetsd` which chunks are missing.
2. Only missing chunks are uploaded to the CAS store.
3. The scheduler assigns the job to a compute worker matching the toolchain and least active load.
4. The worker executes the task (inside a Subspace or fresh directory) and streams stdout/stderr back over gRPC.
5. If the build produces artifacts (e.g. `.apk` or binaries), they are packed and made available for local retrieval.

---

## Core Components

### 1. The Client (`packets`)

The client binary runs on your workstation. It has three main jobs:

- **Detect toolchains**: Scans directory markers to identify whether a project is Android, Rust, Go, C++, etc., and selects appropriate default compilers and arguments.
- **Compute file deltas**: Walks the workspace, checks `.gitignore` and `.packetsignore`, computes per-file SHA-256 hashes, and normalizes file permissions across Windows and POSIX.
- **Stream execution**: Connects to `packetsd` over gRPC (port `50051`) to stream stdout/stderr lines live to your local terminal and pull back build artifacts.

### 2. The Scheduler (`packetsd`)

The daemon runs on a server or VPS. It coordinates jobs, workers, storage, and security:

- **State Database**: SQLite database (WAL mode) tracking active jobs, worker nodes, Subspace records, and command approval tickets.
- **Content-Addressable Storage (CAS)**: Files are split and stored by their SHA-256 hash. If 10 jobs use the same library or unchanged source file, it is only stored once.
- **Worker Pool**: Tracks connected compute nodes, receives heartbeats, and routes tasks to the least-loaded worker with matching capabilities.
- **Policy Engine**: Intercepts commands before execution. Depending on configuration (`never`, `always`, `on-miss`), it can require human approval before running untrusted commands.

### 3. Execution Runners

Workers execute tasks through one of three runners:

| Runner | How It Runs | Primary Use Case |
| :--- | :--- | :--- |
| **`host`** | Runs natively on the worker OS inside the persistent workspace directory. | Incremental builds where compilers need persistent disk caches (e.g. Gradle daemon, Cargo `target/`, Go `GOCACHE`). |
| **`docker`** | Mounts the workspace directory into an isolated container image. | Hermetic builds requiring specific Linux distributions, system libraries, or CI images. |
| **`peer`** | Runs via `packets worker join` on secondary laptops or desktops. | Pooling spare machines in an office or home network. |

---

## The Subspace Model

Standard CI runners are ephemeral: they clone, build from scratch, and destroy the environment. For projects like Android or C++, cold compilation wastes minutes on every build.

Subspaces solve this by keeping state warm on the worker:

- **Project ID**: The logical project identifier (e.g. `mobile-core`).
- **Assigned Worker**: Binds jobs to a specific node (e.g. `vps-worker`).
- **Environment**: Toolchain specification (e.g. JDK 21 + Android SDK 34).
- **Workspace Path**: `~/.packets/workspaces/<owner>/<project_id>/` storing the persistent CAS source tree, warm Gradle deamon, and compiler artifacts.

When you run `packets build --subspace <id>`:
1. The client scans only what changed locally.
2. Missing delta chunks are uploaded to `packetsd`.
3. The worker updates modified files in place.
4. The build tool (e.g. `./gradlew` or `cargo`) runs incrementally, keeping compiler caches alive between invocations.

---

## Network Protocol & Endpoints

All internal communication between `packets`, `packetsd`, and worker nodes uses gRPC over HTTP/2:

- **`:50051` (gRPC)**: Core control plane. Handles `SubmitJob`, `StreamJobLogs`, `CreateSubspace`, `UploadChunk`, and worker heartbeat streams.
- **`:9090` (HTTP)**: Health probes (`/healthz`, `/readyz`) and Prometheus metrics (`/metrics`).
