# core — Go core

The Musubee core, written in Go. It runs **on the device** (a library embedded in Electron, Android and iOS) or **remotely** (hosted core).

**Status: empty.** The first code will arrive with the phase 1 spikes (T1.1, T1.2), see [`docs/TASKS.md`](../docs/TASKS.md). No `go.mod` is created before T1.1.

## Responsibilities

- **Domain**: `Account`, `Conversation`, `Message`, `Person`, and merging conversations per person. **No Matrix type leaves this layer towards the UI.**
- **Connectors**: `bridgev2` bridges from [mautrix-go](https://github.com/mautrix/go) (MPL-2.0), in "on-device" or "hosted bridge" mode.
- **Encryption**: Olm/Megolm in pure Go (`goolm` build tag, no cgo for libolm).
- **Storage**: local SQLite.
- **Local API**: commands and event streams towards the UI (contract defined in T1.4).

## Commands (to be confirmed in T1.1)

```
go test -race ./...
go vet ./...
golangci-lint run
```

## Skills to re-read before coding

`go`, `golang-testing`, `golang-concurrency`, `golang-safety`, `golang-security`, `golang-error-handling`, `golang-context`, `golang-project-layout`, `golang-continuous-integration` (see [`docs/SKILLS.md`](../docs/SKILLS.md)).

## Core-specific rules

<!-- REUSE-IgnoreStart -->

- Every `.go` file starts with:
  ```go
  // SPDX-FileCopyrightText: 2026 Quentin Raimbaud
  // SPDX-License-Identifier: AGPL-3.0-or-later
  ```

<!-- REUSE-IgnoreEnd -->

- Never log message content.
- Integration tests run against a real Synapse ([`infra/`](../infra/)), never mocks.
