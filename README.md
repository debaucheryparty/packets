# Packets

Packets is a remote development platform that lets you use another machine for builds, tests, development environments, android devices, services, and other resource-heavy tasks. your local machine stays lightweight while packets handles the heavy work on remote machines.

## Description

I do most of my coding on an older laptop with only 8 GB of RAM. Whenever I try to compile Android apps with Gradle, build Rust crates, or run test matrices, my whole machine freezes and lags.

I built Packets so my laptop only has to be an editor. All the heavy compilation, container tasks, and testing happen on an old desktop on my home network or a remote VPS.

Packets only syncs the specific file chunks you edited, runs the build or tests remotely, streams output live to your terminal, brings the built binaries back to your folder, and keeps caches warm across runs so incremental builds stay fast.

### Screenshots

**Peer fleet status & queue dashboard**
![Fleet status](./archive/tui-fleet-status.png)

**Remote Android emulator session**
![Remote Android session](./archive/android-remote-session.png)

**Artifact download after remote build**
![Artifact extraction](./archive/artifact-download.png)

## Getting Started

### Dependencies

* Linux, macOS, or Windows
* Go 1.22 or higher (if building from source)
* A remote machine, desktop, or VPS reachable over the network

### Installing

Install the pre-built binary:

```bash
curl -fsSL https://raw.githubusercontent.com/debaucheryparty/packets/main/scripts/install.sh | bash
```

Or install directly with Go:

```bash
go install github.com/debaucheryparty/packets/cmd/packets@latest
```

Or build from source:

```bash
git clone https://github.com/debaucheryparty/packets.git
cd packets
go build -o packets ./cmd/packets
go build -o packetsd ./cmd/packetsd
```

### Setup

1. **Start the daemon** on your remote machine (VPS or desktop):

```bash
export SCHEDULER_GRPC_PORT=50051
./packetsd
```

2. **Point your local client** to the remote host:

```bash
export PACKETS_SERVER_ADDR="your-remote-host:50051"
packets status
```

3. **Initialize your project**:

```bash
cd your-project
packets init
```

### Executing the program

* Run a remote build (auto-detects project type, uploads changes, and downloads outputs):

```bash
packets build --wait
```

* Run tests on the remote machine:

```bash
packets test
```

* Keep build caches warm between runs with a persistent subspace:

```bash
packets subspace create --project my-app --worker vps-worker
packets build --subspace sub-xxxxxx --wait
```

* Run any command or open a shell inside the remote workspace:

```bash
packets exec "cargo check"
packets subspace shell sub-xxxxxx
```

* Connect an AI assistant (Claude Desktop, Cursor) via MCP:

```json
{
  "mcpServers": {
    "packets": {
      "command": "packets",
      "args": ["mcp"],
      "env": {
        "PACKETS_SERVER_ADDR": "vps.example.com:50051"
      }
    }
  }
}
```

## AI Usage

I used AI as a development assistant throughout packets. I used it for debugging, exploring implementation approaches, understanding errors, reviewing code, and helping with development tasks. I also used AI while developing parts of the mcp/ai agent integration. I reviewed, tested, and made the final implementation decisions myself.

## Help

Check connection status and cluster health:

```bash
packets status
```

View all available commands and flags:

```bash
packets --help
```

Detailed guides and architecture notes are available in the [docs/](./docs/README.md) folder.

## License

This project is licensed under the MIT License -> see the [LICENSE](LICENSE) file for details.
