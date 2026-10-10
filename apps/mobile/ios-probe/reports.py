# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later
"""Puts the memory probe's reports back together from the unified log.

The app and its extension log each JSON report in chunks (Probe.swift,
ProbeLog.report): "report SOURCE ID INDEX/COUNT CHUNK". This script reads
the log of "log show --style ndjson" and:

  reports.py count LOG SOURCE N   exits 0 if N complete reports from SOURCE
                                  (app or nse) are in the log, 1 otherwise
  reports.py write LOG OUTPUT_DIR writes one JSON file per report of the
                                  log, and report.md: a summary of the peaks
                                  per configuration and a table of every
                                  step, for the reports of the log and the
                                  bare processes' reports already in
                                  OUTPUT_DIR (process-REP-N.json)
"""

from __future__ import annotations

import json
import re
import statistics
import sys
from pathlib import Path

LINE = re.compile(r"^report (\w+) (\w+) (\d+)/(\d+) (.*)$", re.DOTALL)
MIB = 1024 * 1024


def read_reports(log: Path) -> list[tuple[str, dict]]:
    """Returns the complete reports of the log as (source, report), in the
    order they were logged."""
    chunks: dict[str, dict[int, str]] = {}
    meta: dict[str, tuple[str, int]] = {}
    order: list[str] = []
    for raw in log.read_text(encoding="utf-8", errors="replace").splitlines():
        try:
            entry = json.loads(raw)
        except json.JSONDecodeError:
            continue
        match = LINE.match(entry.get("eventMessage", ""))
        if not match:
            continue
        source, report_id, index, count, chunk = match.groups()
        if report_id not in meta:
            meta[report_id] = (source, int(count))
            order.append(report_id)
        chunks.setdefault(report_id, {})[int(index)] = chunk
    reports = []
    for report_id in order:
        source, count = meta[report_id]
        parts = chunks[report_id]
        if len(parts) != count:
            continue
        text = "".join(parts[i] for i in range(1, count + 1))
        try:
            reports.append((source, json.loads(text)))
        except json.JSONDecodeError:
            reports.append((source, {"error": "unreadable report", "raw": text}))
    return reports


def mib(value: int) -> str:
    return "?" if value is None or value < 0 else f"{value / MIB:.1f}"


def label(source: str, report: dict) -> str:
    if report.get("goos") == "none":
        return f"{source}: empty C program"
    config = report.get("config", {})
    parts = [name for name in ("core", "crypto") if config.get(name)]
    if config.get("memory_limit_mb"):
        parts.append(f"GOMEMLIMIT {config['memory_limit_mb']} MiB")
    return f"{source}: {' + '.join(parts) or 'Go runtime only'}"


def peak(report: dict) -> int:
    values = [step.get("peak_footprint_bytes", -1) for step in report.get("steps", [])]
    return max(values, default=-1)


def process_reports(output: Path) -> list[tuple[str, dict]]:
    """The bare processes' reports, in the order they ran."""
    def key(path: Path) -> tuple[int, int]:
        _, rep, n = path.stem.split("-")
        return int(rep), int(n)

    reports = []
    for path in sorted(output.glob("process-*-*.json"), key=key):
        try:
            reports.append(("process", json.loads(path.read_text(encoding="utf-8"))))
        except json.JSONDecodeError:
            reports.append(("process", {"error": f"unreadable report {path.name}"}))
    return reports


def write(log: Path, output: Path) -> None:
    logged = read_reports(log) if log.exists() else []
    for n, (source, report) in enumerate(logged, 1):
        (output / f"report-{n}-{source}.json").write_text(json.dumps(report, indent=2), encoding="utf-8")
    reports = process_reports(output) + logged

    peaks: dict[str, list[int]] = {}
    for source, report in reports:
        if not report.get("error"):
            peaks.setdefault(label(source, report), []).append(peak(report))
    lines = [
        "### Peak footprint per run (MiB)",
        "",
        "| Run | SQLite | Runs | Min | Median | Max |",
        "|---|---|---:|---:|---:|---:|",
    ]
    drivers = {label(s, r): r.get("sqlite_driver", "?") for s, r in reports}
    for name, values in peaks.items():
        lines.append(
            f"| {name} | {drivers.get(name, '?')} | {len(values)} | {mib(min(values))} "
            f"| {mib(int(statistics.median(values)))} | {mib(max(values))} |"
        )
    lines += [
        "",
        "### Every step",
        "",
        "| Run | Step | Footprint (MiB) | Peak (MiB) | Limit left (MiB) | Go mapped (MiB) | Go heap (MiB) | Goroutines | ms |",
        "|---|---|---:|---:|---:|---:|---:|---:|---:|",
    ]
    for source, report in reports:
        if report.get("error"):
            lines.append(f"| {label(source, report)} | error: {report['error']} | | | | | | | |")
        for step in report.get("steps", []):
            lines.append(
                f"| {label(source, report)} | {step['step']} "
                f"| {mib(step['footprint_bytes'])} | {mib(step['peak_footprint_bytes'])} "
                f"| {mib(step['limit_remaining_bytes'])} | {mib(step['go_mapped_bytes'])} "
                f"| {mib(step['go_heap_objects_bytes'])} | {step['goroutines']} | {step['duration_ms']:.0f} |"
            )
    go = next((r for _, r in reports if r.get("go_version")), None)
    if go:
        lines += ["", f"{go['go_version']} {go['goos']}/{go['goarch']}, GOMAXPROCS {go['gomaxprocs']}."]
    extension = output / "extension.txt"
    if extension.exists():
        lines += ["", f"Notification Service Extension through simctl push: {extension.read_text(encoding='utf-8').strip()}."]
    (output / "report.md").write_text("\n".join(lines) + "\n", encoding="utf-8")


def main(argv: list[str]) -> int:
    if len(argv) == 5 and argv[1] == "count":
        found = sum(1 for source, _ in read_reports(Path(argv[2])) if source == argv[3])
        return 0 if found >= int(argv[4]) else 1
    if len(argv) == 4 and argv[1] == "write":
        write(Path(argv[2]), Path(argv[3]))
        return 0
    print(__doc__, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
