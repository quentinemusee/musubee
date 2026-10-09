// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package webp stands in for go.mau.fi/webp without cgo. It declares the
// part of its API that the core's dependencies use, and encodes nothing:
// mautrix-telegram only calls Encode to turn a PNG or JPEG sticker sent from
// Musubee into WebP, and that send fails with ErrUnsupported instead (see
// docs/ADR/0014-telegram-on-device.md).
package webp

import (
	"errors"
	"image"
	"io"
)

// ErrUnsupported is returned by every encoder of this build.
var ErrUnsupported = errors.New("webp: encoding is not available in this build of Musubee")

// Options mirrors the options of go.mau.fi/webp.
type Options struct {
	Lossless bool
	Quality  float32 // 0 ~ 100
	Exact    bool    // Preserve RGB values in transparent area.
}

// Encode always fails with ErrUnsupported.
func Encode(io.Writer, image.Image, *Options) error {
	return ErrUnsupported
}
