# Remote Execution & Interactive Shell

Packets provides flexible remote command execution via `packets exec` and `packets subspace shell`, allowing developers to run arbitrary terminal commands on persistent workers without SSH configuration or manual file transfers.

---

## `packets exec`

Synchronizes the local workspace and runs an arbitrary command remotely on the VPS or worker node.

```bash
packets exec [flags] <command...>
```

### Options & Flags

| Flag | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `--runner` | `string` | `host` | Execution mode: `host`, `docker`, or `local`. |
| `--subspace` | `string` | *none* | Target a persistent Subspace ID. |
| `--no-sync` | `bool` | `false` | Skip workspace synchronization before executing the command. |
| `--timeout` | `duration` | *none* | Execution timeout duration (e.g. `5m`, `30s`). |
| `--artifact` | `string[]` | *none* | Artifact glob patterns to retrieve after command finishes. |

### Examples

```bash
# 1. Run commands with automatic workspace sync
packets exec "ls -la"

# 2. Run without re-syncing workspace (fast execution)
packets exec --no-sync "cat /etc/os-release"

# 3. Target a persistent Subspace
packets exec --subspace sub-e91760a84f01 "./gradlew tasks"

# 4. Set execution timeout
packets exec --timeout 30s "pytest -v"
```

---

## `packets subspace shell`

Spawns an interactive or non-interactive shell command inside a Subspace's persistent workspace:

```bash
packets subspace shell <subspace-id> [-- <cmd>...]
```

When no arguments are specified after `--`, `sh` is launched by default:

```bash
# Interactive shell session
packets subspace shell sub-e91760a84f01

# Execute a single command inside the Subspace
packets subspace shell sub-e91760a84f01 -- uname -a
packets subspace shell sub-e91760a84f01 -- ./gradlew assembleDebug
```

> [!TIP]
> **Subspace Shell vs Plain SSH**
> Unlike SSH, `packets subspace shell` executes inside the persistent CAS workspace directory corresponding to your project, ensures file permissions and wrappers are intact, routes through policy checks, and streams output back with structured exit codes.
