# Subspace Management

The `packets subspace` command suite manages persistent remote Subspaces directly from your local terminal.

```bash
packets subspace [command]
```

---

## Subspace Commands

### 1. `packets subspace create`

Allocates a new persistent Subspace on the target worker.

```bash
packets subspace create [flags]
```

| Flag | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `--project` | `string` | *current dir* | Project identifier. |
| `--worker` | `string` | `local` | Target worker ID (e.g. `vps-worker`, `node-01`). |
| `--env` | `string` | `default` | Environment profile ID. |
| `--ws` | `string` | *none* | Explicit workspace snapshot ID to initialize with. |

```bash
# Example
packets subspace create --project mobile-core --worker vps-worker --env android
```

Output:
```
✓ Subspace created successfully
  ID:          sub-e91760a84f01
  Project:     mobile-core
  Worker:      vps-worker
  Environment: android
  State:       ready
```

---

### 2. `packets subspace list`

Lists all persistent Subspaces in the cluster.

```bash
packets subspace list [flags]
```

| Flag | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `--project` | `string` | *none* | Filter by project identifier. |
| `--owner` | `string` | *none* | Filter by owner identifier. |
| `--json` | `bool` | `false` | Output results in JSON format. |

```bash
packets subspace list
```

Output:
```
SUBSPACE ID         PROJECT       WORKER        STATE    CREATED                LAST USED
sub-e91760a84f01    mobile-core   vps-worker    ready    2026-09-18T15:07:03Z   2026-09-18T15:08:23Z
sub-7193bca908df    rust-engine   builder-02    ready    2026-09-18T12:30:15Z   2026-09-18T14:22:40Z
```

---

### 3. `packets subspace status`

Displays comprehensive details and metadata for a specific Subspace.

```bash
packets subspace status <subspace-id>
```

```bash
packets subspace status sub-e91760a84f01
```

Output:
```
Subspace:    sub-e91760a84f01
Project:     mobile-core
Owner:       default
Worker:      vps-worker
Workspace:   ws-sub-e91760a84f01
Environment: android
State:       ready
Created:     2026-09-18T15:07:03Z
Last Used:   2026-09-18T15:08:23Z
Metadata:
  compile_sdk: 34
  java_home: /usr/local/sdkman/candidates/java/21.0.12+1-ms
```

---

### 4. `packets subspace shell`

Runs an interactive or single-command shell inside the persistent workspace of the Subspace.

```bash
packets subspace shell <subspace-id> [-- <command...>]
```

```bash
# Run interactive bash/sh inside the Subspace
packets subspace shell sub-e91760a84f01

# Run Gradle task inside the warm Subspace
packets subspace shell sub-e91760a84f01 -- ./gradlew assembleDebug
```

---

### 5. `packets subspace destroy`

Tears down the Subspace, frees the worker assignment, and marks the persistent state as terminated.

```bash
packets subspace destroy <subspace-id>
```

```bash
packets subspace destroy sub-e91760a84f01
```

Output:
```
✓ Subspace sub-e91760a84f01 destroyed
```
