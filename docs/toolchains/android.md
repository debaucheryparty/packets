# Android & Mobile Development

Compiling Android applications locally is notorious for thermal throttling, heavy memory usage, and slow build times. Packets provides end-to-end acceleration for Gradle-based Android workflows.

---

## Remote Android Compilation

When executing `packets build` inside an Android repository:

1. **Gradle Wrapper Detection**: Packets identifies `gradlew`, preserves executable permissions, and detects `app/src/main/AndroidManifest.xml`.
2. **SDK Discovery**: Workers auto-discover local Android SDK installations (checking `$ANDROID_HOME`, `~/android-sdk`, `~/Android/Sdk`, or `/opt/android-sdk`).
3. **Incremental Daemon**: Inside persistent Subspaces, the remote Gradle Daemon stays warm between builds, utilizing Gradle's configuration cache and compilation cache.
4. **Artifact Retrieval**: After compilation succeeds, debug APKs (matching `app/build/outputs/apk/debug/*.apk`) are automatically downloaded to your local machine.

```bash
# Build Android APK in a persistent Subspace on your VPS
packets build --subspace sub-e91760a84f01 --runner host --wait
```

---

## Toolchain Resolver (`internal/environment/android.go`)

The `AndroidResolver` validates whether a remote worker has the prerequisites to compile your project:

| Requirement | Detection Check | Auto-Preparation Command |
| :--- | :--- | :--- |
| **JDK 17+** | `java -version` | `apt-get update && apt-get install -y openjdk-17-jdk` |
| **Android SDK** | `resolveAndroidSDK()` | Downloads cmdline-tools from Google repository |
| **Android Platform** | `$ANDROID_HOME/platforms/android-<ver>` | `sdkmanager "platforms;android-<ver>"` |
| **Build Tools** | `$ANDROID_HOME/build-tools/<ver>` | `sdkmanager "build-tools;<ver>"` |
| **Gradle Wrapper** | `gradle-wrapper.properties` | Configured wrapper version validation |

---

## Virtual Device & ADB Orchestration (`internal/android`)

Packets includes dedicated abstractions for managing remote Android emulators and debugging sessions:

- **`DeviceClassifier`**: Automatically categorizes connected Android devices as `USB`, `Network` (IP:Port), or `Emulator` (`emulator-5554`).
- **`SessionManager`**: Allocates virtual display sessions on remote workers with hardware acceleration (KVM).
- **`Watcher`**: Monitors build output directories and signals local test harnesses when new APK artifacts are compiled.

---

## Android CLI Commands

Packets provides integrated commands for managing mobile workflows:

### `packets android devices`
Lists connected Android physical devices, network devices, and emulators. Packets checks the local ADB server first and automatically queries the remote worker node if local devices are not found.
```bash
# List available devices across local and remote nodes
packets android devices

# Target devices inside a specific remote Subspace
packets android devices --subspace sub-e91760a84f01
```
*Note: Remote device detection is handled automatically. Stale flags like `--remote` are not required.*

### `packets android dev`
Runs interactive mobile development mode with file watching, automated incremental recompilation, automatic APK installation, and live screen mirroring via `scrcpy`.
```bash
# Start live development with scrcpy display
packets android dev . --package=com.example.app --activity=.MainActivity

# Run headless without opening the display window
packets android dev . --package=com.example.app --no-display
```
*Note: Use `--no-display` to suppress the screen window (the flag is `--no-display`, not `--display`).*

### `packets android connect`
Connects your local machine's ADB server to a remote emulator running on your cloud VPS or remote worker.
```bash
# Connect local ADB to remote emulator
packets android connect --host 100.64.0.1 --port 5555
```

---

## Network Architecture & Required Ports

When developing remotely with Packets and Android emulators, ensure the following ports are open or forwarded:

| Port | Protocol | Purpose | Direction |
| :--- | :--- | :--- | :--- |
| **`50051`** | gRPC (HTTP/2) | Packets daemon control plane, build job dispatch, and log streaming | Client -> Worker |
| **`9090`** | HTTP | Prometheus metrics, health checks (`/healthz`, `/readyz`), and storage downloads | Client -> Worker |
| **`5554`** | TCP | Android emulator control console (telnet) | Client <-> Emulator |
| **`5555`** | TCP | ADB daemon (adbd) connection and `scrcpy` video stream | Client <-> Emulator |

---

## Android Emulator 5554 + 5555 Networking

The Android emulator architecture binds to a pair of consecutive ports for every running instance:
1. **Console Port (`5554`)**: Used by the emulator to expose control commands (geo, power, snapshot, battery). Local ADB connects to this console port to verify emulator health and read device configuration.
2. **ADB Transport Port (`5555`)**: Used by `adbd` inside the Android guest OS for debugging commands, file transfers, and application installation.

### Port Forwarding Gotcha (GitHub Codespaces / Cloud VPS)
If you forward **only port 5555** over an SSH tunnel or GitHub Codespaces port-forwarding:
- Local ADB connects to port 5555, but attempts to query port 5554 to probe the emulator status.
- Because port 5554 is unreachable, ADB will report the device as `offline` or fail to complete the handshake.

**Solution**: Always forward **both port 5554 and port 5555**:
```bash
# Forward both console and ADB daemon ports when tunneling
ssh -L 5554:localhost:5554 -L 5555:localhost:5555 user@your-vps
```
In GitHub Codespaces, ensure both ports `5554` and `5555` are set to forwarded in the Ports panel.

---

## Troubleshooting: Java / Gradle IPv6 Issues in Cloud Environments

In certain cloud environments (such as Azure virtual machines or GitHub Codespaces containers), the host network resolves external domains to dual-stack IPv4 and IPv6 (`AAAA` records), but the underlying infrastructure lacks outbound IPv6 routing.

Because modern JVMs (including Java 17, 21, and 25) attempt IPv6 connections by default when an IPv6 address is resolved, Gradle and Maven dependency downloads can fail with:
```
java.net.SocketException: Network is unreachable (connect failed)
```

This is an environmental host networking limitation rather than a defect in Packets. To force Java to use IPv4 routing, configure one of the following:

1. **In `gradle.properties`** (project-level or `~/.gradle/gradle.properties`):
   ```properties
   org.gradle.jvmargs=-Djava.net.preferIPv4Stack=true
   ```

2. **In environment variables** (session or container level):
   ```bash
   export _JAVA_OPTIONS="-Djava.net.preferIPv4Stack=true"
   ```

