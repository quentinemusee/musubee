# Contributing to Musubee

Thank you for your interest. This document lists the mandatory rules for every contribution. The project context and architecture decisions are in [`CLAUDE.md`](CLAUDE.md) and [`docs/`](docs/).

## 1. License of your contributions

Musubee is distributed under **AGPL-3.0-or-later**. By contributing, you agree that your contribution is distributed under this license. There is **no** CLA (Contributor License Agreement): you keep the copyright on your code. You only certify, through the DCO below, that you have the right to submit it.

## 2. Developer Certificate of Origin (DCO)

Every commit must carry a `Signed-off-by` line with your real name and a valid email address:

```
Signed-off-by: Jane Doe <jane@example.org>
```

`git commit -s` adds it automatically from `user.name` and `user.email`. To sign off the last commit afterwards: `git commit --amend -s --no-edit`. For a series: `git rebase --signoff master`.

With this line you certify the following (official text, reproduced unchanged from <https://developercertificate.org/>):

<!-- SPDX-SnippetBegin -->
<!-- SPDX-SnippetCopyrightText: 2004, 2006 The Linux Foundation and its contributors -->
<!-- SPDX-License-Identifier: LicenseRef-DCO -->

```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.


Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

<!-- SPDX-SnippetEnd -->

## 3. SPDX headers (REUSE specification)

Every source file starts with two lines, in the comment syntax of its language:

<!-- REUSE-IgnoreStart -->

```go
// SPDX-FileCopyrightText: 2026 Your Name
// SPDX-License-Identifier: AGPL-3.0-or-later
```

```python
# SPDX-FileCopyrightText: 2026 Your Name
# SPDX-License-Identifier: AGPL-3.0-or-later
```

```bat
@rem SPDX-FileCopyrightText: 2026 Your Name
@rem SPDX-License-Identifier: AGPL-3.0-or-later
```

<!-- REUSE-IgnoreEnd -->

- If you substantially modify an existing file, add your own `SPDX-FileCopyrightText` line below the existing ones.
- Files that cannot carry a comment (JSON, images…) are declared in [`REUSE.toml`](REUSE.toml). **Never** add source code there: it must carry its own header.
- Code taken from a third-party project: keep its original header, add the text of its license to `LICENSES/` (`reuse download <SPDX-ID>`), check that it is compatible with AGPL-3.0 and update [`NOTICE`](NOTICE).

## 4. English only

Everything in the repository is written in **English**: documentation, ADRs, code, identifiers, file names, comments, script output, UI strings in source code, commit messages and pull requests. `python scripts/language_check.py` rejects French text; CI runs it on every pull request.

## 5. Running the checks locally

CI runs the same commands and fails if any of them fails:

```
python -m pip install -r scripts/requirements-dev.txt
python scripts/license_check.py
python scripts/language_check.py
python -m unittest discover -s scripts/tests -v
```

## 6. Dependencies

Before adding a dependency (Go, npm, Gradle, Swift…):

1. Look up its SPDX license identifier **in its repository**, not only in the package registry.
2. Reject any license incompatible with AGPL-3.0 (for example: proprietary, SSPL, BUSL, Commons Clause, GPL-2.0-only, CC-BY-NC).
3. Update [`NOTICE`](NOTICE) in the same commit.

The automated dependency audit arrives in T0.6.

## 7. Commits, branches and pull requests

- Messages follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) (`feat:`, `fix:`, `docs:`, `chore:`, `test:`, `refactor:`, `ci:`…), with the **why** in the message body.
- Small, coherent commits, all DCO-signed.
- **One task = one branch = one pull request.** Branch name: `type/short-id`, for example `feat/t0.1-repo-skeleton`.
- A PR is merged only if the tests pass. Paste the results into the description and separate what is **verified**, **assumed** and **unknown** (see [`docs/TESTING.md`](docs/TESTING.md), "Definition of done").
- Every structural decision gets an ADR in [`docs/ADR/`](docs/ADR/).

## 8. Tests

Nothing is done without passing tests. Integration tests run against a real Synapse and real bridges ([`infra/`](infra/)); mocks are only tolerated in pure unit tests. Details: [`docs/TESTING.md`](docs/TESTING.md).

## 9. Security

- **Never a secret in the repository** (tokens, keys, passwords, `.env` files). Never a real user account nor a production secret in CI; test credentials go into encrypted CI secrets.
- Never log message content.
- Found a vulnerability? Do not open a public issue. Contact the maintainer privately (a reporting address will be published before the first release).
