# ui — React + TypeScript interface

A single interface codebase for every platform, built with Vite and displayed by Electron (desktop) and Capacitor (mobile).

**Status: a minimal interface (T1.5, T1.6).** Add an account on any network the core offers (echo, and Telegram since T1.6), log out of it after a confirmation, list conversations, read and send messages, shown by the Electron app ([`apps/desktop`](../apps/desktop/)). Tokens and passwords are typed into password fields. Since T2.4 ([ADR 0020](../docs/ADR/0020-ui-state-and-thread-list.md)) the thread is a virtualized list that reads older messages as the user scrolls up; the state stays in the app's own store.

## Layout

| Path | Role |
|---|---|
| `src/shell.ts` | `window.musubee`, what a shell (Electron, later Capacitor) gives the interface: `core.call`, `core.onEvent`, `core.onStatus` |
| `src/store.ts` | `Store`: the interface's state, read from the core and kept up to date by its events; reloads everything after a (re)start of the core or `resync.required`; reads older messages page by page (`loadOlder`) |
| `src/app/` | The React components: status line, accounts, conversation list, thread with its composer, the add-account flow; `MessageList` is the only user of the list library (virtua) |
| `src/styles.css` | Design tokens (CSS custom properties), light and dark |
| `bench/` | The thread list benchmark of [ADR 0020](../docs/ADR/0020-ui-state-and-thread-list.md): a page showing one candidate list (or the app's) with synthetic messages, its driver (`run.mjs`) for Electron and the Android app's WebView, the regression test (`--check`) and the results |
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
- State: the app's own store over `useSyncExternalStore` (no state library); the thread: virtua behind `MessageList`, guarded by a performance regression test ([ADR 0020](../docs/ADR/0020-ui-state-and-thread-list.md)).

## Commands

Verified in T1.4 and T1.5 (Node 24, npm 11):

```
npm ci
npm test               (Vitest: contract and unit tests)
npm run typecheck      (tsc --noEmit)
npm run lint           (oxlint; typescript-eslint does not support TypeScript 7 yet, ADR 0013)
npm run build          (Vite, into dist/; relative paths and no inlined assets, for the app:// protocol and its policy)
```

Verified in T2.4: `npm run bench:build` and `npm run bench:check` (see below).

## Thread list benchmark

The page `bench/index.html` (built into `dist-bench/`) shows one list (`?list=`: `plain`, `content-visibility`, `react-virtuoso`, `tanstack-virtual`, `virtua`, or `app`, the app's `MessageList`) of `n` synthetic messages (`?n=`) and exposes its measurements as `window.bench`: time to show the newest message, DOM nodes and JS heap, a status update, a new message followed at the bottom, a fast fling (frame intervals, blank areas), 200 older messages added above (time, and how far the message being read moved), and for `app`, older messages read by the list itself as the user scrolls up. `bench/run.mjs` drives it with Playwright, with the CPU throttled through the DevTools protocol, and prints one JSON line per run then the medians. Results of [ADR 0020](../docs/ADR/0020-ui-state-and-thread-list.md): `bench/results/`.

```
npm run bench:build
node bench/run.mjs --target electron --n 1000,5000,20000 --throttle 1,4 --runs 3
npm run bench:check          (the regression test run by CI: the app's list, 5000 messages, CPU x4)
```

`--target electron` uses the Electron of `apps/desktop` (`cd apps/desktop && npm ci` first) and shows a small window: a hidden window gets one frame per second. On Linux without a display, run it under `xvfb-run`.

For `--target android`, put the page in the debug app (it is git-ignored there), then run with `adb` in `PATH`. The Android 11 emulator image has WebView 83, too old for the default build: build for it with `MUSUBEE_BENCH_TARGET=chrome83` (`bench/legacy.ts` adds the few missing functions). With a single device attached, Gradle installs on it; otherwise set `ANDROID_SERIAL`.

```
MUSUBEE_BENCH_TARGET=chrome83 npm run bench:build
cp -r dist-bench ../apps/mobile/www/bench
cd ../apps/mobile && npx cap sync android && cd android && ANDROID_SERIAL=emulator-5554 ./gradlew :app:installDebug -Pmusubee.abis=x86
cd ../../../ui && ANDROID_SERIAL=emulator-5554 node bench/run.mjs --target android --n 1000,5000
rm -r ../apps/mobile/www/bench        (and sync again before building the app for anything else)
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
