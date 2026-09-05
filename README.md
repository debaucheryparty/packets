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

packets is an event-driven, remote build platform for compiling applications in any programming language. At a high level, it provides a few major components:

* A multithreaded, scalable build task **scheduler** (`packetsd`).
* An **artifact cache** backed by the S3/MinIO compatible object storage.
* An instantaneous **CLI worker** (`packets`) that streams logs over NATS.

These components provide the runtime necessary for building large-scale applications without relying on a single laptop's processing power.

## Example

A basic remote build execution using packets.

Install the pre-compiled executable via our setup script:

```bash
curl -fsSL https://raw.githubusercontent.com/debaucheryparty/packets/main/scripts/install.sh | bash
```

Alternatively, if you prefer building from source:

```bash
go install github.com/debaucheryparty/packets/cmd/packets@latest
```
Then, inside any project directory (e.g. Go, Rust, C++):

```console
$ packets build .
> Detecting toolchain...
> Cache miss. Dispatching to remote server...
> Compiling...
> Done in 1.2s.
```

If you prefer to bypass the self-hosted daemon and run a "Serverless" build straight to GitHub Actions (great for macOS/iOS builds):

```console
$ export DIRECT_CI_MODE=true
$ packets build .
> Direct-CI mode enabled, bypassing scheduler...
> Direct CI job dispatched successfully.
```

More setup examples and deployment architectures can be found in our [Setup Guide](guide.md). 

## Getting Help

First, see if the answer to your question can be found in our [Documentation](guide.md) or [API Docs](https://pkg.go.dev/github.com/debaucheryparty/packets). If you still need help, feel free to open a GitHub Issue or Discussion!

## License

This project is licensed under the [MIT license](LICENSE).
