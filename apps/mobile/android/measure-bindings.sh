#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Measures both bindings of the core (docs/ADR/0011): installs the app and
# the instrumented tests of each flavor, then runs the "measurements" test
# RUNS times, each in a fresh process, and prints its JSON lines.
#
# Usage: ANDROID_SERIAL=emulator-5554 ./measure-bindings.sh [RUNS] [ABIS]
# ANDROID_SERIAL is required, so that a personal phone is never used by
# accident. ABIS defaults to the device's ABI.
set -euo pipefail
cd "$(dirname "$0")"

: "${ANDROID_SERIAL:?set ANDROID_SERIAL to the device to measure (adb devices)}"
runs="${1:-5}"
adb="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}/platform-tools/adb"
command -v "$adb" >/dev/null 2>&1 || adb=adb
abis="${2:-$("$adb" shell getprop ro.product.cpu.abi | tr -d '\r')}"

for flavor in jni gomobile; do
  Flavor="$(tr '[:lower:]' '[:upper:]' <<<"${flavor:0:1}")${flavor:1}"
  ./gradlew -q "-Pmusubee.abis=$abis" "install${Flavor}Debug" "install${Flavor}DebugAndroidTest" >&2
  package="app.musubee"
  [ "$flavor" = gomobile ] && package="app.musubee.gomobile"
  for _ in $(seq "$runs"); do
    "$adb" shell am force-stop "$package"
    "$adb" logcat -c
    "$adb" shell am instrument -w -e class app.musubee.core.CoreLibraryTest#measurements \
      "$package.test/androidx.test.runner.AndroidJUnitRunner" | grep -q "OK (1 test)" \
      || { echo "measurement run failed for $flavor" >&2; exit 1; }
    # "logcat -c" does not always empty the buffer: keep the last line only.
    "$adb" logcat -d -s MusubeeBench:I | grep -o '{"binding".*"goroutines".*}' | tail -n 1
  done
done
