# packets

A remote build execution and caching system designed specifically for developers using low-end laptops. It is:

* **Fast**: packets' zero-cost abstractions and Git-aware caching give you instant build times if a teammate has already compiled a specific commit.
* **Seamless**: packets hooks right into your normal workflow without you having to change how you work in your IDE or local terminal.
* **Flexible**: packets has a minimal footprint and gracefully falls back to local execution or GitHub Actions naturally.

[![Release][release-badge]][release-url]
[![Build Status][actions-badge]][actions-url]
[![Go Reference][godoc-badge]][godoc-url]
[![MIT licensed][mit-badge]][mit-url]

[release-badge]: https://img.shields.io/github/v/release/debaucheryparty/packets.svg?label=latest
[release-url]: https://github.com/debaucheryparty/packets/releases
[actions-badge]: https://github.com/debaucheryparty/packets/actions/workflows/ci.yml/badge.svg
[actions-url]: https://github.com/debaucheryparty/packets/actions/workflows/ci.yml
[godoc-badge]: https://pkg.go.dev/badge/github.com/debaucheryparty/packets.svg
[godoc-url]: https://pkg.go.dev/github.com/debaucheryparty/packets
[mit-badge]: https://img.shields.io/badge/license-MIT-blue.svg
[mit-url]: https://github.com/debaucheryparty/packets/blob/main/LICENSE

[Setup Guide (Coming Soon)](#) |
[API Docs (Coming Soon)](#) |
[Releases](https://github.com/debaucheryparty/packets/releases)

## Overview

**Packets** is a remote development execution platform for developers and AI coding agents.

The core principle:
> **The developer's machine is the interface. The remote machine performs expensive development work.**

Packets provides:
* **Remote Development Mode (`packets dev .`)**: Automatic polyglot project detection (Android, Zephyr, Rust, Go, Node, CMake), remote environment checks, workspace sync, and AI instructions generation.
* **Persistent Remote Workspaces (`packets sync`)**: Incremental file synchronization and full synchronization with remote deletion detection.
* **Remote Command Execution (`packets exec`)**: Run arbitrary shell commands, compilers, or test suites directly on the remote VPS with real-time log streaming.
* **Model Context Protocol (`packets mcp`)**: Standardized stdio MCP server for AI IDEs (Cursor, VS Code + Copilot, Claude Desktop, Antigravity) with human-in-the-loop approval and sandboxing.
* **Environment Manager (`packets env`)**: Automatic toolchain and SDK detection, verification, and provisioning (`detect`, `check`, `prepare`).
## Installation

Install the pre-compiled executable via our setup script:

```bash
curl -fsSL https://raw.githubusercontent.com/debaucheryparty/packets/main/scripts/install.sh | bash
```

Alternatively, build from source using Go 1.22+:

```bash
git clone https://github.com/debaucheryparty/packets.git
cd packets
go build -o packets ./cmd/packets
```

## Quick Start

### 1. Initialize Remote Development Session
```bash
packets dev .
```
Detects project components, checks remote toolchain readiness, configures AI agent rules (`AGENTS.md`, `.cursor/rules`), and sets up MCP.

### 2. Remote Command Execution
```bash
packets exec "./gradlew assembleDebug"
packets exec "cargo test"
packets exec -- uname -a
```

### 3. Workspace Synchronization
```bash
# Incremental synchronization
packets sync .

# Full synchronization with remote deletion detection
packets sync --full .
```

### 4. Environment Inspection & Provisioning
```bash
packets env detect .
packets env check .
packets env prepare .
```

### 5. AI Coding Agent MCP Server
Configure your AI IDE (Cursor, VS Code Claude Dev / Copilot, Claude Desktop) with:
```json
{
  "mcpServers": {
    "packets": {
      "command": "packets",
      "args": ["mcp"]
    }
  }
}
``` 

## Getting Help

First, see if the answer to your question can be found in our [Documentation](guide.md) or [API Docs](https://pkg.go.dev/github.com/debaucheryparty/packets). If you still need help, feel free to open a GitHub Issue or Discussion!

## License

This project is licensed under the [MIT license](LICENSE).
