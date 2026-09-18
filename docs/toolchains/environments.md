# Environment Profiles & Dependency Caches

Packets manages external compiler toolchains and third-party library dependencies through declarative Environment Profiles and cached dependency bind mounts.

---

## Environment Profiles (`internal/environment`)

An Environment Profile defines the complete runtime specification needed by a project:

```go
type Profile struct {
    ID          string                 // e.g. "android-34", "rust-nightly", "zephyr"
    Description string                 
    Toolchains  []string               // Required toolchains ("go", "rust", "android")
    Components  []Component            // Specific SDK components & versions
    Metadata    map[string]string      // Target metadata (e.g. compile_sdk="34")
}
```

Profiles can be dynamically verified across workers via `EnvironmentService.CheckEnvironment`:

```bash
# Check if a target worker satisfies project requirements
packets doctor
```

---

## Dependency Cache Bind Mounts (`internal/cache/deps.go`)

To avoid re-downloading dependencies (such as Gradle cache, Maven repository, Cargo crates, or Go modules) on every execution, `DepManager` maintains persistent cache directories on the worker:

| Toolchain | Host Cache Directory | Injected Environment Variable |
| :--- | :--- | :--- |
| **Go** | `~/.packets/cache/<user>/go` | `GOPATH=...`, `GOCACHE=...` |
| **Rust** | `~/.packets/cache/<user>/cargo` | `CARGO_HOME=...` |
| **Android / Java** | `~/.packets/cache/<user>/gradle` | `GRADLE_USER_HOME=...` |
| **Node.js** | `~/.packets/cache/<user>/npm` | `npm_config_cache=...` |

When a job executes on the host runner, these directories are bound into the process environment, ensuring dependencies remain cached across tasks.
