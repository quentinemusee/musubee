# ADR 0002 — AGPL-3.0-or-later license, DCO and REUSE check

- **Status**: accepted
- **Date**: 2026-10-06 (translated into English and completed on 2026-10-07, see "Revisions")
- **Task**: T0.1
- **Deciders**: Quentin Raimbaud (maintainer)

> This ADR is not legal advice. Points marked "to be reviewed" must be validated by a competent lawyer before the public launch.

## Context

`CLAUDE.md` sets AGPL-3.0 for all the project's code and the DCO for contributions. Still to be specified: the license variant (`-only` or `-or-later`), the copyright holder in the headers, and the tool that makes CI fail when a file has no SPDX header (T0.1 criterion).

## Options considered

### License variant
- **`AGPL-3.0-or-later` (chosen by the maintainer).** The most common practice; allows moving to a future AGPL v4 without every contributor's consent.
- `AGPL-3.0-only`: full control over future versions, but no change possible without the consent of all contributors.

### Header check
- **REUSE (`reuse lint`, FSFE) (chosen).** A recognized standard (REUSE 3.3 specification). It checks, for every file, the presence of copyright and license information, the validity of SPDX expressions, and the presence in `LICENSES/` of the text of every license cited. The last point also blocks a header from introducing an unapproved license.
- A dependency-free in-house script: simpler to install, but it reinvents the specification and covers neither SPDX validity nor license texts.

## Decision

1. **License**: `AGPL-3.0-or-later` for all the project's code and documentation. Full text in `LICENSE` and `LICENSES/AGPL-3.0-or-later.txt`.
2. **Copyright**: `SPDX-FileCopyrightText: 2026 Quentin Raimbaud` on existing files. Each contributor adds their own line to the files they create or substantially modify.
3. **Contributions**: DCO 1.1 (`Signed-off-by`), **no CLA**. See `CONTRIBUTING.md`.
4. **Check**: `python scripts/license_check.py` runs `reuse lint` (REUSE 6.2.0, pinned in `scripts/requirements-dev.txt`). GitHub Actions CI (`.github/workflows/checks.yml`) runs it on Linux and Windows, together with the tests of the check.
5. **Files without a header**: only Markdown documentation and `NOTICE` are annotated in `REUSE.toml`. All source code must carry its own header.

## State of knowledge

| Claim | Status | Source |
|---|---|---|
| The text of `LICENSES/AGPL-3.0-or-later.txt` is the official AGPL v3 text | **verified**: downloaded with `reuse download` (SPDX list), compared word by word with the choosealicense.com copy. Only differences: three URLs with `http://` instead of `https://`. SHA-256 of the file as committed (LF line endings): `d8a6cc31abc16b6748c7a21f21611f5a1ec33f67d22ca23d7da1c19b95496bee`. gnu.org was unreachable from the work machine on 2026-10-06. | SPDX list; github/choosealicense.com |
| `reuse lint` fails on a file without a header, without copyright, or with a license missing from `LICENSES/` | **verified** by `scripts/tests/test_license_check.py` (run locally on Windows, and on Linux in the CI job run locally with `act`) | — |
| The REUSE tool is licensed `Apache-2.0 AND CC0-1.0 AND CC-BY-SA-4.0 AND GPL-3.0-or-later`; used only for development and CI, it is not distributed with Musubee and imposes nothing on our code | **verified** (`License` field of the 6.2.0 package metadata) | `importlib.metadata`, repository <https://github.com/fsfe/reuse-tool> |
| The DCO 1.1 may be reproduced as is ("verbatim copies") | **verified** | <https://developercertificate.org/> |
| AGPL-3.0 is compatible with distribution on Apple's App Store | **unknown**, known risk (the VLC case in 2011 with the GPL). **To be reviewed by a lawyer** before E3.3. | `CLAUDE.md` §3 |
| Without a CLA, adding an App Store exception later (additional permission, AGPL section 7) will require the consent of **all** past contributors | **assumed** (reading of AGPL section 7: only the copyright holder can grant an additional permission on their code). **To be reviewed.** | AGPL-3.0, section 7 |

## Consequences

- CI rejects any file without a valid SPDX header, and any license whose text is not in `LICENSES/`. Adding a third-party license becomes an explicit act, visible in review.
- Python and REUSE are needed to run the check locally (`python -m pip install -r scripts/requirements-dev.txt`).
- **App Store exception**: should ideally be decided **before** accepting external contributions, while the maintainer is the sole copyright holder; otherwise every contributor's consent will be needed. **Deferred by the maintainer on 2026-10-07**: the repository is private and no external contributors are planned. To be revisited before the first external contribution or before E3.3, whichever comes first.
- License auditing of **dependencies** (Go, npm) is not covered by REUSE: it arrives in T0.6.

## Revisions

- 2026-10-07: translated into English (the project is now English-only); workflow renamed from `license-check.yml` to `checks.yml`; App Store deferral recorded. The decision itself is unchanged.

## Sources

- REUSE 3.3 specification: <https://reuse.software/spec-3.3/>; REUSE tool 6.2.0 (PyPI `reuse`).
- AGPL v3 text: SPDX license list (via `reuse download AGPL-3.0-or-later`) and <https://github.com/github/choosealicense.com/blob/gh-pages/_licenses/agpl-3.0.txt>, accessed 2026-10-06.
- Developer Certificate of Origin 1.1: <https://developercertificate.org/>, accessed 2026-10-06.
