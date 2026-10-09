# ADR 0013 — The desktop shell: Electron with the core as a supervised child process

- **Status**: accepted
- **Date**: 2026-10-09
- **Task**: T1.5
- **Deciders**: Quentin Raimbaud (maintainer), with the design assistant

## Context

T1.5 delivers the first desktop app: an Electron window with a minimal interface, the core as a sidecar or a library, `contextIsolation` enabled and validated IPC, tested by a Playwright journey "open the app, send a dummy message".

ADR 0012 already chose the transport: on the desktop, the core runs in a child process and speaks newline-delimited JSON over stdio, and the shared library stays as a fallback if T1.5 finds a blocker. It left these questions to T1.5:

- Does the transport cost measured from plain Node hold inside Electron, on Linux and macOS too?
- Is koffi (the fallback's binding) needed?
- Can a child process be signed and notarised inside the macOS app, and does it start fast enough?

This ADR records how the shell is built: process model, how the interface is loaded, how IPC is checked, how the core is supervised, and the lint tools that came with it.

Constraints:

- **Electron's security checklist** (`CLAUDE.md` §6.8): context isolation, sandboxed renderers, no Node.js in the page, IPC validated in the main process, no navigation to untrusted content.
- **No Matrix type crosses the contract** (ADR 0012): the shell passes JSON through and never interprets the domain.
- **One codebase for the interface** (ADR 0001): `ui/` must also run in Capacitor later, so the interface talks to an abstract `window.musubee.core` and never to Electron.
- **No message content in logs** (`CLAUDE.md` §6.8): the supervisor forwards the core's standard error, which carries the core's logs, never message bodies.

## Options considered

### Where the core runs

#### A. A child process over stdio, supervised by the main process (chosen, as ADR 0012 planned)

`core/cmd/musubee-core` is the core as a program. It takes `-data DIR` and `-log-level`, serves `core/api/stream` on its standard input and output, and exits when its input ends. That last rule means a crashed or killed Electron never leaves an orphaned core.

- Pros: everything ADR 0012 listed. A Go fatal error kills only the core, which is restarted. There is no Go runtime inside Electron's main process. The pipes are private to the parent.
- Cons: one more executable to ship and sign (E3.2). The supervisor is code of our own: restarts, pending requests, stop.

#### B. The shared library in the main process with koffi (the fallback)

- Pro: no second executable.
- Cons: a crash of the core takes every window with it, and the library can never be unloaded (ADR 0010). **Not needed:** nothing in T1.5 blocked option A.

#### C. Electron's `utilityProcess`

Electron's `utilityProcess` runs a Node.js child with a `MessagePort` to the main process.

- Con: it runs JavaScript, not a Go executable. Hosting the Go library inside it with koffi would combine the cons of A (two processes) and B (a native addon).

### How the interface is loaded

#### A. A privileged custom scheme, `app://musubee/` (chosen)

The scheme is registered as `standard` and `secure` before the app is ready, and `protocol.handle` serves the built interface from `resources/ui/`.

- Pros: a real origin, so the IPC handlers can check it. A content security policy on every response, as an HTTP header (Electron honours it). No port open to other local processes. Paths are confined to the interface's directory.
- Con: a little code of our own: MIME types and path checks. Unit tests cover traversal attempts.

#### B. `file://`

- Cons: every local file shares one origin, so the sender check has nothing to compare against. A content security policy can only come from a `<meta>` tag, and `frame-ancestors` cannot be set there. Electron's security checklist advises against it.

#### C. A local HTTP server

- Con: a loopback port that every local process can reach, as with the TCP transport rejected in ADR 0012.

### Linting the TypeScript

#### A. oxlint (chosen)

- Pros: it reads TypeScript and JSX without the TypeScript compiler. It has ports of the rules we want (ESLint core, typescript, react, react-hooks, unicorn, import, jsx-a11y). It is MIT-licensed and fast.
- Con: type-aware rules (for example floating promises) are not used. `tsc` with `strict`, `exactOptionalPropertyTypes` and `noUncheckedIndexedAccess` covers part of that ground.

#### B. ESLint with typescript-eslint

- Con: **blocked.** The project uses TypeScript 7.0. typescript-eslint 8.71.1, the latest release, declares `typescript >=4.8.4 <6.1.0` as its peer dependency.

#### C. Biome

- **Not trialled.** oxlint covered the rule sets above. Revisit if oxlint falls behind.

## Decision

1. **Process model:** the core is a child process (`core/cmd/musubee-core`), started by Electron's main process with `-data <userData>/core`. The renderer never talks to the core directly; every call goes renderer → preload → IPC → main → core.
2. **Supervisor (`apps/desktop/src/core-process.ts`):**
   - **Request IDs.** Every request gets a fresh ID of the supervisor's, and the response gets the caller's ID back. Two windows, or a window that reloaded, can never receive each other's responses.
   - **Readiness.** The core is `ready` when it answers a `core.hello` probe. Start-up time is measured from spawn to that answer.
   - **Unexpected exits.** On an unexpected exit, pending requests are answered with the API's `closed` error, the status becomes `restarting`, and the core is started again after `min(250 × 2^(n−1), 30 000)` ms. After more than five crashes in 60 seconds, the status becomes `failed`, with a reason the interface shows.
   - **Stop.** `stop()` closes the core's standard input, so the core exits on its own and closes its database. After 10 seconds it is killed. The app stops the core before it quits.
   - **Status changes are numbered**, so a window that asks for the status cannot overwrite a newer change with an older answer.
3. **IPC (`ipc.ts`, `main.ts`, `preload.cts`):**
   - The preload exposes `window.musubee.core` with `call(request: string)`, `onEvent` and `onStatus`, and nothing else; never `ipcRenderer`.
   - The main process answers only the main frame of a window it created, with the origin `app://musubee`.
   - Each request must be a string of at most 1 MiB, or the call is rejected.
   - Ajv 2020 checks each request against the core API schema: first the request envelope, then the params of its command. A refused request gets the API's own error (`invalid_request`, `unknown_command`, `invalid_params`) and never reaches the core.
4. **Window and session hardening (`main.ts`):**
   - `contextIsolation`, `sandbox`, `nodeIntegration: false` and `webSecurity` are set on every window; developer tools only when the app is not packaged.
   - Navigation, redirects, new windows and `<webview>` are refused for every web contents; every permission request and check is denied.
   - The content security policy is `default-src 'none'`, scripts and styles from the app only, no `connect-src` and `frame-ancestors 'none'`. The same policy is also in the `<meta>` tag of `ui/index.html`.
   - **Single instance:** two cores on one SQLite database would corrupt it, so a second launch focuses the first window.
   - A renderer that crashes is reloaded; the core keeps running.
5. **Interface:** `ui/` gains a minimal React 19 interface (accounts, conversation list, thread, the add-account flow) over `window.musubee.core`. Its store is provisional: state management and the virtualized list remain the subject of their own ADR (`CLAUDE.md` §4 question 6). Vite builds with relative paths and no inlined assets, so that the policy holds.
6. **Build:** `apps/desktop/scripts/build.mjs` builds the core with `CGO_ENABLED=0` (pure Go, no C toolchain needed), the interface, and the main process with `tsc`. Packaging, signing and updates are E3.2.
7. **Lint:** oxlint for `ui/` and `apps/desktop/`. For Go, golangci-lint 2.14 (`.golangci.yml`) runs the standard linters plus gosec, errorlint, bodyclose, sqlclosecheck, rowserrcheck, noctx, misspell, unconvert and nolintlint. gosec and noctx are fully on for the shipped core; tests and development tools skip only the findings that are their purpose.
8. **CI:** a desktop job on Linux, Windows and macOS runs, in order:
   - type check, lint and unit tests (the supervisor's tests run the real core);
   - the build;
   - the Playwright tests.

   On Linux they run under Xvfb, and `kernel.apparmor_restrict_unprivileged_userns=0` lets Chromium's sandbox start, rather than turning the sandbox off.

## Measurements

The end-to-end test `measure the start-up of the core and the cost of a call through the app` runs the same procedure three ways, in one app with an echo account:

- **main**: from the main process, through the supervisor and the core only;
- **renderer**: from the page, through the preload, the IPC, the validation and the main process, with the interface showing the Instant Echo conversation;
- **renderer, thread hidden**: the same, with the conversation off screen (the store still applies every event, but React no longer renders the thread).

Each way runs 200 warm-up and 2,000 timed `debug.ping` calls with a 1 KiB payload, then 10 warm-up and 50 timed round trips: `messages.send` to Instant Echo, until its `message.added` event arrives. Start-up is the time from spawning the core to its answer to `core.hello`, over four launches of the app on one data directory.

**Command:** `cd apps/desktop && npm run build && npx playwright test -g measure`. The results are in `test-results/*/measurements.json`; CI uploads them.

**Versions:** Electron 44.7.0 (Chromium 152.0.7977.130, Node 24.21.0), Go 1.27.1. Date: 2026-10-09. GitHub's runners: `ubuntu-latest` (x64), `windows-latest` (x64), `macos-latest` (arm64). Local: Windows 11, Intel Core i7-9750H.

| Machine | Ping, main | Ping, renderer | Round trip p50: main / renderer thread hidden / renderer | Round trip max (renderer) | Core start-up, 4 launches |
|---|---|---|---|---|---|
| CI Linux | 132 µs | 359 µs | 3.0 / 3.2 / 4.6 ms | 9.5 ms | 72, 65, 60, 60 ms |
| CI macOS | 135 µs | 257 µs | 3.1 / 4.1 / 5.7 ms | 14.6 ms | 237, 193, 259, 228 ms |
| CI Windows | 186 µs | 422 µs | 4.3 / 4.9 / 6.8 ms | 65 ms | 424, 247, 214, 185 ms |
| Local Windows | 162 µs | 479 µs | 4.2 / 4.3 / 11.2 ms | 16.9 ms | 311, 242, 233, 205 ms |

For comparison, ADR 0012 measured from plain Node on the same local machine: a ping of 124 µs and a round trip of 4.27 ms over stdio.

Reading:

- **Electron's main process reaches the core as fast as plain Node did** (ADR 0012's unknown, now answered on the three systems). The transport ranking measured there carries over.
- **The renderer's IPC adds about 0.12 to 0.32 ms per call** (ping, renderer minus main). That is twice the core's own ping, but still far below a frame (16 ms).
- **Rendering the open thread is the largest variable cost of a round trip.** One message sent makes the core send several events (the message, its status, the echo, the conversation). On each event the provisional interface re-renders the whole thread, with up to 120 messages in this test. That adds 1.4 to 1.9 ms on the CI runners and 6.9 ms on the local laptop. The list virtualization and state management ADR starts from this test.
- **Windows has outliers** (a round trip of 65 to 105 ms, one per run) in the main process too, so they come from the system or the core, not from the IPC. Not investigated.
- **Start-up is 60 to 72 ms on Linux and about 200 to 260 ms on macOS and Windows,** with a first launch up to 424 ms on Windows. The core and the window start in parallel, and the interface shows "Starting…" until the core answers; when the window appears relative to the core was not measured.

An earlier local run, with only the renderer measured, gave ping 419 µs and round trip p50 10 ms; the split above explains it.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| The journey "open the app, add an account, send a message, get its echo, restart the app, find both messages" works | **verified** on Linux, Windows and macOS (CI) and locally on Windows | `e2e/app.spec.ts`, PR CI runs 2026-10-09 |
| Closing the app ends the core process | **verified** (the test checks the PID is gone) | same |
| The page has no `require`, `process` or `module`; `window.musubee` has exactly `core` with `call`, `onEvent` and `onStatus` | **verified** on the three systems | same |
| The renderer is in Chromium's sandbox | **verified** on Windows and macOS (`app.getAppMetrics()` reports `sandboxed`). **Assumed** on Linux: Electron does not report the flag there; the app runs with `sandbox: true`, and the AppArmor setting is needed for Chromium's sandbox to start | Electron `ProcessMetric` docs |
| Inline scripts are blocked, and `window.open` and navigation are refused | **verified** (behavioural checks in the e2e test) | same |
| The IPC refuses malformed requests with the API's error codes, and non-string or larger than 1 MiB payloads | **verified** (unit tests on the real schema; e2e from the page) | `test/ipc.test.ts`, `e2e/app.spec.ts` |
| The supervisor restarts a killed core, answers its pending requests with `closed`, gives up after repeated crashes, and stops cleanly | **verified** with the real core binary | `test/core-process.test.ts`, e2e crash test |
| Electron's main process reaches the core as fast as plain Node; the IPC adds 0.1 to 0.3 ms | **verified** (Measurements) | same |
| koffi in Electron's main process | **not needed**: option A has no blocker | — |
| The child executable can be signed and notarised inside the macOS app bundle; Windows antivirus programs accept an unsigned helper | **unknown**: packaging is E3.2. Electron apps commonly ship helper executables, so **assumed** feasible | E3.2 |
| The core's start-up on a slow machine (HDD, low-end laptop) | **unknown**: measured on CI runners and one laptop only | — |
| The Windows round-trip outliers (about 100 ms) | **unknown** cause; they also occur without the renderer | — |

## Consequences

- The interface depends only on `window.musubee.core`. The Capacitor shell can provide the same object over the in-process binding (ADR 0011).
- A crash of the core is visible to the user only as "restarting" for a moment. The interface reloads its state afterwards.
- Every new command needs no change in the shell: the schema drives the validation. A command the schema does not know is refused before the core sees it, so the schema copied into `resources/` must match the core built with it. The build copies both from the same commit.
- The e2e tests open real windows on the developer's screen. They are run sparingly locally and always in CI.
- golangci-lint is now part of CI. New `//nolint` directives need a reason (`nolintlint`).
- **Revisit if:** signing or notarising the helper fails in E3.2 (then option B, the shared library with koffi); start-up becomes noticeable on real machines; a feature needs large binary data between the interface and the core (pass file paths first); typescript-eslint supports TypeScript 7 and type-aware rules become worth their cost.

## Sources

Accessed 2026-10-09.

- Electron security checklist: https://www.electronjs.org/docs/latest/tutorial/security
- Electron `protocol.registerSchemesAsPrivileged` and `protocol.handle`: https://www.electronjs.org/docs/latest/api/protocol
- Electron process sandboxing and context isolation: https://www.electronjs.org/docs/latest/tutorial/sandbox, https://www.electronjs.org/docs/latest/tutorial/context-isolation
- Electron `app.getAppMetrics()` and `ProcessMetric.sandboxed` (Windows and macOS only): https://www.electronjs.org/docs/latest/api/structures/process-metric
- Electron `utilityProcess`: https://www.electronjs.org/docs/latest/api/utility-process
- Playwright's Electron support (`_electron.launch`): https://playwright.dev/docs/api/class-electron
- typescript-eslint 8.71.1 peer dependencies (`npm view typescript-eslint peerDependencies`): `typescript >=4.8.4 <6.1.0`
- oxlint 1.87: https://oxc.rs/docs/guide/usage/linter
- golangci-lint 2.14.0 configuration (version 2, exclusion presets): https://golangci-lint.run/docs/configuration/
- Ubuntu 24.04's restriction of unprivileged user namespaces (`kernel.apparmor_restrict_unprivileged_userns`): https://ubuntu.com/blog/ubuntu-23-10-restricted-unprivileged-user-namespaces
- ADR 0010 (C shared library), ADR 0011 (Android), ADR 0012 (core API contract and transports)
