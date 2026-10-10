#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Builds the memory probe app for the simulator, installs it on a booted
# simulator and runs the probe: once in the app, then in the Notification
# Service Extension for each configuration below, each time in a new
# extension process. Collects the reports from the unified log into
# OUTPUT_DIR (one JSON file per run, plus report.md). Needs macOS with Xcode,
# XcodeGen and python3, and build/MusubeeMemProbe.xcframework
# (build-go-xcframework.sh).
#
#   run-simulator.sh UDID OUTPUT_DIR
set -euo pipefail

if [ $# -ne 2 ]; then
	sed -n '5,13p' "$0" >&2
	exit 2
fi
udid=$1
output=$2
here=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$output"
output=$(cd "$output" && pwd)
bundle=app.musubee.memoryprobe
nse_process=MemoryProbeNSE

cd "$here"
xcodegen generate
xcodebuild -project MemoryProbe.xcodeproj -scheme MemoryProbe -configuration Release \
	-sdk iphonesimulator -destination "id=$udid" -derivedDataPath build/derived \
	build >"$output/xcodebuild.log" 2>&1 || {
	tail -60 "$output/xcodebuild.log"
	exit 1
}
app=build/derived/Build/Products/Release-iphonesimulator/MemoryProbe.app
echo "== app built"
du -sh "$app" "$app/MemoryProbe" "$app/PlugIns/$nse_process.appex/$nse_process" | tee "$output/sizes.txt"

xcrun simctl install "$udid" "$app"
start=$(date '+%Y-%m-%d %H:%M:%S')

logs() {
	xcrun simctl spawn "$udid" log show --start "$start" --style ndjson --info \
		--predicate "subsystem == \"$bundle\"" 2>/dev/null || true
}

# wait_reports SOURCE COUNT: waits until COUNT complete reports from SOURCE
# (app or nse) are in the log.
wait_reports() {
	local source=$1 count=$2
	for _ in $(seq 1 90); do
		logs >"$output/log.ndjson"
		if python3 "$here/reports.py" count "$output/log.ndjson" "$source" "$count"; then
			return 0
		fi
		sleep 2
	done
	echo "no report $count from $source within 180 s; log so far:" >&2
	cat "$output/log.ndjson" >&2
	return 1
}

# The app asks for provisional notification authorization, then runs the
# probe in its own process.
xcrun simctl launch "$udid" "$bundle" --probe
wait_reports app 1
echo "== app report received"
xcrun simctl terminate "$udid" "$bundle" || true

# One push per configuration, each in a new extension process: iOS keeps an
# extension's process alive for a while after a push, and the peak
# footprint lives as long as the process. The simulator's processes are
# processes of the Mac, so pkill reaches the extension.
configs=(
	'{"core": true, "crypto": true}'
	'{"crypto": true}'
	'{"core": true}'
	'{"core": true, "crypto": true, "memory_limit_mb": 8}'
)
n=0
for config in "${configs[@]}"; do
	n=$((n + 1))
	pkill -9 -x "$nse_process" || true
	sleep 1
	python3 - "$output/push.json" "$bundle" "$config" <<'PY'
import json, sys
path, bundle, config = sys.argv[1:4]
payload = {
    "Simulator Target Bundle": bundle,
    "aps": {"alert": {"title": "Memory probe", "body": "running"}, "mutable-content": 1},
    "probe": config,
}
with open(path, "w", encoding="utf-8") as f:
    json.dump(payload, f)
PY
	xcrun simctl push "$udid" "$bundle" "$output/push.json"
	wait_reports nse "$n"
	echo "== extension report $n received ($config)"
done

python3 "$here/reports.py" write "$output/log.ndjson" "$output"
cat "$output/report.md"
