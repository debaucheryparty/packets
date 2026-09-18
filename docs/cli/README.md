# Packets CLI Overview

The `packets` command-line utility provides the primary developer interface to the Packets distributed build runtime.

---

## Installation

### From Source
```bash
go install github.com/debaucheryparty/packets/cmd/packets@latest
```

### Pre-Compiled Binaries
Pre-compiled binaries for Linux (amd64, arm64), macOS (Apple Silicon, Intel), and Windows are distributed via GitHub releases.

```bash
# Verify installation
packets version
```

---

## Configuration Hierarchy

Packets resolves configuration through a precedence hierarchy (highest to lowest):

1. **CLI Flags**: Explicit command-line arguments (e.g. `--server`, `--runner`).
2. **Environment Variables & Dotenv**: Environment variables or local `.env.local` / `.env` files (e.g. `PACKETS_SERVER_ADDR`).
3. **Local Project Config**: `.packets.json` located in the current project directory.
4. **Global User Config**: `~/.packets/config.json` in the user's home directory.

### Environment Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PACKETS_SERVER_ADDR` | `127.0.0.1:50051` | Address and port of the `packetsd` gRPC server (also accepts legacy `ORACLE_VM_TAILSCALE_HOSTNAME`). |
| `PACKETS_HTTP_ADDR` | `127.0.0.1:9090` | Address and port of the `packetsd` HTTP / metrics server. |
| `PACKETS_AUTH_TOKEN` | *empty* | Shared bearer authentication token for secure clusters. |
| `PACKETS_DEFAULT_RUNNER`| `docker` | Default execution runner mode: `host`, `docker`, or `github`. |
| `PACKETS_APPROVAL_MODE` | `never` | Approval policy: `always`, `never`, or `on-miss`. |

### Project Configuration (`.packets.json`)

You can define project defaults in the root of any repository:

```json
{
  "name": "mobile-app",
  "toolchain": "android",
  "runner": "host",
  "build": {
    "command": "./gradlew",
    "args": ["assembleDebug"],
    "artifacts": ["app/build/outputs/apk/debug/*.apk"]
  },
  "test": {
    "command": "./gradlew",
    "args": ["test"]
  },
  "provider": ""
}
```

---

## Command Reference

| Command | Description | Link |
| :--- | :--- | :--- |
| `packets build` | Execute a remote compilation and pull artifacts | [Reference](./build-test.md) |
| `packets test` | Execute automated test suites remotely with live logs | [Reference](./build-test.md) |
| `packets exec` | Run arbitrary shell commands on remote workers | [Reference](./exec-shell.md) |
| `packets subspace` | Manage persistent Subspace environments | [Subspaces](../subspaces/management.md) |
| `packets status` | Display daemon connection health or query a job | `packets status [job-id]` |
| `packets logs` | Stream logs for an active or completed job | `packets logs <job-id>` |
| `packets cache` | Inspect or clear compilation and chunk caches | `packets cache clear [toolchain]` |
| `packets worker` | Join peer worker nodes or display local specs | [Reference](./worker-node.md) |
| `packets doctor` | Diagnose local toolchains, network, and daemon connectivity | `packets doctor` |

---

## Shell Autocompletion

Packets includes built-in autocompletion for Bash, Zsh, Fish, and PowerShell:

```bash
# Bash
packets completion bash > /etc/bash_completion.d/packets

# Zsh
packets completion zsh > "${fpath[1]}/_packets"

# PowerShell
packets completion powershell | Out-String | Invoke-Expression
```
