# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later

# Installs the caveman plugin (short answers) for Claude Code, at user level.
# Same commands as install-plugins.bat and install-plugins.sh.
#
# Usage (from anywhere):
#   powershell -ExecutionPolicy Bypass -File scripts/install-plugins.ps1
#   powershell -ExecutionPolicy Bypass -File scripts/install-plugins.ps1 --dry-run
# Prerequisite: Claude Code installed (the `claude` command is available).
#
# OFFICIAL SOURCE ONLY: JuliusBrussee/caveman (many copies exist elsewhere).
# Two separate commands because '&&' does not exist in Windows PowerShell 5.1.
# This plugin adds hooks (code run at session start and on every message):
# see the audit in docs/SKILLS.md.
# DO NOT install the proxy, caveman-code or the 'irm | iex' installer from the
# same project.
#
# Keep this file pure ASCII: Windows PowerShell 5.1 reads BOM-less files in
# the ANSI code page.

$ErrorActionPreference = "Stop"
$DryRun = ($args -contains "--dry-run") -or ($args -contains "-DryRun")

if (-not $DryRun -and -not (Get-Command claude -ErrorAction SilentlyContinue)) {
    Write-Host "claude not found: install Claude Code first." -ForegroundColor Red
    exit 1
}

$commands = @(
    @("plugin", "marketplace", "add", "JuliusBrussee/caveman"),
    @("plugin", "install", "caveman@caveman")
)

$failed = $false
foreach ($arguments in $commands) {
    $display = "claude " + ($arguments -join " ")
    Write-Host ""
    Write-Host ">> $display" -ForegroundColor Cyan
    if ($DryRun) { continue }

    & claude @arguments
    if ($LASTEXITCODE -ne 0) {
        Write-Host "FAILED: $display" -ForegroundColor Red
        $failed = $true
    }
}

Write-Host ""
if ($DryRun) {
    Write-Host "Dry run: nothing was installed."
    exit 0
}
if ($failed) {
    Write-Host "At least one command failed (see above)." -ForegroundColor Red
    exit 1
}
Write-Host "Plugin installed. Restart the Claude Code session, then check with: claude plugin list" -ForegroundColor Green
