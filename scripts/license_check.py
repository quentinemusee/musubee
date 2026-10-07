# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later

"""License check for the Musubee repository.

Runs ``reuse lint`` (https://reuse.software) and fails when a file has no
SPDX copyright and license information, has an invalid SPDX expression, or
uses a license whose text is not present in LICENSES/.

Files ignored by Git are skipped. REUSE is supposed to skip them itself, but
it does not when .gitignore contains a negation rule (we ignore third-party
skills in .claude/skills/ but keep our own musubee-* skills): the Git query it
relies on, "git ls-files --ignored --directory --no-empty-directory", then
returns nothing (checked with Git 2.41 and 2.54). This script therefore reads
REUSE's JSON report and drops the files that "git check-ignore" reports.

Usage (from anywhere):
    python scripts/license_check.py            # check this repository
    python scripts/license_check.py --root DIR # check another directory

The REUSE tool is a development dependency, pinned in
scripts/requirements-dev.txt. Install it with:
    python -m pip install -r scripts/requirements-dev.txt
"""

from __future__ import annotations

import argparse
import json
import re
import shutil
import subprocess
import sys
from pathlib import Path, PurePath

REPO_ROOT = Path(__file__).resolve().parents[1]

# Keys of REUSE's JSON report ("non_compliant") that list files, and keys
# that list license identifiers (the files using them are found through the
# SPDX expressions of each file in the report).
FILE_LISTS = ("missing_copyright_info", "missing_licensing_info", "read_errors")
LICENSE_LISTS = ("bad_licenses", "deprecated_licenses", "missing_licenses")
LICENSE_TOKEN = re.compile(r"[A-Za-z0-9.+-]+")
OPERATORS = frozenset({"AND", "OR", "WITH"})
# Keys about the LICENSES/ directory itself; never filtered.
DIRECTORY_LISTS = ("licenses_without_extension", "unused_licenses")


def reuse_command() -> list[str]:
    """Find the REUSE tool: the ``reuse`` executable, or the Python module."""
    exe = shutil.which("reuse")
    if exe:
        return [exe]
    probe = subprocess.run(
        [sys.executable, "-c", "import reuse"], capture_output=True, check=False
    )
    if probe.returncode == 0:
        return [sys.executable, "-m", "reuse"]
    sys.exit(
        "license-check: the REUSE tool is not installed.\n"
        "Install it with: python -m pip install -r scripts/requirements-dev.txt"
    )


def relative(path: str, root: Path) -> str:
    """Normalize a path from REUSE's report to a POSIX path relative to root."""
    pure = Path(path)
    if pure.is_absolute():
        try:
            pure = pure.resolve().relative_to(root)
        except ValueError:
            pass
    return PurePath(pure).as_posix()


def git_ignored(paths: set[str], root: Path) -> set[str]:
    """Return the subset of paths that Git ignores (empty outside a Git repo)."""
    if not paths:
        return set()
    result = subprocess.run(
        ["git", "check-ignore", "--stdin", "-z"],
        cwd=root,
        input="\0".join(sorted(paths)).encode("utf-8"),
        capture_output=True,
        check=False,
    )
    # Exit code 0: some paths ignored; 1: none; 128: not a Git repository.
    if result.returncode not in (0, 1):
        return set()
    return {p for p in result.stdout.decode("utf-8").split("\0") if p}


def find_problems(report: dict, root: Path) -> tuple[list[str], int]:
    """Return human-readable problems and the number of ignored files skipped."""
    non_compliant = report["non_compliant"]

    users: dict[str, set[str]] = {}
    for entry in report.get("files", []):
        path = PurePath(entry["path"]).as_posix()
        for expression in entry.get("spdx_expressions", []):
            for token in LICENSE_TOKEN.findall(expression["value"]):
                if token not in OPERATORS:
                    users.setdefault(token, set()).add(path)

    mentioned: set[str] = set()
    for key in FILE_LISTS:
        mentioned.update(relative(p, root) for p in non_compliant.get(key) or [])
    for key in LICENSE_LISTS:
        for license_id in non_compliant.get(key) or []:
            mentioned.update(users.get(license_id, set()))
    invalid = {
        PurePath(entry["path"]).as_posix(): [
            e["value"] for e in entry.get("spdx_expressions", []) if not e.get("is_valid", True)
        ]
        for entry in report.get("files", [])
    }
    invalid = {path: values for path, values in invalid.items() if values}
    mentioned.update(invalid)

    ignored = git_ignored(mentioned, root)
    problems: list[str] = []

    for path, values in sorted(invalid.items()):
        if path not in ignored:
            problems.append(f"{path}: invalid SPDX expression {', '.join(values)!r}")
    for key in FILE_LISTS:
        label = key.replace("_", " ")
        for path in sorted({relative(p, root) for p in non_compliant.get(key) or []}):
            if path not in ignored and path not in invalid:
                problems.append(f"{path}: {label}")
    for key in LICENSE_LISTS:
        label = key.replace("_", " ").rstrip("s")
        for license_id in sorted(non_compliant.get(key) or []):
            license_users = users.get(license_id, set())
            kept = sorted(license_users - ignored)
            if kept:
                problems.append(f"{label} {license_id}, used by: {', '.join(kept)}")
            elif not license_users:
                # Not used by any file: the problem is in LICENSES/ itself.
                problems.append(f"LICENSES/: {label} {license_id}")
    for key in DIRECTORY_LISTS:
        for name in non_compliant.get(key) or []:
            problems.append(f"LICENSES/: {key.replace('_', ' ')} {name}")
    # A category this script does not know (newer REUSE) is never filtered.
    for key, value in non_compliant.items():
        if key not in FILE_LISTS + LICENSE_LISTS + DIRECTORY_LISTS and value:
            problems.append(f"{key.replace('_', ' ')}: {value}")

    return problems, len(ignored)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--root",
        type=Path,
        default=REPO_ROOT,
        help="directory to check (default: the repository root)",
    )
    root = parser.parse_args().root.resolve()
    if not root.is_dir():
        sys.exit(f"license-check: {root} is not a directory")

    cmd = reuse_command() + ["--root", str(root), "lint", "--json"]
    result = subprocess.run(cmd, capture_output=True, check=False)
    try:
        report = json.loads(result.stdout.decode("utf-8"))
    except json.JSONDecodeError:
        sys.stderr.write(result.stderr.decode("utf-8", "replace"))
        sys.exit("license-check: could not read the REUSE report")

    problems, skipped = find_problems(report, root)
    summary = report.get("summary", {})
    if problems:
        for problem in problems:
            print(problem)
        # REUSE-IgnoreStart
        print(
            f"\nlicense-check: FAILED, {len(problems)} problem(s). Every file needs "
            "an SPDX header, e.g.:\n"
            "  // SPDX-FileCopyrightText: 2026 Your Name\n"
            "  // SPDX-License-Identifier: AGPL-3.0-or-later\n"
            "Files that cannot carry a comment are annotated in REUSE.toml.\n"
            "See CONTRIBUTING.md.",
            file=sys.stderr,
        )
        # REUSE-IgnoreEnd
        return 1

    print(
        f"license-check: OK, compliant with REUSE {report.get('reuse_spec_version', '?')} "
        f"({summary.get('files_total', '?')} files scanned, "
        f"{skipped} git-ignored file(s) skipped)."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
