# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later

# Registers the maintainer's phone number on Signal's STAGING service with
# signal-cli, as the primary device of the Signal test account (ADR 0022,
# ADR 0025). A number registered on staging is a separate account from the
# same number on Signal's public service: this does not touch the
# maintainer's real Signal account, and the Signal app on their phone keeps
# working.
#
# Usage (the maintainer runs it; it asks for the captcha and the SMS code):
#   powershell -ExecutionPolicy Bypass -File infra/signaltest/register-staging.ps1 `
#     -Number +33612345678 -DataDir D:\SDK\signal-staging-account
#
# -DataDir receives signal-cli's state: the test account's private keys. Keep
# it outside the repository, and never commit, paste or log its content.
# Prerequisites: signal-cli 0.14.9 or later (MUSUBEE_SIGNAL_CLI: path of its
# bin\signal-cli.bat) and Java 25 or later (JAVA_HOME).
#
# Keep this file pure ASCII: Windows PowerShell 5.1 reads BOM-less files in
# the ANSI code page.

param(
    [Parameter(Mandatory = $true)][string]$Number,
    [Parameter(Mandatory = $true)][string]$DataDir,
    [switch]$Voice
)

$ErrorActionPreference = "Stop"

if ($Number -notmatch '^\+[1-9][0-9]{6,14}$') {
    throw "The number must be in international format, for example +33612345678."
}
$cli = $env:MUSUBEE_SIGNAL_CLI
if (-not $cli -or -not (Test-Path $cli)) {
    throw "Set MUSUBEE_SIGNAL_CLI to the path of signal-cli's bin\signal-cli.bat."
}
if (-not $env:JAVA_HOME) {
    throw "Set JAVA_HOME to a Java 25 (or later) runtime."
}
$repo = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
New-Item -ItemType Directory -Force $DataDir | Out-Null
if ((Resolve-Path $DataDir).Path.StartsWith($repo, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "-DataDir must be outside the repository: it holds the test account's private keys."
}

function Invoke-SignalCli {
    & $cli --config $DataDir --service-environment staging -a $Number @args
    if ($LASTEXITCODE -ne 0) { throw "signal-cli $($args[0]) failed (exit code $LASTEXITCODE)." }
}

Write-Host "1. A browser opens Signal's STAGING captcha page. Solve it; the page then"
Write-Host "   shows an 'Open Signal' link. Right-click it, copy the link address"
Write-Host "   (it starts with signalcaptcha://) and paste it below."
Start-Process "https://signalcaptchas.org/staging/registration/generate.html"
$captcha = (Read-Host "Captcha link").Trim()
if ($captcha -notlike "signalcaptcha://*") {
    throw "That is not a captcha link (it must start with signalcaptcha://)."
}

if ($Voice) {
    Invoke-SignalCli register --voice --captcha $captcha
} else {
    Invoke-SignalCli register --captcha $captcha
}

Write-Host "2. Signal's staging service sends a code to $Number (SMS, or a call with -Voice)."
$code = (Read-Host "Verification code").Trim() -replace '[- ]', ''
Invoke-SignalCli verify $code

# A recognizable profile on the test account; the journeys do not depend on it.
Invoke-SignalCli updateProfile --given-name Musubee --family-name Staging

Write-Host ""
Write-Host "Done: $Number is registered on Signal staging, with its state in $DataDir."
Write-Host "Check it with: signal-cli --config <DataDir> --service-environment staging -a $Number listDevices"
Write-Host "Then tell Claude Code it is done. Do not paste anything from $DataDir in a chat."
