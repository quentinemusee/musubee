#!/bin/sh
# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later

# Installs the caveman plugin (short answers) for Claude Code, at user level.
# Same commands as install-plugins.ps1 and install-plugins.bat.
#
# Usage (from anywhere; Linux, macOS, Git Bash on Windows):
#   sh scripts/install-plugins.sh
#   sh scripts/install-plugins.sh --dry-run
# Prerequisite: Claude Code installed (the `claude` command is available).
#
# OFFICIAL SOURCE ONLY: JuliusBrussee/caveman (many copies exist elsewhere).
# This plugin adds hooks (code run at session start and on every message):
# see the audit in docs/SKILLS.md.
# DO NOT install the proxy, caveman-code or the 'irm | iex' installer from the
# same project.

set -u

dry_run=0
if [ "${1:-}" = "--dry-run" ]; then
    dry_run=1
fi

if [ "$dry_run" -eq 0 ] && ! command -v claude >/dev/null 2>&1; then
    echo "claude not found: install Claude Code first." >&2
    exit 1
fi

failed=0
run() {
    printf '\n>> %s\n' "$*"
    if [ "$dry_run" -eq 1 ]; then
        return 0
    fi
    if ! "$@"; then
        echo "FAILED: $*" >&2
        failed=1
    fi
}

run claude plugin marketplace add JuliusBrussee/caveman
run claude plugin install caveman@caveman

echo
if [ "$dry_run" -eq 1 ]; then
    echo "Dry run: nothing was installed."
    exit 0
fi
if [ "$failed" -ne 0 ]; then
    echo "At least one command failed (see above)." >&2
    exit 1
fi
echo "Plugin installed. Restart the Claude Code session, then check with: claude plugin list"
