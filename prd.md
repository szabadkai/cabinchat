# CabinChat – Compact PRD

## Overview

CabinChat is a zero‑internet, local‑only chatroom that runs as a single executable on macOS and Windows. Users run the binary, automatically discover a nearby room, and join a shared chatroom. If no room exists, one user becomes the host. The product is intentionally ephemeral and simple.

This is a novelty / utility, not a scalable messaging platform.

---

## Goals

* Enable a shared chatroom without internet access
* Work reliably on planes and similar environments
* Require no accounts, setup, or configuration
* Run as a single self‑contained executable
* Support macOS and Windows

---

## Non‑Goals

* Mobile support
* Multiple rooms
* Message history or persistence
* Mesh networking or host migration
* Media sharing

---

## Target Users

* Tech‑savvy users with laptops
* Friends traveling together
* Users comfortable running a binary from Terminal / Command Prompt

---

## Platforms

* macOS (arm64, amd64)
* Windows (amd64)

---

## User Experience

### Startup Flow

1. User runs `cabinchat`
2. App searches for nearby rooms
3. If a room is found → prompt to join
4. If none found → prompt to host
5. User enters nickname
6. Chatroom opens

### Chatroom UX

* Single shared room
* Text messages only
* Join/leave system messages
* Participant count shown implicitly via messages
* Room ends when host exits

---

## Success Criteria

* Two or more laptops can chat without internet
* Time‑to‑chat under 10 seconds
* No manual IP entry
* Works on macOS and Windows

---

# CabinChat – Short Design Document

## Architecture

### Network Topology

* Host‑based model
* One device acts as room host
* All clients connect directly to host
* All messages are relayed through host

If the host exits, the session ends.

---

## Discovery

### Primary

* mDNS / Bonjour
* Service: `_cabinchat._tcp.local.`
* Fixed TCP port (e.g. 7777)

### Fallback (Windows)

* Local subnet TCP scan on the fixed port
* Short timeouts to avoid delay

Discovery timeout: ~3 seconds total before fallback or host prompt.

---

## Transport

* TCP sockets
* One persistent connection per client
* Line‑delimited messages

TLS may be added later; MVP uses plaintext TCP.

---

## Protocol

### Message Format

* JSON, newline‑delimited

Examples:

* `{ "type": "join", "nick": "Levi" }`
* `{ "type": "msg", "nick": "Levi", "text": "anyone bored?" }`
* `{ "type": "system", "text": "Alex joined" }`

---

## Concurrency Model

### Host

* TCP listener
* Goroutine per connected client
* Central broadcast loop

### Client

* One goroutine for stdin
* One goroutine for socket read

---

## Error Handling

* Discovery failure → prompt to host
* Connection drop → exit with message
* Host termination → clients exit cleanly

No retries or state recovery.

---

## Build & Distribution

* Written in Go
* Built as static binaries per OS`
* Distributed via AirDrop, USB, or file share

---

## Design Principles

* Prefer reliability over elegance
* Cheat when necessary (fallbacks are fine)
* Keep scope intentionally small
* Optimize for "it just worked" moments
