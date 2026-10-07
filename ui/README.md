# ui — React + TypeScript interface

A single interface codebase for every platform, built with Vite and displayed by Electron (desktop) and Capacitor (mobile).

**Status: empty.** The first code will arrive with the Electron shell (T1.5), see [`docs/TASKS.md`](../docs/TASKS.md). No `package.json` is created before then.

## Principles

- The UI knows **only** the domain types (`Account`, `Conversation`, `Message`, `Person`), generated from the core API contract (T1.4). No Matrix concept (room, event, homeserver, MXID) appears in the code or in displayed text.
- The UI always states honestly the connection mode of an account: "on-device" or "hosted bridge".
- Customization through design tokens (themes, layouts).
- State management and the virtualized message list will be decided in an ADR, with measurements on low-end Android.

## Commands (to be confirmed in T1.5)

```
npm test
npm run lint
npm run typecheck
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
