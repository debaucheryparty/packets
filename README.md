# packets

Remote development execution platform that offloads heavy builds, test matrices, and shell commands from laptops to remote servers, dedicated VPS boxes, or peer workstations.

[![Release][release-badge]][release-url]
[![Go Reference][godoc-badge]][godoc-url]
[![MIT licensed][mit-badge]][mit-url]

[release-badge]: https://img.shields.io/github/v/release/debaucheryparty/packets.svg?label=latest
[release-url]: https://github.com/debaucheryparty/packets/releases
[actions-badge]: https://img.shields.io/github/v/debaucheryparty/packets/actions/workflows/ci.yml/badge.svg
[actions-url]: https://github.com/debaucheryparty/packets/actions/workflows/ci.yml
[godoc-badge]: https://pkg.go.dev/badge/github.com/debaucheryparty/packets.svg
[godoc-url]: https://pkg.go.dev/github.com/debaucheryparty/packets
[mit-badge]: https://img.shields.io/badge/license-MIT-blue.svg
[mit-url]: https://github.com/debaucheryparty/packets/blob/main/LICENSE

[Documentation](./docs/README.md) |
[Architecture](./docs/architecture.md) |
[Subspaces Guide](./docs/subspaces/README.md) |
[Releases](https://github.com/debaucheryparty/packets/releases)

---

## The Idea

> **The developer's machine is the interface. The remote machine performs the heavy compute.**

I built **Packets** out of personal necessity. I work on a 5-year-old laptop with only 8GB of RAM. Whenever I tried doing heavy development (compiling large codebases, running Docker containers, building Android or Rust projects, or running local AI tools), my laptop would lag, freeze, and struggle to keep up.

Upgrading hardware is not always an option, but limited hardware should never stop anyone from building ambitious software. I created Packets so that my laptop can stay fast and responsive as just the editing interface, while all the heavy compilation, container workloads, and testing happen on a remote machine.

---

## What Packets Does

- **Content-Addressable Delta Sync**: Workspaces are split into SHA-256 chunks. Only modified bytes are uploaded, syncing large projects in milliseconds over gRPC.
- **Persistent Subspaces (`packets subspace`)**: Remote development workspaces that keep compiler daemons (like the Gradle Daemon) and incremental build caches (`target/`, `.gradle/`, `GOCACHE`) warm across commands.
- **Remote Builds & Tests (`packets build`, `packets test`)**: Automatically detects over 25 language toolchains (Android, Rust, Go, C++, Swift, Python, Node), executes remotely, streams output live, and downloads built artifacts.
- **Remote Execution & Shell (`packets exec`, `packets subspace shell`)**: Run arbitrary commands or open interactive shells inside the persistent remote workspace.
- **Peer Worker Clustering (`packets worker join`)**: Turn an idle desktop or secondary laptop into an active compute worker in the cluster.
- **Model Context Protocol (`packets mcp`)**: Built-in MCP server for AI coding assistants (Claude Desktop, Cursor, Antigravity) with policy-based human approval workflows.

---

## Installation

### Pre-Compiled Binary
```bash
curl -fsSL https://raw.githubusercontent.com/debaucheryparty/packets/main/scripts/install.sh | bash
```

### Build from Source (Go 1.22+)
```bash
go install github.com/debaucheryparty/packets/cmd/packets@latest
```

---

## Quick Start

### 1. Check Server Connection
```bash
export PACKETS_SERVER_ADDR="vps.example.com:50051"
packets status
```

### 2. Build Remotely & Auto-Download Artifacts
```bash
cd my-project

# Auto-detects toolchain, syncs delta chunks, and downloads outputs
packets build --wait
```

### 3. Create a Persistent Subspace
```bash
# Allocate a warm remote workspace on your worker
packets subspace create --project mobile-app --worker vps-worker

# Run incremental builds inside the Subspace
packets build --subspace sub-xxxxxx --runner host --wait
```

### 4. Run Tests Remotely
```bash
# Streams test execution output directly to your terminal
packets test
```

### 5. Execute Commands in Remote Workspace
```bash
packets exec "./gradlew tasks"
packets exec "cargo check"
```

### 6. Connect AI Coding Assistants (MCP)
Add Packets to your Claude Desktop or Cursor MCP config:

```json
{
  "mcpServers": {
    "packets": {
      "command": "packets",
      "args": ["mcp"],
      "env": {
        "PACKETS_SERVER_ADDR": "vps.example.com:50051"
      }
    }
  }
}
```

---

## Documentation

Full guides, CLI references, and architecture deep dives are available in the [`docs/`](./docs/README.md) directory:

- [**System Architecture**](./docs/architecture.md): Topology, gRPC control plane, and CAS storage pipeline.
- [**CLI Reference**](./docs/cli/README.md): Flags, configuration files, and commands.
- [**Subspaces Guide**](./docs/subspaces/README.md): Persistent workspaces and state machines.
- [**Android Development**](./docs/toolchains/android.md): Gradle acceleration, ADB forwarding, and SDK discovery.
- [**MCP Server & Tools**](./docs/mcp/tools.md): Tool schemas for AI agent integrations.

---

## Screenshots

### Peer Fleet Status & Workspace Dashboard
![Peer Fleet and Queue Dashboard](./archive/tui-fleet-status.png)

### Remote Android Emulator Streaming
![Android Remote Emulator Session](./archive/android-remote-session.png)

### Artifact Extraction & Download
![Artifact Extraction](./archive/artifact-download.png)

---

## License

MIT © [Sahil](https://github.com/aikyaam) / [debaucheryparty](https://github.com/debaucheryparty)
