# ADR 0003 — Guardrails for third-party Claude Code skills

- **Status**: accepted
- **Date**: 2026-10-07
- **Task**: T0.2
- **Deciders**: Quentin Raimbaud (maintainer), with the design assistant

> This ADR is not legal advice. The licensing points marked "to be reviewed" must be validated by a competent lawyer before the public launch.

## Context

T0.2 installs 38 third-party skills from 10 GitHub repositories into `.claude/skills/` (list: `scripts/skills.txt`, rationale: `docs/SKILLS.md`). Skills are instructions and files written by other people that Claude Code loads into its context and may act on. Installing them raised four concrete issues:

1. **Pre-approved tools.** The eight `golang-*` skills (samber/cc-skills-golang) declare `allowed-tools` that include `Bash(git:*)`, and `golang-continuous-integration` adds `Bash(gh:*)`. While such a skill is active, Claude Code can run any `git` or `gh` command without asking, including `git push --force` or `gh repo delete`.
2. **Telemetry.** The `skills` CLI (npm `skills` 1.7.1, MIT) reports every install (source repository and skill names) to `skills.sh` unless `DISABLE_TELEMETRY` or `DO_NOT_TRACK` is set. The same call fetches skills.sh's security rating.
3. **Licenses.** The skills are development aids, never shipped with Musubee, but their content (including code examples) can end up in our code through Claude.
4. **Integrity.** We do not version third-party skill copies (`.gitignore`), so nothing recorded what exactly was installed and reviewed.

## Options considered

- **Remove the skills that pre-approve tools.** Loses the Go guidance that is central to the core.
- **Edit the installed `SKILL.md` files to strip `allowed-tools`.** Undone at every reinstall or update; silently diverges from upstream.
- **Project permission rules (chosen).** Claude Code evaluates permission rules in the order deny, then ask, then allow, and "deny and ask rules still override `allowed-tools`" (verified in the official documentation, see Sources). A versioned `.claude/settings.json` with `ask` rules keeps the skills unchanged while forcing a confirmation for destructive commands.

## Decision

1. **`.claude/settings.json` (versioned)** forces a confirmation (`ask`) for destructive or account-level commands: force pushes, branch and tag deletion on the remote, `git reset --hard`, `git clean`, `git branch -D`, history rewriting (`filter-branch`, `filter-repo`), `git config --global`, and `gh repo delete|archive|edit|rename`, `gh release delete`, `gh secret`, `gh variable`, `gh auth`, `gh api`, `gh workflow disable`, `gh run delete`, `gh cache delete`. It also disables the user-level `dart-flutter` plugin for this project only.
2. **Telemetry off.** The three `install-skills` scripts set `DISABLE_TELEMETRY=1` for their own process. We give up skills.sh's rating and rely on our own review (`docs/SKILLS.md`, "Audit").
3. **Licenses: reference only.** Third-party skills are used as reference material. **Code is never copied verbatim from a skill into Musubee** unless that skill's license is compatible with AGPL-3.0-or-later and the copy is recorded in `NOTICE`. This matters most for:
   - `dpearson2699/swift-ios-skills` (9 skills): **PolyForm Perimeter 1.0.0**, a source-available license that is not open source. It allows any use except marketing a product that substitutes for the software itself; Musubee does not compete with an iOS skill pack.
   - `Cap-go/capgo-skills` (10 skills): **no license file** found, which means all rights reserved by default.
4. **Integrity.** `skills-lock.json`, written by the `skills` CLI, is versioned. It records the source, path and content hash of every installed skill, so any upstream change shows up as a diff on reinstall.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| `allowed-tools` grants tools without prompting during the turn that invokes the skill; deny and ask rules still override it | **verified** (official docs) | <https://code.claude.com/docs/en/skills>, 2026-10-07 |
| Permission rules are evaluated deny, then ask, then allow; an ask rule prompts even when an allow rule matches | **verified** (official docs) | <https://code.claude.com/docs/en/permissions>, 2026-10-07 |
| The `golang-*` skills pre-approve `Bash(git:*)`; `golang-continuous-integration` also `Bash(gh:*)`; `playwright-cli` pre-approves `Bash(playwright-cli:*)` and `Bash(npx playwright:*)` | **verified** (frontmatter of the installed `SKILL.md` files) | `.claude/skills/*/SKILL.md` |
| The `skills` CLI sends install telemetry to skills.sh unless `DISABLE_TELEMETRY` or `DO_NOT_TRACK` is set | **verified** (read in `dist/cli.mjs` of npm `skills` 1.7.1) | npm `skills` 1.7.1 |
| With `DISABLE_TELEMETRY=1` set by our script, the variable reaches `npx` | **verified**: a real reinstall printed no skills.sh rating table, while the first install (without it) did | install logs, 2026-10-07 |
| The `ask` rules match commands written as `git …` / `gh …` | **assumed** from the documented matching rules; a command invoked through an absolute path to the executable (for example `/c/…/gh.exe api …`) would not match, so commands must be written with the bare program name | permissions docs |
| Repository licenses: spf13/go-skills MIT, samber/cc-skills-golang MIT, avdlee/swift-concurrency-agent-skill MIT, Drjacky/claude-android-ninja Apache-2.0, TerminalSkills/skills Apache-2.0, microsoft/playwright-cli Apache-2.0; anthropics/skills per-skill Apache-2.0 (`LICENSE.txt` in `skill-creator`, `webapp-testing`); vercel-labs/agent-skills per-skill MIT (frontmatter) | **verified** (GitHub license API and installed files) | GitHub API, 2026-10-07 |
| dpearson2699/swift-ios-skills is under PolyForm Perimeter 1.0.0 | **verified** (repository `LICENSE`) | <https://github.com/dpearson2699/swift-ios-skills> |
| Cap-go/capgo-skills has no license | **verified** (no `LICENSE` at the repository root; GitHub reports none) | GitHub API, 2026-10-07 |
| Using PolyForm Perimeter or unlicensed skills as reference during development is permitted, and only verbatim copies into our code are a problem | **assumed**. **To be reviewed by a lawyer.** | — |

## Consequences

- Destructive git and GitHub operations always ask the maintainer, whichever skill is active. Routine work (`git commit`, `git push` without force, `gh pr create`, `gh pr merge`, `gh run watch`) is unaffected.
- Reinstalling skills no longer reports anything to skills.sh, and no longer shows its rating; reviewing skills is our job (`docs/SKILLS.md`).
- Contributors and Claude must treat iOS and Capacitor skill content as reference, not as copyable code.
- **Revisit if**: a skill with a clear open-source license replaces the PolyForm or unlicensed ones; or the Capgo repository adds a license; or Claude Code changes how `allowed-tools` and `ask` rules interact.

## Sources

- Claude Code docs, skills: <https://code.claude.com/docs/en/skills> (accessed 2026-10-07).
- Claude Code docs, permissions: <https://code.claude.com/docs/en/permissions> (accessed 2026-10-07).
- npm `skills` 1.7.1, `dist/cli.mjs` (`isEnabled`, `fetchAuditData`), read on 2026-10-07.
- PolyForm Perimeter 1.0.0: <https://polyformproject.org/licenses/perimeter/1.0.0>.
