# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later

"""Tests for scripts/language_check.py (the repository is English-only).

Each test copies the repository files known to Git into a temporary
directory, adds or changes a file, and runs the real check on that copy.
French samples are written with escapes or carry the ignore pragma, so
that this file itself passes the check.

Run from the repository root:
    python -m unittest discover -s scripts/tests -v
"""

from __future__ import annotations

import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
CHECK_SCRIPT = REPO_ROOT / "scripts" / "language_check.py"
PRAGMA = "language-check: " + "ignore"


def repo_files() -> list[Path]:
    out = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=REPO_ROOT,
        check=True,
        capture_output=True,
    ).stdout.decode("utf-8")
    return [Path(p) for p in out.split("\0") if p and (REPO_ROOT / p).is_file()]


class LanguageCheckTest(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory(prefix="musubee-language-")
        self.root = Path(self._tmp.name)
        for rel in repo_files():
            dest = self.root / rel
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(REPO_ROOT / rel, dest)

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def run_check(self) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, str(CHECK_SCRIPT), "--root", str(self.root)],
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
        )

    def write(self, rel: str, content: str | bytes) -> None:
        path = self.root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        if isinstance(content, bytes):
            path.write_bytes(content)
        else:
            path.write_text(content, encoding="utf-8", newline="\n")

    def assertPasses(self, result: subprocess.CompletedProcess[str]) -> None:
        self.assertEqual(
            result.returncode, 0, f"expected success\n{result.stdout}\n{result.stderr}"
        )

    def assertFails(self, result: subprocess.CompletedProcess[str], needle: str) -> None:
        self.assertNotEqual(
            result.returncode, 0, f"expected failure\n{result.stdout}\n{result.stderr}"
        )
        self.assertIn(needle, result.stdout + result.stderr)

    def test_repository_as_is_is_english_only(self) -> None:
        self.assertPasses(self.run_check())

    def test_accented_french_text_fails(self) -> None:
        # A French heading, written with escapes.
        self.write("docs/notes.md", "# Contr\u00f4le des en-t\u00eates\n")  # language-check: ignore
        self.assertFails(self.run_check(), "docs/notes.md:1")

    def test_unaccented_french_sentence_fails(self) -> None:
        sentence = "Nous allons tester les scripts dans la CI."  # language-check: ignore
        self.write("core/notes.txt", "Intro line.\n" + sentence + "\n")
        self.assertFails(self.run_check(), "core/notes.txt:2")

    def test_french_code_comment_fails(self) -> None:
        comment = "// Envoie le message et attend la r\u00e9ponse"  # language-check: ignore
        self.write("core/send.go", "package core\n\n" + comment + "\n")
        self.assertFails(self.run_check(), "core/send.go:3")

    def test_french_file_name_fails(self) -> None:
        self.write("docs/d\u00e9cisions.md", "# Decisions\n")
        self.assertFails(self.run_check(), "cisions.md: French character in the file name")

    def test_english_false_friends_pass(self) -> None:
        # Single French-looking words in English text must not trigger.
        self.write(
            "docs/english.md",
            "See Smith et al. for details.\n"
            "The job runs at 9 AM EST on the main branch.\n"
            "A la carte options are listed in the table.\n",
        )
        self.assertPasses(self.run_check())

    def test_pragma_skips_the_line(self) -> None:
        self.write(
            "docs/quote.md",
            "The motto reads: nous sommes les testeurs. <!-- " + PRAGMA + " -->\n",  # language-check: ignore
        )
        self.assertPasses(self.run_check())

    def test_binary_files_are_ignored(self) -> None:
        self.write("ui/logo.png", b"\x89PNG\r\n\x1a\n\x00\x00les des une \xe9t\xe9")  # language-check: ignore
        self.assertPasses(self.run_check())

    def test_license_texts_are_ignored(self) -> None:
        self.write("LICENSES/LicenseRef-Example.txt", "Texte de la licence en fran\u00e7ais.\n")  # language-check: ignore
        self.assertPasses(self.run_check())


if __name__ == "__main__":
    unittest.main()
