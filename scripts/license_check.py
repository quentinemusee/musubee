# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later

"""License check for the Musubee repository.

Runs ``reuse lint`` (https://reuse.software) and exits with its status.
The check fails when a file has no SPDX copyright and license information,
or uses a license whose text is not present in LICENSES/.

Usage (from anywhere):
    python scripts/license_check.py            # check this repository
    python scripts/license_check.py --root DIR # check another directory

The REUSE tool is a development dependency, pinned in
scripts/requirements-dev.txt. Install it with:
    python -m pip install -r scripts/requirements-dev.txt
"""

from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]


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


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--root",
        type=Path,
        default=REPO_ROOT,
        help="directory to check (default: the repository root)",
    )
    args = parser.parse_args()
    root = args.root.resolve()
    if not root.is_dir():
        sys.exit(f"license-check: {root} is not a directory")

    cmd = reuse_command() + ["--root", str(root), "lint"]
    result = subprocess.run(cmd, check=False)
    if result.returncode != 0:
        # REUSE-IgnoreStart
        print(
            "\nlicense-check: FAILED. Every file needs an SPDX header, e.g.:\n"
            "  // SPDX-FileCopyrightText: 2026 Your Name\n"
            "  // SPDX-License-Identifier: AGPL-3.0-or-later\n"
            "Files that cannot carry a comment are annotated in REUSE.toml.\n"
            "See CONTRIBUTING.md.",
            file=sys.stderr,
        )
        # REUSE-IgnoreEnd
    return result.returncode


if __name__ == "__main__":
    sys.exit(main())
