// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package mobile exposes the core to gomobile bind, which generates Java
// (Android) and Objective-C (iOS) bindings from exported Go declarations.
//
// T1.3 compares this binding with the C shared library and its JNI layer
// (core/ffi): see docs/ADR/0011-core-on-android.md. The API mirrors core/ffi:
// JSON requests, responses and events, as defined by core/embedded. gomobile
// only binds a few types (numbers, strings, []byte, errors, structs and
// interfaces of those), hence the byte slices and the millisecond timeout.
package mobile

import (
	"time"

	"github.com/quentinemusee/musubee/core/embedded"
)

// Core is one running core.
type Core struct {
	core *embedded.Core
}

// Open starts a core from a JSON configuration (see embedded.Config).
func Open(config []byte) (*Core, error) {
	c, err := embedded.Open(config)
	if err != nil {
		return nil, err
	}
	return &Core{core: c}, nil
}

// Call runs one JSON request and returns the JSON response.
func (c *Core) Call(request []byte) []byte {
	return c.core.Call(request)
}

// NextEvent waits up to timeoutMS milliseconds for the next JSON event. It
// returns nil on timeout, and {"type":"closed"} once the core is closed.
func (c *Core) NextEvent(timeoutMS int32) []byte {
	evt, ok := c.core.NextEvent(time.Duration(timeoutMS) * time.Millisecond)
	if !ok {
		return nil
	}
	return evt
}

// Close stops the core. It is safe to call more than once.
func (c *Core) Close() error {
	return c.core.Close()
}
