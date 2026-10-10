// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package secrets

import (
	"errors"
	"fmt"
	"math"
	"unsafe"

	"golang.org/x/sys/windows"
)

// dpapiEntropy is mixed into the protection. It is not a secret (it is in
// this source code): it only ties the blob to Musubee. DPAPI protects the
// key from other users and from a copy of the disk, not from programs that
// run as the same user (docs/ADR/0019).
var dpapiEntropy = []byte("app.musubee.core master key v1")

// protect encrypts the key for the current Windows user with DPAPI.
func protect(raw []byte) ([]byte, Protection, error) {
	out, err := dpapi(raw, true)
	if err != nil {
		return nil, "", err
	}
	return out, ProtectionDPAPI, nil
}

func unprotect(p Protection, blob []byte) ([]byte, error) {
	switch p {
	case ProtectionDPAPI:
		return dpapi(blob, false)
	case ProtectionNone:
		return blob, nil
	default:
		return nil, fmt.Errorf("unknown protection %q", p)
	}
}

func dpapi(in []byte, encrypt bool) ([]byte, error) {
	if len(in) == 0 || len(in) > math.MaxUint32 {
		return nil, errors.New("nothing to protect, or too much")
	}
	inBlob := windows.DataBlob{Size: uint32(len(in)), Data: &in[0]}                      //nolint:gosec // Checked above.
	entropy := windows.DataBlob{Size: uint32(len(dpapiEntropy)), Data: &dpapiEntropy[0]} //nolint:gosec // A short constant.
	var out windows.DataBlob
	var err error
	if encrypt {
		err = windows.CryptProtectData(&inBlob, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptUnprotectData(&inBlob, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, fmt.Errorf("DPAPI: %w", err)
	}
	defer func() {
		_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data))) //nolint:gosec // DPAPI's output is freed with LocalFree.
	}()
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil //nolint:gosec // The buffer and size DPAPI returned.
}
