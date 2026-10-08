#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Measures the core on Android (docs/ADR/0011): installs the app and its
# instrumented tests, then runs the "measurements" test RUNS times, each in
# a fresh process, and prints one JSON line per run.
#
# Usage: ANDROID_SERIAL=emulator-5554 ./measure-core.sh [RUNS] [ABIS]
# ANDROID_SERIAL is required, so that a personal phone is never used by
# accident. ABIS defaults to the device's ABI.
set -euo pipefail
cd "$(dirname "$0")"

: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to measure (adb devices)}"
runs="${1:-5}"
adb="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}/platform-tools/adb"
command -v "$adb" >/dev/null 2>&1 || adb=adb
abis="${2:-$("$adb" shell getprop ro.product.cpu.abi | tr -d '\r')}"

./gradlew -q "-Pmusubee.abis=$abis" :app:installDebug :app:installDebugAndroidTest >&2
for _ in $(seq "$runs"); do
  "$adb" shell am force-stop app.musubee
  "$adb" logcat -c
  "$adb" shell am instrument -w -e class app.musubee.core.CoreLibraryTest#measurements \
    app.musubee.test/androidx.test.runner.AndroidJUnitRunner | grep -q "OK (1 test)" \
    || { echo "measurement run failed" >&2; exit 1; }
  # "logcat -c" does not always empty the buffer: keep the last line only.
  "$adb" logcat -d -s MusubeeBench:I | grep -o '{"pss_open_kib".*}' | tail -n 1
done
