// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build cgo

// Command ffi is the core as a C shared library (musubee.h):
//
//	go build -buildmode=c-shared -o musubee.dll ./core/ffi
//
// The exported functions live in ffi.go. The package needs cgo: without it,
// it has no files to build, and ./core/... still builds for every target
// with CGO_ENABLED=0 (an empty program would not link for iOS).
package main

// main is required by -buildmode=c-shared and never runs in the library.
func main() {}
