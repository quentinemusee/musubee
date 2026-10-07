@echo off
@rem SPDX-FileCopyrightText: 2026 Quentin Raimbaud
@rem SPDX-License-Identifier: AGPL-3.0-or-later

@rem Installs the caveman plugin (short answers) for Claude Code, at user level.
@rem Same commands as install-plugins.ps1 and install-plugins.sh.
@rem
@rem Usage (from anywhere, Windows cmd):
@rem   scripts\install-plugins.bat
@rem   scripts\install-plugins.bat --dry-run
@rem Prerequisite: Claude Code installed (the `claude` command is available).
@rem
@rem OFFICIAL SOURCE ONLY: JuliusBrussee/caveman (many copies exist elsewhere).
@rem This plugin adds hooks (code run at session start and on every message):
@rem see the audit in docs\SKILLS.md.
@rem DO NOT install the proxy, caveman-code or the 'irm | iex' installer from
@rem the same project.
@rem
@rem Keep this file pure ASCII with CRLF line endings (see .gitattributes).

setlocal EnableExtensions

set "DRY_RUN=0"
if /i "%~1"=="--dry-run" set "DRY_RUN=1"

if "%DRY_RUN%"=="0" (
    where claude >nul 2>nul
    if errorlevel 1 (
        echo claude not found: install Claude Code first.
        exit /b 1
    )
)

set "FAILED=0"
call :run claude plugin marketplace add JuliusBrussee/caveman
call :run claude plugin install caveman@caveman

echo.
if "%DRY_RUN%"=="1" (
    echo Dry run: nothing was installed.
    exit /b 0
)
if "%FAILED%"=="1" (
    echo At least one command failed ^(see above^).
    exit /b 1
)
echo Plugin installed. Restart the Claude Code session, then check with: claude plugin list
exit /b 0

:run
echo.
echo ^>^> %*
if "%DRY_RUN%"=="1" exit /b 0
call %*
if errorlevel 1 (
    echo FAILED: %*
    set "FAILED=1"
)
exit /b 0
