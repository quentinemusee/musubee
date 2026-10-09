# ui — React + TypeScript interface

A single interface codebase for every platform, built with Vite and displayed by Electron (desktop) and Capacitor (mobile).

**Status: the core API client only.** The interface itself arrives with the Electron shell (T1.5), see [`docs/TASKS.md`](../docs/TASKS.md). T1.4 created `package.json` for the contract tests: TypeScript, Vitest and Ajv, no runtime dependency yet.

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

Verified in T1.4 (Node 24, npm 11):

```
npm ci
npm test               (Vitest: contract and unit tests)
npm run typecheck      (tsc --noEmit)
```

To be added in T1.5: `npm run lint`, the Vite build.

## Skills to re-read before coding

`vercel-react-best-practices` (mainly the client-side rules), `vercel-composition-patterns`, `web-design-guidelines`; for tests: `webapp-testing`, `playwright-testing`.

## Mandatory header

<!-- REUSE-IgnoreStart -->

```ts
// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later
```

<!-- REUSE-IgnoreEnd -->
