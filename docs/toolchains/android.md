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
