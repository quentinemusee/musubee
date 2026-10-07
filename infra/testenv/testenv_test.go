// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package testenv

import (
	"os"
	"path/filepath"
	"testing"
)

// The expected digests were computed independently with Python's hmac module,
// following the Synapse admin API documentation (HMAC-SHA1 over nonce, user,
// password and "admin"/"notadmin", separated by NUL bytes).
func TestRegistrationMAC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		admin bool
		want  string
	}{
		{name: "regular user", admin: false, want: "d62f4e740b74651d1a341bac1c1d8d3c2aa1fdca"},
		{name: "admin user", admin: true, want: "18186ddb218f510e91efda2159cbcb9e2db509e2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RegistrationMAC("test-secret", "abc123", "alice", "correct horse", tt.admin)
			if got != tt.want {
				t.Errorf("RegistrationMAC() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestParsePublishedPort(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		output  string
		want    string
		wantErr bool
	}{
		{name: "ipv4", output: "127.0.0.1:63422\n", want: "http://127.0.0.1:63422"},
		{name: "windows line ending", output: "127.0.0.1:8008\r\n", want: "http://127.0.0.1:8008"},
		{name: "first of several", output: "127.0.0.1:1234\n[::1]:1234\n", want: "http://127.0.0.1:1234"},
		{name: "empty", output: "\n", wantErr: true},
		{name: "no port", output: "127.0.0.1\n", wantErr: true},
		{name: "not a number", output: "127.0.0.1:http\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParsePublishedPort(tt.output)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParsePublishedPort(%q) error = %v, wantErr %v", tt.output, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParsePublishedPort(%q) = %q, want %q", tt.output, got, tt.want)
			}
		})
	}
}

func TestInfraDirContainsComposeFile(t *testing.T) {
	t.Parallel()
	dir, err := InfraDir()
	if err != nil {
		t.Fatalf("InfraDir() error = %v", err)
	}
	for _, name := range []string{ComposeFileName, filepath.Join("synapse", "homeserver.yaml")} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s in %s: %v", name, dir, err)
		}
	}
}

func TestNewSharedSecretIsRandomAndLongEnough(t *testing.T) {
	t.Parallel()
	a, err := newSharedSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, err := newSharedSecret()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two generated secrets are equal")
	}
	if len(a) < 64 {
		t.Errorf("secret length = %d, want at least 64 hex characters", len(a))
	}
}
