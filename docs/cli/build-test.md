# Remote Builds & Automated Tests

Packets provides `packets build` and `packets test` to offload resource-intensive compilation and test suites to remote workers with automatic workspace synchronization and artifact harvesting.

---

## `packets build`

Executes a remote build for the detected toolchain or specified command.

```bash
packets build [flags] [-- <args...>]
```

### Options & Flags

| Flag | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `--runner` | `string` | `docker` | Execution runner: `host`, `docker`, or `github`. |
| `--toolchain` | `string` | *auto* | Explicit toolchain: `android`, `rust`, `go`, `cpp`, `java`, etc. |
| `--subspace` | `string` | *none* | Target a persistent Subspace ID (`sub-xxxxxx`). |
| `--artifact` | `string[]` | *auto* | Glob pattern of files to collect from the remote workspace. |
| `--wait` | `bool` | `false` | Block until build completes and auto-download artifacts. |
| `--force` | `bool` | `false` | Force complete re-upload of all workspace chunks. |
| `--dry-run` | `bool` | `false` | Scan and list workspace files and root hash without building. |
| `--source` | `string` | `workspace`| Source mode: `workspace` (local sync) or `git` (remote clone). |

### Examples

```bash
# 1. Auto-detect toolchain, build remotely, and wait for artifacts
packets build --wait

# 2. Build on bare-metal host runner in a persistent Subspace
packets build --runner host --subspace sub-451bb392f012 --wait

# 3. Override default build arguments
packets build --wait -- ./gradlew assembleRelease

# 4. Dry-run workspace scan to inspect files and calculated root hash
packets build --dry-run
```

---

## `packets test`

Executes automated tests remotely, streaming test outputs directly to stdout.

```bash
packets test [dir] [flags] [-- <args...>]
```

### Options & Flags

| Flag | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `--runner` | `string` | `host` | Execution runner: `host`, `docker`, `github`, or `local`. |
| `--subspace` | `string` | *none* | Target a persistent Subspace ID. |
| `--wait` | `bool` | `true` | Stream live test execution logs (default: `true`). |
| `--force` | `bool` | `false` | Force complete re-upload of the workspace. |

### Examples

```bash
# 1. Run tests for current project
packets test

# 2. Run tests for a specific subdirectory
packets test ./core/engine

# 3. Target a persistent Subspace with custom arguments
packets test --subspace sub-e91760a84f01 -- --verbose --filter UnitTests
```

---

## Cache Key Computation

Before executing a build, Packets generates a deterministic cache key based on inputs:

```
CacheKey = SHA256(ProjectID + ":" + Toolchain + ":" + Runner + ":" + SourceMode + ":" + SnapshotRef + ":" + CommandArgs)
```

If the cache key matches a previously successful job:
1. The compilation step is skipped.
2. The remote Scheduler instantly flags `cache_hit: true`.
3. Cached artifacts are immediately made available for local download.

---

## Artifact Collection

When a build completes successfully (`ExitCode == 0`), matched artifact patterns are packed into a compressed archive (`output.tar.gz`) on the worker:

```bash
# Explicitly query or re-download artifacts for any completed job
packets artifact get <job-id> -d ./dist
```

If `--wait` was passed to `packets build`, artifacts matching the configured patterns are automatically extracted into your local working tree.
