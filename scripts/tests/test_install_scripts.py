# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later

"""Tests for the skill and plugin installers (.ps1, .bat and .sh variants).

The three variants must run exactly the same commands. Each test runs a real
script in its real shell (Windows PowerShell or PowerShell 7, cmd, POSIX sh)
in dry-run mode, from a directory outside the repository, and compares the
commands it prints (lines starting with ">> ") with the expected list.
Shells that are not installed are skipped (cmd only exists on Windows).

The real installation (network, npx, claude) is not run here: it is done by
hand in T0.2.

Run from the repository root:
    python -m unittest discover -s scripts/tests -v
"""

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPTS = REPO_ROOT / "scripts"

PLUGIN_COMMANDS = [
    "claude plugin marketplace add JuliusBrussee/caveman",
    "claude plugin install caveman@caveman",
]


def expected_skill_commands() -> list[str]:
    commands = []
    for raw in (SCRIPTS / "skills.txt").read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        source, *skills = line.split()
        parts = ["npx", "skills", "add", source]
        for skill in skills:
            parts += ["--skill", skill]
        parts += ["-a", "claude-code", "-y"]
        commands.append(" ".join(parts))
    return commands


def powershell() -> str | None:
    return shutil.which("powershell") or shutil.which("pwsh")


def posix_sh() -> str | None:
    if os.name == "nt":
        # Prefer Git for Windows' sh; C:\Windows\System32\bash.exe is WSL.
        git = shutil.which("git")
        if git:
            for candidate in (
                Path(git).parents[1] / "usr" / "bin" / "sh.exe",
                Path(git).parents[1] / "bin" / "sh.exe",
            ):
                if candidate.is_file():
                    return str(candidate)
        found = shutil.which("sh")
        return found if found and "system32" not in found.lower() else None
    return shutil.which("sh")


def command_line(variant: str, script: str, *args: str) -> list[str] | None:
    path = SCRIPTS / f"{script}.{variant}"
    if variant == "ps1":
        exe = powershell()
        return [exe, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", str(path), *args] if exe else None
    if variant == "bat":
        return ["cmd.exe", "/d", "/c", str(path), *args] if os.name == "nt" else None
    exe = posix_sh()
    return [exe, str(path), *args] if exe else None


def tool_free_env(cmd: list[str]) -> dict[str, str]:
    """Environment whose PATH holds only the shell and system directories.

    npx and claude are then unreachable, so a script that ignored --dry-run
    could not install anything for real: it would fail instead.
    """
    shell_dir = str(Path(cmd[0]).parent) if os.path.isabs(cmd[0]) else ""
    if os.name == "nt":
        root = os.environ.get("SystemRoot", r"C:\Windows")
        system = [root, os.path.join(root, "System32"),
                  os.path.join(root, "System32", "WindowsPowerShell", "v1.0")]
    else:
        system = ["/usr/bin", "/bin"]
    path = os.pathsep.join(p for p in [shell_dir, *system] if p)
    return dict(os.environ, PATH=path)


def printed_commands(output: str) -> list[str]:
    return [line[3:].strip() for line in output.splitlines() if line.startswith(">> ")]


class InstallScriptsTest(unittest.TestCase):
    def run_script(self, variant: str, script: str, *args: str):
        cmd = command_line(variant, script, *args)
        if cmd is None:
            self.skipTest(f"no shell available for .{variant} on this machine")
        env = tool_free_env(cmd)
        for tool in ("npx", "claude"):
            if shutil.which(tool, path=env["PATH"]):
                self.skipTest(f"{tool} is in a system directory; cannot run safely")
        with tempfile.TemporaryDirectory(prefix="musubee-install-") as outside:
            return subprocess.run(
                cmd,
                cwd=outside,  # the scripts must find the repository by themselves
                capture_output=True,
                text=True,
                encoding="utf-8",
                errors="replace",
                env=env,
                timeout=120,
            )

    def assert_dry_run(self, variant: str, script: str, expected: list[str]) -> None:
        result = self.run_script(variant, script, "--dry-run")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(printed_commands(result.stdout), expected, result.stdout)

    def test_skill_installers_disable_telemetry(self) -> None:
        # The skills CLI reports installs to skills.sh unless told not to.
        for variant in ("ps1", "bat", "sh"):
            with self.subTest(variant=variant):
                result = self.run_script(variant, "install-skills", "--dry-run") if command_line(
                    variant, "install-skills"
                ) else None
                if result is None:
                    continue
                self.assertIn("DISABLE_TELEMETRY=1", result.stdout)

    def test_manifest_lists_the_expected_sources(self) -> None:
        commands = expected_skill_commands()
        self.assertEqual(len(commands), 10)
        self.assertIn(
            "npx skills add Drjacky/claude-android-ninja -a claude-code -y", commands
        )

    def test_skills_ps1_dry_run(self) -> None:
        self.assert_dry_run("ps1", "install-skills", expected_skill_commands())

    def test_skills_bat_dry_run(self) -> None:
        self.assert_dry_run("bat", "install-skills", expected_skill_commands())

    def test_skills_sh_dry_run(self) -> None:
        self.assert_dry_run("sh", "install-skills", expected_skill_commands())

    def test_plugins_ps1_dry_run(self) -> None:
        self.assert_dry_run("ps1", "install-plugins", PLUGIN_COMMANDS)

    def test_plugins_bat_dry_run(self) -> None:
        self.assert_dry_run("bat", "install-plugins", PLUGIN_COMMANDS)

    def test_plugins_sh_dry_run(self) -> None:
        self.assert_dry_run("sh", "install-plugins", PLUGIN_COMMANDS)

    def test_missing_tool_fails_cleanly(self) -> None:
        # Without npx / claude on PATH, a real run must stop with an error
        # instead of reporting success.
        for variant in ("ps1", "bat", "sh"):
            for script, tool in (("install-skills", "npx"), ("install-plugins", "claude")):
                with self.subTest(variant=variant, script=script):
                    if command_line(variant, script) is None:
                        continue
                    result = self.run_script(variant, script)
                    self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertIn(f"{tool} not found", result.stdout + result.stderr)
                    self.assertEqual(printed_commands(result.stdout), [])

    def test_windows_scripts_are_ascii(self) -> None:
        # Windows PowerShell 5.1 reads BOM-less files in the ANSI code page,
        # and cmd uses the OEM code page: keep both script types pure ASCII.
        for path in sorted(SCRIPTS.glob("*.ps1")) + sorted(SCRIPTS.glob("*.bat")):
            with self.subTest(path=path.name):
                data = path.read_bytes()
                self.assertTrue(data.isascii(), f"{path.name} contains non-ASCII bytes")


if __name__ == "__main__":
    unittest.main()
