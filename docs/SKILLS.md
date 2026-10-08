# Musubee skills — what, why, how

## Installation

From the repository root, with Node.js and git installed, run the variant for your shell:

```
powershell -ExecutionPolicy Bypass -File scripts/install-skills.ps1     # Windows PowerShell
scripts\install-skills.bat                                              # Windows cmd
sh scripts/install-skills.sh                                            # Linux, macOS, Git Bash
```

The three variants read the same list, `scripts/skills.txt`, and run the same `npx skills add` commands. Add `--dry-run` (all three variants; PowerShell also accepts `-DryRun`) to print the commands without running them.

Everything goes into `.claude/skills/` (project level). The `skills` CLI also writes `skills-lock.json` at the repository root (source, path and content hash of every installed skill); it is versioned, so an upstream change shows up as a diff after a reinstall. The scripts set `DISABLE_TELEMETRY=1`, see [ADR 0003](ADR/0003-third-party-skills-guardrails.md). Then restart the Claude Code session; check with `npx skills list`.

**Verified on 2026-10-07 (T0.2)** with `scripts/install-skills.ps1` on Windows: 38 skills installed, all with a `SKILL.md`, 38 entries in `skills-lock.json` (≈ 6.5 MB). The `.bat` and `.sh` variants are checked in dry-run mode by `scripts/tests/test_install_scripts.py` (they print the same commands).

The Flutter plugin (`dart-flutter`, user level) is no longer useful here; it is disabled **for this project only** in `.claude/settings.json`, so other projects keep it.

> Third-party skills are content written by others: **read each `SKILL.md` before enabling it**. We do not version their copies (see `.gitignore`); we version our own, prefixed with `musubee-`.

## Why `npx skills` and not `/plugin`
A *skill* is knowledge (a directory with a `SKILL.md`). A *plugin* is a package that can contain skills **plus** hooks, agents and MCP servers, which run code. Here we only need the knowledge, so we install the skills alone, in one place, versionable. If a useful MCP server shows up one day (driving a browser or a device in tests), we will install it as a plugin or through `.mcp.json`.

## Relevance to the tech stack

| Stack layer | Installed skills | Notes |
|---|---|---|
| **Go core** (mautrix-go, bridgev2) | `go`, `golang-testing`, `golang-concurrency`, `golang-safety`, `golang-security`, `golang-error-handling`, `golang-context`, `golang-project-layout`, `golang-continuous-integration` | Heart of the project, highly relevant. No skill knows mautrix-go: see "in-house skills". |
| **React/TypeScript interface** | `vercel-react-best-practices`, `vercel-composition-patterns`, `web-design-guidelines` | **Partially** relevant: many rules in `vercel-react-best-practices` target Next.js/server. Apply the client-side rules (re-renders, bundle size, lists). |
| **Capacitor mobile** | `capacitor-best-practices`, `capacitor-plugins`, `capacitor-testing`, `capacitor-push-notifications`, `capacitor-security`, `capacitor-offline-first`, `capacitor-performance`, `debugging-capacitor`, `ios-android-logs`, `safe-area-handling` | Highly relevant. None covers writing a native plugin embedding a Go core: to be documented in our in-house skills. |
| **Native iOS (plugin + notification extension)** | `swift-concurrency`, `swift-testing`, `background-processing`, `push-notifications`, `cryptokit`, `swift-security`, `ios-simulator`, `debugging-instruments`, `ios-memgraph-analysis`, `app-store-review` | **No SwiftUI skills on purpose**: the UI is web. `debugging-instruments` and `ios-memgraph-analysis` are used to measure NSE memory (task T1.7). |
| **Native Android (Kotlin shell)** | `claude-android-ninja` | Centered on Jetpack Compose, which we do not use. Consult only for foreground services, notifications, Gradle and tests. |
| **Electron desktop** | `electron` (community, Apache-2.0) | Covers security (contextIsolation, IPC), packaging, updates. Re-read alongside the security ADR. |
| **E2E and UI tests** | `webapp-testing`, `playwright-testing`, `playwright-cli` | Playwright supports Electron. For mobile, the UI tool (Maestro or Appium) is still to be decided in T0.5. |
| **Meta** | `skill-creator` | To write our in-house skills. |

Two Capgo skills (Capawesome migrations) were skipped by the tool because of a format error; irrelevant for us.

**Path-scoped skills.** Six `golang-*` skills (`testing`, `concurrency`, `context`, `error-handling`, `safety`, `security`) declare `paths: "**/*.go"`: Claude Code loads them automatically only when working on Go files, which do not exist before T1.1. They can still be invoked by name.

### Routing check (T0.2 acceptance)

Fresh, non-interactive Claude Code sessions (`claude -p --model sonnet`) were asked, for one task per area, which installed skills they would load. Verified on 2026-10-07:

| Area | Task asked | Skills cited |
|---|---|---|
| Go | Table-driven tests with goroutine leak detection | `go`, `golang-testing`, `golang-concurrency`, `golang-safety` |
| React | Avoid re-renders in a large conversation list | `vercel-react-best-practices`, `vercel-composition-patterns` |
| Capacitor | Plugin exposing a native library on Android and iOS | `capacitor-plugins`, `capacitor-best-practices`, … (also Swift and Android skills) |
| Swift / iOS | Peak memory of a Notification Service Extension on an iPhone | `push-notifications`, `ios-memgraph-analysis`, `debugging-instruments` |
| Android | Foreground service with a persistent notification | `claude-android-ninja` |
| Electron | `contextIsolation` and IPC validation | `electron` |
| Tests | Playwright E2E test of the Electron app | `playwright-testing`, `electron`, `webapp-testing` |

## Audit (2026-10-07)

Third-party skills are read before use. What was checked, on the 38 installed skills:

- **Pre-approved tools (`allowed-tools`)**: the eight `golang-*` skills pre-approve `Bash(git:*)` (and `Bash(gh:*)` for `golang-continuous-integration`); `playwright-cli` pre-approves `Bash(playwright-cli:*)` and `Bash(npx playwright:*)`. Destructive git and gh commands are forced back to a confirmation by `.claude/settings.json` (ADR 0003).
- **Scripts**: only four skills ship scripts. `skill-creator` (runs `claude -p` for its evals), `webapp-testing` (starts a server command you pass it, with `shell=True`), `ios-memgraph-analysis` (runs `xcrun`), `claude-android-ninja` (repository maintenance scripts under `.github/`, not used). None is run automatically; none downloads and executes code.
- **Instructions to install tools** (`go install …@latest`, `brew install`, `pip install pidcat`, `npm install -g @playwright/cli`) appear as documentation only. Any such install still goes through the normal rules: license check, pinned version.
- **Prompt-injection phrases** ("ignore previous instructions", "do not tell the user"…): none found; the matches for "system prompt" are about iOS and Android permission dialogs.
- **skills.sh rating** (shown by the CLI during the first install, before telemetry was disabled): all "Safe" except `golang-project-layout` (generic analysis: medium risk), and one Socket alert each for `golang-continuous-integration`, `playwright-cli`, `webapp-testing` and `skill-creator`. These are consistent with the pre-approved tools and scripts above.
- **Licenses** (development use only, nothing is shipped): MIT or Apache-2.0 for every source except `dpearson2699/swift-ios-skills` (**PolyForm Perimeter 1.0.0**, not open source) and `Cap-go/capgo-skills` (**no license**). Rule: skill content is reference material; **never copy code verbatim from a skill** into Musubee unless its license is AGPL-compatible and the copy is recorded in `NOTICE` (ADR 0003).

## `caveman` plugin (short answers)

**Source: `JuliusBrussee/caveman` only.** Many copies and forks exist under other names; do not use them.

Installation, from a terminal (Claude Code installed):
```
powershell -ExecutionPolicy Bypass -File scripts/install-plugins.ps1     # Windows PowerShell
scripts\install-plugins.bat                                              # Windows cmd
sh scripts/install-plugins.sh                                            # Linux, macOS, Git Bash
```
It runs `claude plugin marketplace add JuliusBrussee/caveman` then `claude plugin install caveman@caveman` (two separate commands, because `&&` does not work in Windows PowerShell 5.1). Restart the session afterwards.

**Why a plugin and not a plain skill?** This plugin adds *hooks* that activate it automatically from the first message. This is the case where the plugin packaging is justified.

**Status (2026-10-07):** installed at user level (version 3.1.0, official source). Its hooks only use Node.js built-in modules; the npm packages the installer reported as "not installed" belong to sub-projects we do not use (browser, extension, MCP server).

**Audit (2026-10-06, version 3.1.0).**
- Verified: the marketplace and plugin name (`caveman@caveman`) match the manifest; license **Apache-2.0** since version 3.0.0 (many READMEs of copies still say MIT); compatible with AGPL-3.0, and in any case it is a development tool that we do not distribute.
- Verified in both hooks (at session start and on every message): no network call in their code; they write a small flag file in `~/.claude` and a config in `%APPDATA%\caveman`, and run `node` locally for statistics.
- Not verified: the rest of the repository (CLI, proxy, MCP servers, extension). **Do not install them.** Also avoid the `irm … | iex` installer and the `caveman-code` option: they add a proxy and binaries we have not audited.
- It only saves **output** tokens (the plugin adds about 1 to 1.5k input tokens per turn, according to its author): the gain is modest for a project where most of the output is code and documents.

**Optional setting.** To make it activate only on demand (`/caveman`) instead of starting on its own:
```
setx CAVEMAN_DEFAULT_MODE manual
```

**Usage rules**: `CLAUDE.md` §7a (including the ban on `caveman-commit` and `caveman-compress`).

## In-house skills to create (no public skill covers them)
Prefix `musubee-`. To be created with `skill-creator`, **after** the corresponding spikes, never before.

1. `musubee-domain-abstraction`: rules of the domain layer, no Matrix type towards the UI.
2. `musubee-bridgev2-on-device`: writing and embedding an in-process `bridgev2` connector (after T1.1).
3. `musubee-connection-modes`: "on-device" / "hosted bridge" modes and honest messages to the user.
4. `musubee-ios-nse-budget`: memory and execution constraints of the notification extension (after T1.7).
5. `musubee-merge-chats`: data model for merging.
6. `musubee-realistic-test-env`: bringing up the full test environment (Synapse and PostgreSQL, the in-house echo connector, the Telegram test bots of ADR 0006).
7. `musubee-go-mobile-embedding`: integrating the Go core into Android/iOS/Electron (after T1.2, T1.3, T1.5).
