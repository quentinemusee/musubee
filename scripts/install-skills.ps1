# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later

# Installs Musubee's third-party skills into .claude/skills/ (project level).
# The list lives in scripts/skills.txt, shared with install-skills.bat and
# install-skills.sh.
#
# Usage (from anywhere):
#   powershell -ExecutionPolicy Bypass -File scripts/install-skills.ps1
#   powershell -ExecutionPolicy Bypass -File scripts/install-skills.ps1 --dry-run
# Prerequisites: Node.js (npx) and git.
#
# Third-party skills are content written by others: read each SKILL.md before
# enabling it. If a repository renames a skill, its command fails and the
# summary at the end lists it.
#
# Keep this file pure ASCII: Windows PowerShell 5.1 reads BOM-less files in
# the ANSI code page.

$ErrorActionPreference = "Stop"
$DryRun = ($args -contains "--dry-run") -or ($args -contains "-DryRun")

# The skills CLI reports each install (source and skill names) to skills.sh
# unless DISABLE_TELEMETRY or DO_NOT_TRACK is set. Opt out for this process.
$env:DISABLE_TELEMETRY = "1"
Write-Host "Telemetry to skills.sh disabled (DISABLE_TELEMETRY=$env:DISABLE_TELEMETRY)."

$RepoRoot = Split-Path -Parent $PSScriptRoot
$Manifest = Join-Path $PSScriptRoot "skills.txt"
Set-Location $RepoRoot

if (-not $DryRun -and -not (Get-Command npx -ErrorAction SilentlyContinue)) {
    Write-Host "npx not found: install Node.js first (https://nodejs.org)." -ForegroundColor Red
    exit 1
}

$failed = @()
foreach ($raw in Get-Content -LiteralPath $Manifest -Encoding UTF8) {
    $line = $raw.Trim()
    if ($line -eq "" -or $line.StartsWith("#")) { continue }

    $fields = $line -split "\s+"
    $arguments = @("skills", "add", $fields[0])
    if ($fields.Count -gt 1) {
        foreach ($skill in $fields[1..($fields.Count - 1)]) {
            $arguments += @("--skill", $skill)
        }
    }
    $arguments += @("-a", "claude-code", "-y")
    $display = "npx " + ($arguments -join " ")

    Write-Host ""
    Write-Host ">> $display" -ForegroundColor Cyan
    if ($DryRun) { continue }

    & npx @arguments
    if ($LASTEXITCODE -ne 0) { $failed += $display }
}

Write-Host ""
if ($DryRun) {
    Write-Host "Dry run: nothing was installed."
    exit 0
}
if ($failed.Count -gt 0) {
    Write-Host "Failed commands (skill name or option to fix):" -ForegroundColor Red
    $failed | ForEach-Object { Write-Host "  $_" }
    exit 1
}
Write-Host "Everything is installed in .claude/skills/." -ForegroundColor Green
Write-Host "Check with: npx skills list   then restart the Claude Code session."
