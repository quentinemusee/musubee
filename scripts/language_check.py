# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later

"""English-only check for the Musubee repository.

Everything in the repository must be written in English (CLAUDE.md, section 0).
This check fails when a tracked text file, or a file path, contains French:

- any letter with a French diacritic, or French quotation marks;
- or a line that contains at least two distinct common French function
  words (see FRENCH_WORDS), which catches unaccented French sentences
  while letting English through ("et al.", "EST").

A line can opt out with the pragma "language-check: ignore" (for example a
test fixture that must contain French). License texts in LICENSES/ and
LICENSE are skipped, and so are binary files.

Usage:
    python scripts/language_check.py            # check this repository
    python scripts/language_check.py --root DIR # check another directory
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]

PRAGMA = "language-check: ignore"

# Letters with French diacritics (lower and upper case), the oe ligatures and
# the French quotation marks. Written as code points so that this file stays
# ASCII and does not flag itself.
_FRENCH_CODE_POINTS = (
    0xE0, 0xE2, 0xE7, 0xE8, 0xE9, 0xEA, 0xEB, 0xEE, 0xEF, 0xF4, 0xF9, 0xFB, 0xFC, 0xFF,
    0xC0, 0xC2, 0xC7, 0xC8, 0xC9, 0xCA, 0xCB, 0xCE, 0xCF, 0xD4, 0xD9, 0xDB, 0xDC, 0x178,
    0x153, 0x152,  # oe ligatures
    0xAB, 0xBB,  # French quotation marks
)
FRENCH_CHARACTERS = re.compile("[" + "".join(chr(c) for c in _FRENCH_CODE_POINTS) + "]")

# Common French function words that are not English words. A single one is
# tolerated (English borrows "a la", "et al."); two distinct ones on the same
# line are treated as French.
FRENCH_WORDS = frozenset(
    "le la les des du une est sont avec dans pour nous vous pas cette mais "  # language-check: ignore
    "aussi qui que sur aux ces elle ils leur leurs sans chez donc".split()  # language-check: ignore
)
WORD = re.compile(r"[A-Za-z]+")
MIN_FRENCH_WORDS = 2

SKIPPED_PREFIXES = ("LICENSES/",)
SKIPPED_FILES = frozenset({"LICENSE"})


def list_files(root: Path) -> list[str]:
    """Files to check, as POSIX paths relative to root.

    In a Git work tree, the files Git would commit (tracked, plus untracked
    files that are not ignored). Elsewhere, every file except .git/.
    """
    inside_git = subprocess.run(
        ["git", "rev-parse", "--show-toplevel"],
        cwd=root,
        capture_output=True,
        text=True,
        check=False,
    )
    if inside_git.returncode == 0 and Path(inside_git.stdout.strip()).resolve() == root:
        out = subprocess.run(
            ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
            cwd=root,
            capture_output=True,
            check=True,
        ).stdout.decode("utf-8")
        return sorted(p for p in out.split("\0") if p and (root / p).is_file())
    return sorted(
        p.relative_to(root).as_posix()
        for p in root.rglob("*")
        if p.is_file() and ".git" not in p.relative_to(root).parts
    )


def read_text(path: Path) -> str | None:
    """Return the file content, or None for binary or non-UTF-8 files."""
    data = path.read_bytes()
    if b"\0" in data:
        return None
    try:
        return data.decode("utf-8")
    except UnicodeDecodeError:
        return None


def line_problem(line: str) -> str | None:
    if PRAGMA in line:
        return None
    match = FRENCH_CHARACTERS.search(line)
    if match:
        return f"French character U+{ord(match.group()):04X}"
    words = {w.lower() for w in WORD.findall(line)} & FRENCH_WORDS
    if len(words) >= MIN_FRENCH_WORDS:
        return "French words " + ", ".join(sorted(words))
    return None


def check(root: Path) -> list[str]:
    problems = []
    for rel in list_files(root):
        if rel in SKIPPED_FILES or rel.startswith(SKIPPED_PREFIXES):
            continue
        if FRENCH_CHARACTERS.search(rel):
            shown = rel.encode("ascii", "backslashreplace").decode("ascii")
            problems.append(f"{shown}: French character in the file name")
        text = read_text(root / rel)
        if text is None:
            continue
        for number, line in enumerate(text.splitlines(), start=1):
            problem = line_problem(line)
            if problem:
                problems.append(f"{rel}:{number}: {problem}")
    return problems


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
        sys.exit(f"language-check: {root} is not a directory")

    problems = check(root)
    for problem in problems:
        print(problem)
    if problems:
        print(
            f"\nlanguage-check: FAILED, {len(problems)} line(s) look French. "
            "Everything in the repository must be written in English "
            "(CLAUDE.md, section 0).",
            file=sys.stderr,
        )
        return 1
    print("language-check: OK, no French found.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
