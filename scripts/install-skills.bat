@echo off
@rem SPDX-FileCopyrightText: 2026 Quentin Raimbaud
@rem SPDX-License-Identifier: AGPL-3.0-or-later

@rem Installs Musubee's third-party skills into .claude/skills/ (project level).
@rem The list lives in scripts\skills.txt, shared with install-skills.ps1 and
@rem install-skills.sh.
@rem
@rem Usage (from anywhere, Windows cmd):
@rem   scripts\install-skills.bat
@rem   scripts\install-skills.bat --dry-run
@rem Prerequisites: Node.js (npx) and git.
@rem
@rem Third-party skills are content written by others: read each SKILL.md
@rem before enabling it. If a repository renames a skill, its command fails
@rem and the summary at the end lists it.
@rem
@rem Keep this file pure ASCII with CRLF line endings (see .gitattributes).

setlocal EnableExtensions EnableDelayedExpansion

set "DRY_RUN=0"
if /i "%~1"=="--dry-run" set "DRY_RUN=1"

set "SCRIPT_DIR=%~dp0"
set "MANIFEST=%SCRIPT_DIR%skills.txt"
pushd "%SCRIPT_DIR%.." || exit /b 1

if "%DRY_RUN%"=="0" (
    where npx >nul 2>nul
    if errorlevel 1 (
        echo npx not found: install Node.js first ^(https://nodejs.org^).
        popd
        exit /b 1
    )
)

set "FAILED=0"
for /f "usebackq eol=# tokens=1,*" %%A in ("%MANIFEST%") do (
    set "COMMAND=npx skills add %%A"
    for %%S in (%%B) do set "COMMAND=!COMMAND! --skill %%S"
    set "COMMAND=!COMMAND! -a claude-code -y"
    echo.
    echo ^>^> !COMMAND!
    if "%DRY_RUN%"=="0" (
        call !COMMAND!
        if errorlevel 1 (
            set "FAILED=1"
            echo FAILED: !COMMAND!
        )
    )
)

echo.
if "%DRY_RUN%"=="1" (
    echo Dry run: nothing was installed.
    popd
    exit /b 0
)
if "%FAILED%"=="1" (
    echo Some commands failed: see the FAILED lines above ^(skill name or option to fix^).
    popd
    exit /b 1
)
echo Everything is installed in .claude/skills/.
echo Check with: npx skills list   then restart the Claude Code session.
popd
exit /b 0
