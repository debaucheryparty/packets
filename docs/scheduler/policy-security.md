# Policy & Security Engine

Executing remote code on peer workstations or shared servers introduces security risks. Packets includes a policy engine that gates command execution with human-in-the-loop authorization, cryptographic tickets, and environment sandboxing.

---

## Approval Modes

The `PolicyEngine` evaluates every incoming `SubmitJob` request against one of three configurable modes:

| Mode | Behavior | Use Case |
| :--- | :--- | :--- |
| **`never`** (`ApprovalNever`) | Auto-approves all valid commands without human intervention. | Dedicated personal VPS, trusted private cloud instances, CI runners. |
| **`always`** (`ApprovalAlways`) | Every incoming command requires explicit human authorization. | Sharing compute with friends, untrusted peer developer machines. |
| **`on-miss`** (`ApprovalOnMiss`) | Prompts for authorization only if the command is not on the persistent whitelist. | Workstations with approved standard toolchains (`cargo`, `go`, `gradle`). |

---

## Execution Context & Approval Tickets

When a command requires review, the engine creates an `ExecutionContext`:

```go
type ExecutionContext struct {
    User         string   // Requesting user / owner
    ProjectID    string   // Target project identifier
    WorkspaceID  string   // Workspace snapshot hash
    Command      string   // Target executable or toolchain
    Args         []string // Full command arguments
    Action       string   // "BUILD" or "EXEC"
}
```

In interactive mode (`--friend`), the node operator sees a terminal prompt:

```
packets :: execution request from alice
  Project:  mobile-app
  Action:   EXEC
  Command:  ./gradlew assembleDebug
  Approve? [y/N/always]:
```

- **`y` (Yes)**: Generates a single-use `approval_ticket` valid for this specific execution context.
- **`always`**: Adds the command pattern to the persistent SQLite whitelist, approving future executions automatically.
- **`n` (No)**: Immediately terminates the request with `codes.PermissionDenied`.

---

## Host Runner Security Policy

When executing bare-metal commands on the host (`RunnerHost`), `internal/worker/executor.go` enforces strict sandboxing:

1. **Workspace Escape Detection**: Reject paths containing `..` or attempting to execute outside the designated workspace sandbox directory.
2. **Environment Variable Sanitization**: Strips sensitive environment variables from subprocesses before execution:
   - Specific tokens: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `GITHUB_TOKEN`, `CIRCLECI_TOKEN`, `PACKETS_AUTH_TOKEN`.
   - Suffixes: `*_TOKEN`, `*_SECRET`, `*_KEY`, `*_PASSWORD`, `*_PASS`, `*_CREDENTIALS`.
3. **Process Group Isolation**: Spawns jobs in isolated process groups (`Setpgid: true`) so that timeouts or cancellations cleanly terminate child worker processes.
