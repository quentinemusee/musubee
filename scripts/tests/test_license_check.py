# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later

"""Tests for scripts/license_check.py.

These tests run the real REUSE tool (no mock) against a throw-away copy of
the repository. Each test copies the files Git knows about (tracked, plus
untracked files that are not ignored) into a temporary directory, changes
that copy, and checks the exit code of the license check.

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
CHECK_SCRIPT = REPO_ROOT / "scripts" / "license_check.py"

# REUSE-IgnoreStart
GOOD_HEADER = (
    "// SPDX-FileCopyrightText: 2026 Quentin Raimbaud\n"
    "// SPDX-License-Identifier: AGPL-3.0-or-later\n"
)


def repo_files() -> list[Path]:
    """Return the files Git would commit, relative to the repository root."""
    out = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=REPO_ROOT,
        check=True,
        capture_output=True,
    ).stdout.decode("utf-8")
    return [Path(p) for p in out.split("\0") if p and (REPO_ROOT / p).is_file()]


class LicenseCheckTest(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory(prefix="musubee-license-")
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

    def write(self, rel: str, content: str) -> None:
        path = self.root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
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

    def test_repository_as_is_is_compliant(self) -> None:
        self.assertPasses(self.run_check())

    def test_source_file_without_header_fails(self) -> None:
        self.write("core/domain/bad.go", "package domain\n")
        self.assertFails(self.run_check(), "bad.go")

    def test_adding_the_header_fixes_it(self) -> None:
        self.write("core/domain/bad.go", "package domain\n")
        self.assertNotEqual(self.run_check().returncode, 0)
        self.write("core/domain/bad.go", GOOD_HEADER + "\npackage domain\n")
        self.assertPasses(self.run_check())

    def test_typescript_file_without_header_fails(self) -> None:
        self.write("ui/src/App.tsx", "export const App = () => null;\n")
        self.assertFails(self.run_check(), "App.tsx")

    # REUSE-IgnoreStart
    def test_license_identifier_without_copyright_fails(self) -> None:
        self.write(
            "core/domain/nocopyright.go",
            "// SPDX-License-Identifier: AGPL-3.0-or-later\n\npackage domain\n",
        )
        self.assertFails(self.run_check(), "nocopyright.go")

    def test_unknown_license_without_text_fails(self) -> None:
        # A license whose text is not in LICENSES/ must be rejected, so that
        # an incompatible license cannot slip in through a header.
        self.write(
            "core/domain/gpl2.go",
            "// SPDX-FileCopyrightText: 2026 Someone\n"
            "// SPDX-License-Identifier: GPL-2.0-only\n\npackage domain\n",
        )
        self.assertFails(self.run_check(), "GPL-2.0-only")

    # REUSE-IgnoreEnd

    def test_markdown_docs_are_covered_by_reuse_toml(self) -> None:
        # Documentation is annotated in REUSE.toml; a new doc needs no header.
        self.write("docs/ADR/9999-example.md", "# Example\n")
        self.assertPasses(self.run_check())


if __name__ == "__main__":
    unittest.main()
