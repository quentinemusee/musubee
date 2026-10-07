#!/bin/sh
# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later

# Installs Musubee's third-party skills into .claude/skills/ (project level).
# The list lives in scripts/skills.txt, shared with install-skills.ps1 and
# install-skills.bat.
#
# Usage (from anywhere; Linux, macOS, Git Bash on Windows):
#   sh scripts/install-skills.sh
#   sh scripts/install-skills.sh --dry-run
# Prerequisites: Node.js (npx) and git.
#
# Third-party skills are content written by others: read each SKILL.md before
# enabling it. If a repository renames a skill, its command fails and the
# summary at the end lists it.

set -u
set -f  # no pathname expansion when splitting manifest lines

dry_run=0
if [ "${1:-}" = "--dry-run" ]; then
    dry_run=1
fi

# The skills CLI reports each install (source and skill names) to skills.sh
# unless DISABLE_TELEMETRY or DO_NOT_TRACK is set. Opt out for this process.
DISABLE_TELEMETRY=1
export DISABLE_TELEMETRY
echo "Telemetry to skills.sh disabled (DISABLE_TELEMETRY=$DISABLE_TELEMETRY)."

script_dir=$(cd "$(dirname "$0")" && pwd) || exit 1
repo_root=$(dirname "$script_dir")
manifest="$script_dir/skills.txt"
cd "$repo_root" || exit 1

if [ "$dry_run" -eq 0 ] && ! command -v npx >/dev/null 2>&1; then
    echo "npx not found: install Node.js first (https://nodejs.org)." >&2
    exit 1
fi

failed=""
# The manifest is read on file descriptor 3 so that npx cannot consume it
# through standard input.
while IFS= read -r line <&3 || [ -n "$line" ]; do
    line=$(printf '%s' "$line" | tr -d '\r')
    case "$line" in
        '' | '#'*) continue ;;
    esac

    # shellcheck disable=SC2086  # word splitting of the manifest line is intended
    set -- $line
    skill_source=$1
    shift
    command_line="npx skills add $skill_source"
    for skill in "$@"; do
        command_line="$command_line --skill $skill"
    done
    command_line="$command_line -a claude-code -y"

    printf '\n>> %s\n' "$command_line"
    if [ "$dry_run" -eq 1 ]; then
        continue
    fi

    # shellcheck disable=SC2086  # the command line is made of plain words
    if ! $command_line; then
        failed="$failed
  $command_line"
    fi
done 3< "$manifest"

echo
if [ "$dry_run" -eq 1 ]; then
    echo "Dry run: nothing was installed."
    exit 0
fi
if [ -n "$failed" ]; then
    echo "Failed commands (skill name or option to fix):$failed" >&2
    exit 1
fi
echo "Everything is installed in .claude/skills/."
echo "Check with: npx skills list   then restart the Claude Code session."
