// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package webp

import (
	"bytes"
	"errors"
	"image"
	"testing"
)

func TestEncodeIsUnsupported(t *testing.T) {
	var out bytes.Buffer
	err := Encode(&out, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Encode returned %v, want ErrUnsupported", err)
	}
	if out.Len() != 0 {
		t.Fatalf("Encode wrote %d bytes", out.Len())
	}
}
