# Subspaces Concept & Architecture

Subspaces are persistent remote development environments in Packets. They solve the cold-start problem of ephemeral CI runners by binding a workspace snapshot, an environment profile, and a dedicated worker node together.

---

## The Problem with Ephemeral Builders

Standard remote build systems operate statelessly:
1. Spin up a container.
2. Clone or extract the entire workspace.
3. Download dependencies from scratch.
4. Execute build.
5. Destroy container.

For modern projects (especially Android, C++, Rust, and Scala), cold compilation can take 15-30 minutes, whereas incremental re-compilation takes under 10 seconds.

Subspaces maintain stateful, warm working directories on remote workers while preserving strict isolation between projects.

---

## Anatomy of a Subspace

A Subspace record groups five components:

| Component | Example Value | Role |
| :--- | :--- | :--- |
| **ID** | `sub-cd6515f86928` | Unique cluster identifier prefixed with `sub-`. |
| **Project** | `mobile-app` | Logical project identifier grouping builds and workspaces. |
| **Worker** | `vps-worker` | Node assignment where the workspace directory lives on disk. |
| **Environment** | `android` | Toolchain runtime settings (JDK version, compiler flags). |
| **State** | `ready` | Current lifecycle status (`creating`, `ready`, `busy`, `failed`, `terminated`). |

On the assigned worker, the Subspace maintains a persistent on-disk directory (`~/.packets/workspaces/<owner>/<project>/`). Compiler daemons (like the Gradle Daemon) and local build caches (`target/`, `.gradle/`, `GOCACHE`) remain warm across invocations.

---

## State Machine

Subspaces transition across the following states:

```
[creating] ---> [ready] <---> [busy]
                   |
                   +---> [failed]
                   |
                   +---> [terminated]
```

- **`creating`**: Worker allocating persistent directory and checking environment requirements.
- **`ready`**: Subspace is idle, warm, and ready to accept build, test, or shell jobs.
- **`busy`**: A compilation or shell task is currently executing inside the Subspace.
- **`failed`**: Subspace encountered an unrecoverable worker or environment error.
- **`terminated`**: Subspace has been destroyed; persistent resources are marked for cleanup.
