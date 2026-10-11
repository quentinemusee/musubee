#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Runs Go test binaries built for Android (go test -c with GOOS=android) on a
# device or an emulator through adb, and fails when one of them fails
# (docs/ADR/0028). Each binary runs in its own directory under
# /data/local/tmp, with TMPDIR and HOME there, as the shell user.
#
#   scripts/run-android-go-tests.sh BINARY.test... [-- TEST_FLAGS...]
#
# ANDROID_SERIAL selects the device when several are connected; set it
# explicitly so that the tests never run on an unexpected phone. TEST_FLAGS
# go to every binary (for example -test.run Sealed); -test.v and
# -test.count=1 are always given. The binaries must not need files from
# their package directory: the tests of this repository's packages built
# this way (Signal's, ADR 0028) have no testdata.
set -euo pipefail

binaries=()
while [ $# -gt 0 ] && [ "$1" != "--" ]; do
  binaries+=("$1")
  shift
done
if [ $# -gt 0 ]; then shift; fi
if [ ${#binaries[@]} -eq 0 ]; then
  echo "usage: run-android-go-tests.sh BINARY.test... [-- TEST_FLAGS...]" >&2
  exit 2
fi

remote=/data/local/tmp/musubee-go-tests
adb shell rm -rf "$remote"
failed=()
for binary in "${binaries[@]}"; do
  name=$(basename "$binary")
  dir=$remote/${name%.test}
  adb shell mkdir -p "$dir/tmp"
  adb push "$binary" "$dir/$name" > /dev/null
  adb shell chmod 755 "$dir/$name"
  echo "=== $name"
  # adb returns the remote command's exit status (Android 7 and later).
  if ! adb shell "cd $dir && TMPDIR=$dir/tmp HOME=$dir ./$name -test.v -test.count=1 $*"; then
    failed+=("$name")
  fi
done
adb shell rm -rf "$remote"

if [ ${#failed[@]} -gt 0 ]; then
  echo "FAILED on the device: ${failed[*]}" >&2
  exit 1
fi
echo "all ${#binaries[@]} test binaries passed on the device"
