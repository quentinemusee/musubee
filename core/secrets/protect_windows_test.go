// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package secrets

import "testing"

// TestDPAPIBlobNeedsTheEntropy checks that the key file is tied to Musubee:
// DPAPI refuses it with another entropy.
func TestDPAPIBlobNeedsTheEntropy(t *testing.T) {
	blob, _, err := protect([]byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := unprotect(ProtectionDPAPI, blob); err != nil || string(got) != "key" {
		t.Fatalf("unprotect = %q, %v", got, err)
	}
	saved := dpapiEntropy
	defer func() { dpapiEntropy = saved }()
	dpapiEntropy = []byte("another program")
	if _, err := unprotect(ProtectionDPAPI, blob); err == nil {
		t.Error("DPAPI opened the key with another entropy")
	}
}
