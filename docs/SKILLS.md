# Musubee skills — what, why, how

## Installation

From the repository root, with Node.js and git installed, run the variant for your shell:

```
powershell -ExecutionPolicy Bypass -File scripts/install-skills.ps1     # Windows PowerShell
scripts\install-skills.bat                                              # Windows cmd
sh scripts/install-skills.sh                                            # Linux, macOS, Git Bash
```

The three variants read the same list, `scripts/skills.txt`, and run the same `npx skills add` commands. Add `--dry-run` (all three variants; PowerShell also accepts `-DryRun`) to print the commands without running them.

Everything goes into `.claude/skills/` (project level). **Tested on 2026-10-06 on a blank project with the original PowerShell script: 38 skills installed, all with a valid `SKILL.md`** (≈ 7 MB). Then restart the Claude Code session; check with `npx skills list`.

The Flutter plugin is no longer useful (the stack is no longer Flutter):
```
claude plugin uninstall dart-flutter@dart-flutter
```

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
6. `musubee-realistic-test-env`: bringing up the full test environment (Synapse, dummybridge, Telegram test servers).
7. `musubee-go-mobile-embedding`: integrating the Go core into Android/iOS/Electron (after T1.2, T1.3, T1.5).
