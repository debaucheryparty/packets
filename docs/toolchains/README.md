# Toolchain Registry & Detection

Packets features an automated toolchain detection engine (`internal/toolchain`) capable of identifying over 25 compiler environments from project marker files and directory patterns.

---

## Built-In Toolchain Registry

| Toolchain | Marker Files / Dirs | Default Local Command | Default Args | Default Container Image |
| :--- | :--- | :--- | :--- | :--- |
| **Android** | `gradlew` + `app/` | `./gradlew` | `assembleDebug` | `reactnativecommunity/react-native-android:latest` |
| **Kotlin** | `build.gradle.kts` | `gradle` | `build` | `gradle:latest` |
| **Groovy** | `build.gradle` | `gradle` | `build` | `gradle:latest` |
| **Java (Maven)** | `pom.xml` | `mvn` | `compile` | `maven:latest` |
| **Scala** | `build.sbt` | `sbt` | `compile` | `sbtscala/scala-sbt:latest` |
| **Rust** | `Cargo.toml` | `cargo` | `build` | `rust:latest` |
| **C++** | `CMakeLists.txt` | `cmake` | `--build .` | `gcc:latest` |
| **C** | `Makefile`, `configure` | `make` | *none* | `gcc:latest` |
| **Go** | `go.mod` | `go` | `build ./...` | `golang:latest` |
| **Python** | `pyproject.toml` | `python` | `-m build` | `python:latest` |
| **Node.js** | `package.json` | `npm` | `run build` | `node:latest` |
| **Swift** | `Package.swift` | `swift` | `build` | *host CI runner* |
| **Flutter** | `pubspec.yaml` + `android`/`ios` | `flutter` | `build` | *host CI runner* |
| **Zig** | `build.zig` | `zig` | `build` | `euantorano/zig:latest` |
| **.NET** | `Directory.Build.props` | `dotnet` | `build` | `mcr.microsoft.com/dotnet/sdk:latest` |
| **Zephyr RTOS** | `west.yml`, `prj.conf` | `west` | `build` | `ghcr.io/zephyrproject-rtos/ci:v0.26.8` |

---

## Detection Heuristics

When `toolchain.NewDetector` scans a directory:

1. Evaluates all registered toolchains.
2. Assigns points for matching `DetectFiles` and `DetectDirs`.
3. Selects the highest scoring toolchain. For example, a project containing both `gradlew` and an `app/` directory matches `Android` (score 2) with higher precedence than generic `Groovy` (score 1).
4. If ambiguous or unrecognized, falls back to `exec` mode allowing arbitrary commands to run.

---

## Overriding Toolchains

You can explicitly override auto-detection using the `--toolchain` flag or via `.packets.json`:

```bash
# Force Rust toolchain on custom directories
packets build --toolchain rust -- cargo check
```
