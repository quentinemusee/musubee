#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Quentin Raimbaud
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Builds a Go package of the core module as a C static library for iOS
# (go build -buildmode=c-archive, GOOS=ios) and packages it as an
# XCFramework with two slices: devices (iphoneos, arm64) and the simulator
# (iphonesimulator, arm64, the architecture of Apple silicon Macs). Needs
# macOS with Xcode and Go.
#
#   build-go-xcframework.sh PACKAGE HEADER MODULE OUTPUT_DIR [GO_TAGS]
#
# PACKAGE is relative to core/ (./cmd/memprobe), HEADER is the C header of
# its exports, MODULE names the library, the Clang module that Swift imports
# and the XCFramework (OUTPUT_DIR/MODULE.xcframework). GO_TAGS is passed to
# go build -tags. MUSUBEE_IOS_MIN sets the minimum iOS version (15.0).
set -euo pipefail

if [ $# -lt 4 ]; then
	sed -n '5,16p' "$0" >&2
	exit 2
fi
package=$1
header=$2
module=$3
output=$4
tags=${5:-}
min_ios=${MUSUBEE_IOS_MIN:-15.0}

core=$(cd "$(dirname "$0")/../../../core" && pwd)
header=$(cd "$(dirname "$header")" && pwd)/$(basename "$header")
mkdir -p "$output"
output=$(cd "$output" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# One slice: the SDK decides the platform. cgo compiles C for it with the
# SDK's clang, and Go's linker reads the platform (device or simulator) from
# those objects.
build_slice() {
	local sdk=$1 min_flag=$2
	local sdk_path cc
	sdk_path=$(xcrun --sdk "$sdk" --show-sdk-path)
	cc=$(xcrun --sdk "$sdk" --find clang)
	mkdir -p "$work/$sdk"
	echo "== $module for $sdk"
	(
		cd "$core"
		CGO_ENABLED=1 GOOS=ios GOARCH=arm64 CC="$cc" \
			CGO_CFLAGS="-isysroot $sdk_path -arch arm64 $min_flag -O2" \
			CGO_LDFLAGS="-isysroot $sdk_path -arch arm64 $min_flag" \
			go build -buildmode=c-archive -trimpath -ldflags="-s -w" -tags="$tags" \
			-o "$work/$sdk/lib$module.a" "$package"
	)
	ls -l "$work/$sdk/lib$module.a"
}
build_slice iphoneos "-miphoneos-version-min=$min_ios"
build_slice iphonesimulator "-mios-simulator-version-min=$min_ios"

# Swift imports the header through a Clang module.
mkdir -p "$work/headers"
cp "$header" "$work/headers/"
cat >"$work/headers/module.modulemap" <<MODULEMAP
module $module {
    header "$(basename "$header")"
    export *
}
MODULEMAP

rm -rf "$output/$module.xcframework"
xcodebuild -create-xcframework \
	-library "$work/iphoneos/lib$module.a" -headers "$work/headers" \
	-library "$work/iphonesimulator/lib$module.a" -headers "$work/headers" \
	-output "$output/$module.xcframework"
echo "== $output/$module.xcframework"
