# ADR 0008 — Automated license audit of Go and npm dependencies

- **Status**: accepted
- **Date**: 2026-10-07
- **Task**: T0.6
- **Deciders**: Quentin Raimbaud (maintainer), with the design assistant

> This ADR is not legal advice. The license compatibility list below follows the FSF's published views and should be reviewed by a competent lawyer before the public launch.

## Context

`CLAUDE.md` §6 rule 4 requires checking the SPDX license of every dependency and rejecting anything incompatible with AGPL-3.0-or-later. Until now this was done by hand (T0.3 to T0.5). T0.6 asks for an automated audit of the Go and npm dependencies in CI, with a list of justified exceptions, and a test showing that CI fails when an incompatible dependency is added.

The REUSE check (ADR 0002) covers our own files only, not dependencies.

## Options considered

- **google/go-licenses** (Apache-2.0): mature, but Go only; we also have npm (Appium tooling now, the UI later).
- **npm-oriented checkers** (license-checker and similar): npm only, and they trust the declared `license` field.
- **A small tool of our own on top of google/licensecheck (chosen)**: one report for both ecosystems; licenses detected from the license **texts** with `licensecheck` (the library behind pkg.go.dev, BSD-3-Clause) for Go modules and for npm packages that declare nothing; SPDX expressions (AND / OR / WITH) evaluated against one policy file.

## Decision

1. `scripts/licenseaudit` (Go module in `go.work`): run `go run ./scripts/licenseaudit` from the repository root (`-v` lists every dependency).
   - **Go**: the modules compiled into the packages and tests of every module of `go.work`, with the `integration` and `telegram` tags, so that test tooling is audited too; the license is detected from the module's license files (`licensecheck`, at least 75% coverage, or an `SPDX-License-Identifier` line).
   - **npm**: every `package-lock.json` tracked by Git (lockfile v2 or v3); the declared SPDX expression, else the installed `package.json`, else the installed license files. For packages **not installed on this platform** (optional, platform-specific builds), whose license appears nowhere locally, the tool asks the **npm registry** for the license of that exact version. A directory without `package.json` (old npm versions leave empty directories for skipped optional packages) does not count as installed.
2. `scripts/license-policy.json` lists the **allowed** SPDX identifiers and the **exceptions**. An exception names one package (or a family with a trailing `*`), what we know of its license, and a reason; an exception that matches nothing fails the audit, so the list cannot rot.
3. **Allowed**: permissive licenses (MIT, MIT-0, ISC, BSD-2/3-Clause, 0BSD, Apache-2.0, Zlib, Unlicense, CC0-1.0, BlueOak-1.0.0, WTFPL, PSF-2.0, Python-2.0), MPL-2.0 (its "Secondary License" clause allows combination with the GNU licenses), LGPL-2.1 / LGPL-3.0, GPL-3.0 and AGPL-3.0 (AGPL §13 allows combination with GPLv3), and **GPL-2.0 only in its "or later" form**. A GNU license text alone does not say "only" or "or later", so a GPL-2.0 license file is rejected unless the package declares "GPL-2.0-or-later".
4. **Not allowed** (examples): GPL-2.0-only, SSPL, BUSL, Elastic License, Commons Clause, non-commercial Creative Commons, proprietary or unknown licenses. A test keeps several of them out of the policy.
5. **Current exceptions** (all in the Appium test tooling, never distributed): `css-value` (MIT stated in its README, no `license` field), `@promptbook/utils` (CC-BY-4.0), `spdx-exceptions` (CC-BY-3.0, data).
6. CI: job "Dependency license audit" on Linux, after `npm ci --ignore-scripts`.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| `licensecheck` recognizes the license files of our Go dependencies (BSD-3-Clause, MPL-2.0, MIT; 98.8% to 100% coverage on the files probed) and our own AGPL-3.0 text | **verified** (probe and `-v` listing match the manual review of T0.3/T0.4) | local runs, 2026-10-07 |
| `licensecheck` does not read `SPDX-License-Identifier` lines; the tool reads them itself | **verified** | local probe |
| First audit of the repository: 16 Go modules, 500 npm packages; 53 problems, all in the Appium tooling. 49 were platform builds of `sharp` whose license is not in the lock file: solved by the registry lookup (Apache-2.0, LGPL-3.0-or-later for libvips). The 3 others are the justified exceptions above | **verified** | local runs, 2026-10-07 |
| On Linux CI, `@emnapi/runtime` (MIT) and `tslib` (0BSD), optional dependencies of sharp's WebAssembly build, are not installed and have no license in the lock file: the first CI run failed on them, which led to the registry lookup | **verified** | CI, PR #10 |
| The unit tests fail when the policy allows an incompatible license | **verified** (mutation: GPL-2.0-only added to the policy made `TestRepositoryPolicyIsValid` fail) | local run |
| CI fails when an incompatible dependency is added | **verified**: see "Acceptance run" below | CI run 37693400189 |
| The compatibility list matches the FSF's views (GPLv3/AGPLv3 compatibility of MPL-2.0, Apache-2.0, LGPL, CC0, CC-BY-4.0) | **assumed** from the FSF license list; **to be reviewed by a lawyer** | <https://www.gnu.org/licenses/license-list.html> |

## Acceptance run

A test branch added `highcharts` (license declared as `https://www.highcharts.com/license`, a proprietary license) as a development dependency of `apps/mobile/e2e`, with npm 11 so that the lock file only gained that package. **Result (verified, CI run 37693400189, PR #12):** the "Dependency license audit" job failed with exactly one problem, `npm highcharts@13.1.1: license "https://www.highcharts.com/license" is not compatible with AGPL-3.0-or-later`; the build, unit, integration and mobile jobs passed. The pull request was closed without merging and the branch deleted.

A first attempt (PR #11) was made with the maintainer's local npm 8.10, which rewrote the lock file and dropped nested optional entries, so `npm ci` failed in unrelated jobs. The e2e project now requires npm 11 or later (`engines` and `engine-strict`).

## Consequences

- Every pull request fails when a dependency with an incompatible, unknown or unreadable license appears, in Go or npm, including test tooling.
- Adding an exception is a visible, reviewed change with a written reason.
- Shipped and development-only dependencies are judged by the same list today; when the UI and the core ship (T1.x), the policy will separate them so that exceptions accepted for test tooling (such as CC-BY-4.0) can never reach shipped code.
- `NOTICE` stays hand-written; the audit's `-v` listing helps keep it accurate.

## Sources

- google/licensecheck v0.3.1: <https://github.com/google/licensecheck> (BSD-3-Clause).
- FSF, "Various Licenses and Comments about Them": <https://www.gnu.org/licenses/license-list.html>.
- npm registry metadata (`npm view <package> license`), 2026-10-07.
