// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux && !(darwin && cgo)

package memprobe

// OSFootprint is Unknown on this platform (or on Apple platforms without
// cgo, which the Mach call needs).
func OSFootprint() (current, peak, limitRemaining int64) { return Unknown() }
