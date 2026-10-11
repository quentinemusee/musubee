// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package signal runs mautrix-signal's network connector inside the core,
// "on device": the core talks to Signal's servers itself, linked to the
// user's phone as a secondary device (docs/ADR/0025-signal-on-device.md).
//
// The connector needs libsignal's C library (libsignal-ffi, built from Rust),
// so it is only compiled with the build tag musubee_signal and cgo; without
// them, New returns ErrNotBuilt and the core does not offer Signal.
package signal

import "errors"

// Config holds what the core needs to connect to Signal.
type Config struct {
	// Environment is the Signal service: "production" (the default) or
	// "staging", whose accounts are separate and which the tests use.
	Environment string
}

// DeviceName is how the core appears in the list of linked devices of the
// user's Signal app.
const DeviceName = "Musubee"

// ErrNotBuilt is returned by New when the core was built without Signal.
var ErrNotBuilt = errors.New("this core was built without Signal (build tag musubee_signal)")

// ErrEnvironmentChanged is returned by New when an earlier connector of the
// same process chose another Signal environment: signalmeow keeps the
// environment in package variables, so one process talks to one service.
var ErrEnvironmentChanged = errors.New("signal: the environment is already set to another service in this process")
