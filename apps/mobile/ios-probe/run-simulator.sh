#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Runs the memory probe on a booted simulator, in three kinds of process:
#
# 1. Bare processes (simctl spawn): the probe built as an iOS executable,
#    with and without the core linked in (memprobe_nocore), one new process
#    per configuration and repetition, next to an empty C program
#    (control/footprint.c). Closest to a notification extension that
#    the simulator can run: no UIKit, nothing but the probe.
# 2. The probe app (UIKit), launched with --probe.
# 3. Its Notification Service Extension, through one simctl push. On the
#    simulators tried so far, simctl push delivers the notification without
#    running the extension (docs/ADR/0015-ios-nse-memory.md); the script
#    records whether it ran and does not fail when it did not.
#
# Collects every report into OUTPUT_DIR (JSON files and report.md). Needs
# macOS with Xcode, Go, XcodeGen and python3, and
# build/MusubeeMemProbe.xcframework (build-go-xcframework.sh, built with the
# same GO_TAGS).
#
#   run-simulator.sh UDID OUTPUT_DIR [GO_TAGS]
set -euo pipefail

if [ $# -lt 2 ]; then
	sed -n '5,22p' "$0" >&2
	exit 2
fi
udid=$1
output=$2
tags=${3:-}
here=$(cd "$(dirname "$0")" && pwd)
core=$(cd "$here/../../../core" && pwd)
mkdir -p "$output"
output=$(cd "$output" && pwd)
bundle=app.musubee.memoryprobe
nse_process=MemoryProbeNSE
repetitions=${MUSUBEE_PROBE_REPETITIONS:-3}
sdk_path=$(xcrun --sdk iphonesimulator --show-sdk-path)
cc=$(xcrun --sdk iphonesimulator --find clang)
min_flag=-mios-simulator-version-min=15.0
mkdir -p "$output/bin"

# 1. Bare processes.
# build_probe NAME TAGS: the probe as an iOS simulator executable.
build_probe() {
	(
		cd "$core"
		CGO_ENABLED=1 GOOS=ios GOARCH=arm64 CC="$cc" \
			CGO_CFLAGS="-isysroot $sdk_path -arch arm64 $min_flag -O2" \
			CGO_LDFLAGS="-isysroot $sdk_path -arch arm64 $min_flag" \
			go build -trimpath -ldflags="-s -w" -tags="$2" -o "$output/bin/$1" ./cmd/memprobe
	)
}
echo "== building the probe and the control for the simulator"
build_probe memprobe "$tags"
# Without the core: the Go runtime with goolm alone, the smallest program an
# extension written in Go could be.
build_probe memprobe-nocore "${tags:+$tags,}memprobe_nocore"
"$cc" -isysroot "$sdk_path" -arch arm64 "$min_flag" -O2 -o "$output/bin/footprint" "$here/control/footprint.c"
ls -l "$output/bin"

configs=(
	""
	"-crypto"
	"-core"
	"-core -crypto"
	"-core -crypto -memory-limit-mb 8"
)
for rep in $(seq 1 "$repetitions"); do
	xcrun simctl spawn "$udid" "$output/bin/footprint" >"$output/process-$rep-0.json"
	i=0
	for config in "${configs[@]}"; do
		i=$((i + 1))
		# shellcheck disable=SC2086 # the configuration is a list of flags
		xcrun simctl spawn "$udid" "$output/bin/memprobe" -data-dir "$output/data" $config \
			>"$output/process-$rep-$i.json"
	done
	for config in "" "-crypto"; do
		i=$((i + 1))
		# shellcheck disable=SC2086 # the configuration is a list of flags
		xcrun simctl spawn "$udid" "$output/bin/memprobe-nocore" $config >"$output/process-$rep-$i.json"
	done
	echo "== bare processes, repetition $rep done"
done

# 2. The app.
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

# wait_reports SOURCE COUNT SECONDS: waits until COUNT complete reports from
# SOURCE (app or nse) are in the log.
wait_reports() {
	local source=$1 count=$2 seconds=$3
	local deadline=$((SECONDS + seconds))
	while [ "$SECONDS" -lt "$deadline" ]; do
		logs >"$output/log.ndjson"
		if python3 "$here/reports.py" count "$output/log.ndjson" "$source" "$count"; then
			return 0
		fi
		sleep 2
	done
	return 1
}

xcrun simctl launch "$udid" "$bundle" --probe
if ! wait_reports app 1 120; then
	echo "no report from the app within 120 s" >&2
	exit 1
fi
echo "== app report received"
xcrun simctl terminate "$udid" "$bundle" || true

# 3. The extension, if the simulator runs it.
python3 - "$output/push.json" "$bundle" <<'PY'
import json, sys
path, bundle = sys.argv[1:3]
payload = {
    "Simulator Target Bundle": bundle,
    "aps": {"alert": {"title": "Memory probe", "body": "running"}, "mutable-content": 1},
}
with open(path, "w", encoding="utf-8") as f:
    json.dump(payload, f)
PY
xcrun simctl push "$udid" "$bundle" "$output/push.json"
if wait_reports nse 1 45; then
	echo "== the extension ran and reported"
	echo "ran" >"$output/extension.txt"
else
	echo "== the extension did not run (simctl push delivered the notification without it)"
	echo "did not run" >"$output/extension.txt"
	xcrun simctl spawn "$udid" log show --start "$start" --style compact --info \
		--predicate "process == \"$nse_process\" OR (process == \"SpringBoard\" AND eventMessage CONTAINS \"$bundle\")" \
		>"$output/extension-log.txt" 2>&1 || true
fi

python3 "$here/reports.py" write "$output/log.ndjson" "$output"
cat "$output/report.md"
