# CabinChat

A zero-internet, local-only chatroom that runs as a single Go executable. Users run the binary, automatically discover a nearby room via mDNS/Bonjour, and join a shared chatroom.

## Features

- **Zero configuration** - Just run it and chat
- **Auto-discovery** - Finds rooms via mDNS (with Windows fallback)
- **Cross-platform** - macOS and Windows support
- **Single binary** - No dependencies at runtime
- **Ephemeral** - No persistence, history, or accounts

## Quick Start

```bash
# Build
go build -o cabinchat .

# Run
./cabinchat
```

## macOS: Removing Quarantine

Downloaded binaries are blocked by Gatekeeper. To fix:

```bash
xattr -d com.apple.quarantine cabinchat-darwin-arm64
chmod +x cabinchat-darwin-arm64
```

## Building for All Platforms

```bash
# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o cabinchat-darwin-arm64 .

# macOS (Intel)
GOOS=darwin GOARCH=amd64 go build -o cabinchat-darwin-amd64 .

# Windows
GOOS=windows GOARCH=amd64 go build -o cabinchat-windows-amd64.exe .
```

## Usage

1. Run `cabinchat` on your machine
2. If a room is found, you'll be prompted to join
3. If no room exists, you'll be prompted to host one
4. Enter a nickname and start chatting
5. Press `Ctrl+C` to exit

## Options

```
-nick string   Set your nickname (skip prompt)
-sound         Enable sound notifications (default: true)
-port int      Port to use for hosting/connecting (default: 7777)
```

Examples:
```bash
# Quick start with nickname
./cabinchat -nick Alice

# Disable sound notifications
./cabinchat -sound=false

# Use custom port
./cabinchat -port 8888
```

## How It Works

```
┌─────────────┐     mDNS      ┌─────────────┐
│   Client    │ ───────────►  │    Host     │
│             │  discover     │   (TCP)     │
└─────────────┘               └─────────────┘
       │                             │
       │         TCP:7777            │
       └─────────────────────────────┘
              join / messages
```

- One device hosts the room (TCP server on port 7777)
- Clients discover via mDNS (`_cabinchat._tcp.local.`)
- All messages flow through the host and are broadcast to all clients
- If the host exits, the room ends

## Protocol

Line-delimited JSON over TCP:

```json
{ "type": "join", "nick": "Alice" }
{ "type": "msg", "nick": "Alice", "text": "Hello!" }
{ "type": "system", "text": "Bob joined" }
{ "type": "leave", "nick": "Bob" }
```
