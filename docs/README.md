# Packets Documentation

Packets lets you edit code locally on your laptop while running the actual builds, tests, and shells on another machine: a remote VPS, a homelab server, or an idle machine on your local network.

Instead of full git clones or raw SSH loops, Packets syncs only modified file chunks via content-addressable storage (CAS) and keeps persistent build caches warm in remote workspaces called **Subspaces**.

---

## Core Philosophy

Modern software compilation (whether building multi-module Android applications, large Rust crates, C++ codebases, or running containerized test matrices) strains laptop thermals, drains battery life, and locks the local developer machine during heavy build steps.

Cloud CI services solve part of this problem, but they introduce multi-minute queue latencies, require pushing commits to Git, lack interactive shell access, and break feedback loops.

Packets establishes a local-remote development model:

1. **Keep editing locally**: You use your local editor, IDE, and terminal of choice without latency.
2. **Execute remotely**: Workspaces are chunked via content-addressable storage (CAS) and synchronized incrementally to remote workers over gRPC.
3. **Stream back feedback**: Remote builds, tests, logs, and artifacts stream back instantly into your local terminal.

> [!NOTE]
> Code edits remain local. Compilation, tests, and heavy processes run on remote workers, streaming output back to your terminal in real time.

---

## Technical Advantages

| Feature | Description |
| :--- | :--- |
| **Content-Addressable Sync** | Workspace files are hashed into SHA-256 chunks. Only modified delta chunks are transferred over the wire, cutting sync times to fractions of a second. |
| **Persistent Subspaces** | Remote development environments that keep workspace state, build caches, and environment configurations warm between commands. |
| **Zero-Config Toolchain Detection** | Built-in heuristics identify 25+ language toolchains (Android, Rust, Go, C++, Java, Kotlin, Swift, Python, etc.) and auto-select optimal execution targets. |
| **Peer-to-Peer Worker Clustering** | Workers connect to the central `packetsd` scheduler over bidirectional gRPC streams, self-reporting hardware specs, active load, and toolchain capabilities. |
| **Policy & Sandboxing Engine** | Node operators control execution permissions with approval modes (`never`, `always`, `on-miss`), command whitelisting, and environment sanitization. |
| **Model Context Protocol (MCP)** | First-class MCP server exposing build, test, worker inspection, and Subspace management directly to AI programming agents. |

---

## Documentation Directory

Explore detailed architecture breakdowns, CLI guides, and subsystem references.

### 1. Conceptual Architecture

- [**System Architecture**](./architecture.md): Comprehensive technical overview of `packetsd`, gRPC protocols, CAS sync, and the execution lifecycle.

### 2. CLI Reference

- [**CLI Overview & Global Configuration**](./cli/README.md): Installation, configuration files, authentication tokens, and global flags.
- [**Build & Test Workflows**](./cli/build-test.md): Executing remote builds (`packets build`) and tests (`packets test`), managing artifacts, and cache keys.
- [**Remote Execution & Shell**](./cli/exec-shell.md): Arbitrary command execution (`packets exec`) and interactive remote workspace shells.
- [**Worker Pairing & Management**](./cli/worker-node.md): Joining peer compute workers (`packets worker join`) and inspecting node specs.

### 3. Subspaces

- [**Subspaces Concept & Lifecycle**](./subspaces/README.md): State machine, worker affinity, and persistent workspace storage.
- [**Subspace Management**](./subspaces/management.md): Full CLI command reference (`create`, `list`, `status`, `shell`, `destroy`).
- [**Workflow Integration**](./subspaces/integration.md): Directing `build`, `test`, and `exec` commands into persistent Subspaces.

### 4. Scheduler & Storage

- [**Scheduler Daemon (`packetsd`)**](./scheduler/README.md): Service bootstrapping, SQLite metadata store, and HTTP/gRPC endpoints.
- [**Worker Pool Clustering**](./scheduler/worker-pool.md): Peer heartbeat monitoring, least-loaded scheduling algorithms, and node draining.
- [**Policy & Security Engine**](./scheduler/policy-security.md): Execution contexts, human-in-the-loop approval workflows, and token security.
- [**Caching & Storage Pipeline**](./scheduler/caching-storage.md): Merkle-like workspace scanner, chunked deduplication, and artifact management.

### 5. Toolchains & Environments

- [**Built-In Toolchains**](./toolchains/README.md): Registry heuristics, execution strategies, and container images.
- [**Android & Mobile Workflows**](./toolchains/android.md): Android SDK resolver, Gradle wrappers, ADB bridge, and virtual device orchestration.
- [**Environment Profiles**](./toolchains/environments.md): Declarative environment definitions, dependency cache binding, and custom project settings.

### 6. AI Agent Integration

- [**Model Context Protocol (MCP)**](./mcp/README.md): Connecting AI coding assistants (Claude Desktop, Cursor, Antigravity) to your Packets cluster.
- [**MCP Tools Reference**](./mcp/tools.md): Complete JSON schemas and tool definitions for autonomous cluster management.

---

## Getting Started

1. **Install the CLI binary**:
   ```bash
   go install github.com/debaucheryparty/packets/cmd/packets@latest
   ```

2. **Start or connect to a `packetsd` daemon**:
   ```bash
   # On your remote VPS or server
   packetsd --auto-approve
   ```

3. **Configure your local client**:
   ```bash
   export PACKETS_SERVER_ADDR="vps.example.com:50051"
   packets status
   ```

4. **Dispatch your first build**:
   ```bash
   cd my-project
   packets build --wait
   ```
