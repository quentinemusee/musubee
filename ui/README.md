# ui — React + TypeScript interface

A single interface codebase for every platform, built with Vite and displayed by Electron (desktop) and Capacitor (mobile).

**Status: a minimal interface (T1.5).** Add an account on the echo network, list conversations, read and send messages, shown by the Electron app ([`apps/desktop`](../apps/desktop/)). Its state store is provisional: state management and the virtualized message list are decided later, in their own ADR (see [`docs/TASKS.md`](../docs/TASKS.md)).

## Layout

| Path | Role |
|---|---|
| `src/shell.ts` | `window.musubee`, what a shell (Electron, later Capacitor) gives the interface: `core.call`, `core.onEvent`, `core.onStatus` |
| `src/store.ts` | `Store`: the interface's state, read from the core and kept up to date by its events; reloads everything after a (re)start of the core or `resync.required` |
| `src/app/` | The React components: status line, accounts, conversation list, thread with its composer, the add-account flow |
| `src/styles.css` | Design tokens (CSS custom properties), light and dark |
| `index.html` | With a content security policy: no inline script or style, no network |

## Core API (`src/core-api`)

The contract between the UI and the Go core is the JSON Schema [`core/api/schema/core-api.schema.json`](../core/api/schema/core-api.schema.json) ([ADR 0012](../docs/ADR/0012-core-api-contract.md)).

- `types.gen.ts`: generated from the schema by `go generate ./core/api` (from the repository root). Never edit it by hand; a Go test fails when it is stale.
- `client.ts`: `CoreClient`, a typed client over any `Transport` (a function that sends one JSON request and resolves with the JSON response). `hello()` checks the API's major version; `parseEvent()` returns `null` for event types of a newer core.
- `contract.test.ts`: Ajv checks the shared examples (`core/api/schema/examples.json`) and one example per command and event written against the generated types. The Go side checks the same schema with another validator.

## Principles

- The UI knows **only** the domain types (`Account`, `Conversation`, `Message`, `Person`), generated from the core API contract (T1.4). No Matrix concept (room, event, homeserver, MXID) appears in the code or in displayed text.
- The UI always states honestly the connection mode of an account: "on-device" or "hosted bridge".
- Customization through design tokens (themes, layouts).
- State management and the virtualized message list will be decided in an ADR, with measurements on low-end Android.

## Commands

Verified in T1.4 and T1.5 (Node 24, npm 11):

```
npm ci
npm test               (Vitest: contract and unit tests)
npm run typecheck      (tsc --noEmit)
npm run lint           (oxlint; typescript-eslint does not support TypeScript 7 yet, ADR 0013)
npm run build          (Vite, into dist/; relative paths and no inlined assets, for the app:// protocol and its policy)
```

## Skills to re-read before coding

`vercel-react-best-practices` (mainly the client-side rules), `vercel-composition-patterns`, `web-design-guidelines`; for tests: `webapp-testing`, `playwright-testing`.

## Mandatory header

<!-- REUSE-IgnoreStart -->

```ts
// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later
```

<!-- REUSE-IgnoreEnd -->
