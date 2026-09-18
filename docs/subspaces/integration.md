# Subspace Workflow Integration

Subspaces integrate directly with existing Packets commands via the `--subspace <subspace-id>` flag, redirecting builds, tests, and executions into persistent worker workspaces.

---

## Directing Builds to Subspaces

Passing `--subspace` to `packets build` routes the compilation task to the worker assigned to that Subspace. The worker executes inside the persistent directory rather than an ephemeral temporary folder:

```bash
# Compile project inside the persistent Subspace
packets build --subspace sub-e91760a84f01 --runner host --wait
```

### Benefits for Incremental Compilers

| Toolchain | Ephemeral Cold Build | Subspace Incremental Build |
| :--- | :--- | :--- |
| **Android / Gradle** | 3m 45s (full dex, aapt, deps) | **4.2s** (up-to-date tasks cached) |
| **Rust / Cargo** | 4m 10s (download & build crates) | **6.1s** (`target/` incremental cache) |
| **Go (`go build`)** | 45s (module download & compile) | **1.8s** (`GOCACHE` warm) |
| **C++ / CMake** | 5m 20s (compile all objects) | **3.5s** (Ninja/make delta compile) |

---

## Directing Tests to Subspaces

Running automated test suites in a persistent Subspace avoids re-compiling test harnesses:

```bash
# Execute unit tests inside the Subspace
packets test --subspace sub-e91760a84f01 --runner host
```

Logs stream live back to your terminal as individual test cases run.

---

## Multi-Branch / Multi-Feature Subspaces

Because Subspaces are identified by unique IDs, you can maintain independent persistent workspaces for different Git branches or features simultaneously:

```bash
# Create feature-specific Subspaces
packets subspace create --project backend-main --worker vps-worker
# Output: sub-1001

packets subspace create --project backend-refactor --worker vps-worker
# Output: sub-1002

# Build branch A
packets build --subspace sub-1001

# Switch and build branch B without blowing away branch A's compilation cache
packets build --subspace sub-1002
```
