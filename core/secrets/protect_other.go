// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package secrets

import (
	"errors"
	"fmt"
)

// protect keeps the key as is: no secure storage is wired on this platform
// yet, so only the file's permissions protect it (docs/ADR/0019).
func protect(raw []byte) ([]byte, Protection, error) {
	return raw, ProtectionNone, nil
}

func unprotect(p Protection, blob []byte) ([]byte, error) {
	switch p {
	case ProtectionNone:
		return blob, nil
	case ProtectionDPAPI:
		return nil, errors.New("the key file was protected by Windows and cannot be read here")
	default:
		return nil, fmt.Errorf("unknown protection %q", p)
	}
}
