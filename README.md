# Packets

[![release](https://img.shields.io/github/v/release/debaucheryparty/packets.svg?label=latest)](https://github.com/debaucheryparty/packets/releases)
[![Build Status](https://github.com/debaucheryparty/packets/actions/workflows/ci.yml/badge.svg)](https://github.com/debaucheryparty/packets/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/debaucheryparty/packets.svg)](https://pkg.go.dev/github.com/debaucheryparty/packets)

Packets is a remote build execution and caching system.
Basically, it takes the heavy lifting of compiling code off your local machine and runs it on a remote server or a CI runner instead. It hooks right into your normal workflow without you having to change how you work in your IDE.

## Features

* Complete support for **30+ programming languages** out of the box
* Written in pure Go, so it runs anywhere without messy dependencies
* **Git-Aware Caching**: Uses `git status` and file hashing to smartly cache builds. If your teammate already built it, you just download the binary instantly.
* **Zero-Config Fallback**: If your remote server goes down, it just quietly falls back to building locally on your machine.
* **Real-Time Logs**: Streams compilation logs right to your terminal as if it was building locally.
* **Direct-CI Mode**: Skip the server entirely and just dispatch your builds straight to GitHub Actions (great for things like macOS/Swift builds when you're on Windows/Linux).
* Secure by default, backing onto Tailscale for network verification.

## Install

### Command-line executable

```bash
go install github.com/debaucheryparty/packets/cmd/packets@latest
```

### Pre-compiled binaries

You can just grab the latest binaries for Mac or Linux straight from our [GitHub Releases](https://github.com/debaucheryparty/packets/releases).

### CI Integration

If you want to use this in a serverless way (dispatching directly to GitHub actions), just set the environment variable:

```bash
export DIRECT_CI_MODE=true
packets build .
```

## Usage

### Using the CLI builder

Run the CLI in your project folder. It'll automatically figure out what language you're using (by looking for stuff like `go.mod` or `Cargo.toml`) and send the build off.

```console
$ packets build /path/to/your/project
> Detecting toolchain...
> Cache miss. Dispatching to remote server...
> Compiling...
> Done in 1.2s.
```

Need to pass some extra flags to your compiler? Just add them at the end:

```console
$ packets build /path/to/your/project -- -v -race
```

### Running the remote Daemon

Start the daemon on the beefy remote server you want to use for compiling. It handles all the worker queues, caching, and storage.

```console
$ packetsd
> INFO starting packetsd version=v1.0.0
> INFO grpc server listening addr=:50051
```

### Checking Job Status & Cache

If you dispatched a job to a CI provider and want to see how it's doing, or if you just want to clear your cache:

```console
$ packets status <job_id>
$ packets cache clear /path/to/your/project
```

## Documentation

Check out our [Setup Guide](guide.md) if you want a step-by-step walkthrough on how to get everything installed, including the GitHub Actions failover stuff.

## Limitations

Besides the known bugs, there are a few things to keep in mind:
- **Tailscale is required**: The daemon currently demands Tailscale to make sure your network is secure.
- **MacOS Fallbacks**: If you want to do remote macOS builds, you'll need to rely on the GitHub Actions fallback since we don't natively manage macOS workers yet.

## Contributing

Contributions are totally welcome! Feel free to open an issue or drop a PR if you find bugs or want to add something cool.

## License

[MIT](LICENSE)
