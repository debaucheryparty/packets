<p align="center">
  <h1 align="center">📦 Packets</h1>
  <p align="center">A zero-cost remote build execution system</p>
</p>

[![release](https://img.shields.io/github/v/release/waris4ly/packets.svg?label=latest)](https://github.com/waris4ly/packets/releases)
[![Build Status](https://github.com/waris4ly/packets/actions/workflows/ci.yml/badge.svg)](https://github.com/waris4ly/packets/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/waris4ly/packets)](https://goreportcard.com/report/github.com/waris4ly/packets)

Packets is an elegant remote build execution system.
It offloads compilation in **any programming language** from a local machine to remote compute, while keeping your IDE experience completely unchanged.

## Features

* Complete support for **30+ programming languages** out of the box
* Written in pure Go, ensuring zero-dependency, static binaries
* **Git-Aware Caching**: Uses `git ls-tree` and `git status` for instant, robust state-dependent cache keys
* **Zero-Config Fallback**: Gracefully falls back to local execution if the remote node is unreachable
* **Real-Time Logs**: Streams compilation stdout/stderr directly to your local terminal via NATS
* **Direct-CI Mode**: Bypass self-hosted infrastructure and dispatch builds directly to GitHub Actions
* Security: Secured by default using Tailscale `whois` gRPC interceptors

## Install

### Command-line executable

```bash
go install github.com/waris4ly/packets/cmd/packets@latest
```

### Pre-compiled binaries (Mac/Linux)

Download the latest compiled binaries for your OS directly from [GitHub Releases](https://github.com/waris4ly/packets/releases).

### CI Integration

You can integrate Packets directly into your CI pipeline using our action (if you are dispatching to a serverless mode):

```bash
export DIRECT_CI_MODE=true
packets build .
```

## Usage

### As a CLI builder (Local execution to Remote)

Run the CLI in your project directory. The registry will automatically detect your toolchain (e.g., finding a `go.mod`, `Cargo.toml`, or `Package.swift`) and route the build appropriately.

```console
$ packets build /path/to/your/project
> Detecting toolchain...
> Cache miss. Dispatching to remote server...
> Compiling...
> Done in 1.2s.
```

Or pass custom arguments to the underlying compiler:

```console
$ packets build /path/to/your/project -- -v -race
```

### As a remote Daemon

Start the daemon on your remote heavy-compute node. The daemon manages the worker pool, state machine, cache, and artifact storage.

```console
$ packetsd
> INFO starting packetsd version=v1.0.0
> INFO grpc server listening addr=:50051
```

### Check Job Status & Cache

If a job is running asynchronously (e.g., dispatched to a CI provider), you can check its status or clear the cache:

```console
$ packets status <job_id>
$ packets cache clear /path/to/your/project
```

## Configuration

Set up your environment variables either locally or in `~/.packets/config.env` for global availability:

```env
# Tailscale Server Config
TAILSCALE_AUTH_KEY=your_key
SCHEDULER_GRPC_PORT=50051

# MinIO Storage (Artifact Cache)
MINIO_ENDPOINT=localhost:9000
MINIO_ACCESS_KEY=admin
MINIO_SECRET_KEY=admin123
B2_BUCKET_NAME=packets-cache

# NATS Message Queue
NATS_URL=nats://localhost:4222

# GitHub Actions (For Direct-CI Mode / Failover)
DIRECT_CI_MODE=true
GITHUB_ACTIONS_TOKEN=ghp_your_token
GITHUB_ACTIONS_REPO=username/repo
```

## Documentation

For a detailed, step-by-step installation guide (including how to set up GitHub Actions failover), see the [Setup Guide](guide.md).

## Limitations

Beside the known features, there are a few limitations currently:
- **Tailscale Required**: The daemon currently mandates Tailscale for zero-trust network verification.
- **MacOS Fallbacks**: Native remote MacOS execution requires a MacOS runner on GitHub Actions (handled via failover).

## Contributing

Contributions are welcome! Please open an issue or submit a PR for any bugs or enhancements.

## License

[MIT](LICENSE)
