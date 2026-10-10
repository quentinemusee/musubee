// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package secrets

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSealRoundTrip(t *testing.T) {
	k := NewKey()
	s := NewSealer(k, "test")
	plain := []byte(`{"auth_key":"secret"}`)
	sealed := s.Seal(plain)
	if !IsSealed(sealed) || strings.Contains(sealed, "secret") {
		t.Fatalf("sealed = %q", sealed)
	}
	if again := s.Seal(plain); again == sealed {
		t.Error("two seals of one value are equal: the nonce is not random")
	}
	got, err := s.Open(sealed)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("Open = %q, %v", got, err)
	}
	if got, err := s.Open(s.Seal(nil)); err != nil || len(got) != 0 {
		t.Errorf("empty value: %q, %v", got, err)
	}
}

func TestOpenRefuses(t *testing.T) {
	k := NewKey()
	s := NewSealer(k, "test")
	sealed := s.Seal([]byte("value"))
	if _, err := NewSealer(NewKey(), "test").Open(sealed); err == nil {
		t.Error("another key opens the value")
	}
	if _, err := NewSealer(k, "other").Open(sealed); err == nil {
		t.Error("another purpose opens the value")
	}
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, SealedPrefix))
	raw[len(raw)-1] ^= 1
	if _, err := s.Open(SealedPrefix + base64.StdEncoding.EncodeToString(raw)); err == nil {
		t.Error("a modified value opens")
	}
	for _, bad := range []string{"value", SealedPrefix, SealedPrefix + "!!", SealedPrefix + "AAAA"} {
		if _, err := s.Open(bad); err == nil {
			t.Errorf("Open(%q) succeeded", bad)
		}
	}
	if _, err := s.Open(`{"plain":true}`); !errors.Is(err, ErrNotSealed) {
		t.Errorf("a plain value: %v", err)
	}
}

func TestDerive(t *testing.T) {
	k := NewKey()
	a, b := k.Derive("a"), k.Derive("b")
	if len(a) != KeySize || bytes.Equal(a, b) || bytes.Equal(a, k.b[:]) {
		t.Error("derived keys are not independent")
	}
	if !bytes.Equal(a, k.Derive("a")) {
		t.Error("derivation is not stable")
	}
}

func TestParseKey(t *testing.T) {
	k := NewKey()
	parsed, err := ParseKey(" " + base64.StdEncoding.EncodeToString(k.b[:]) + "\n")
	if err != nil || parsed != k {
		t.Fatalf("ParseKey = %v", err)
	}
	for _, bad := range []string{"", "not base64", base64.StdEncoding.EncodeToString(make([]byte, 16))} {
		if _, err := ParseKey(bad); err == nil || strings.Contains(err.Error(), bad) && bad != "" {
			t.Errorf("ParseKey(%q) = %v", bad, err)
		}
	}
}

func TestKeyFile(t *testing.T) {
	dir := t.TempDir()
	k, protection, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := ProtectionNone
	if runtime.GOOS == "windows" {
		want = ProtectionDPAPI
	}
	if protection != want {
		t.Errorf("protection = %s, want %s", protection, want)
	}
	again, protection2, err := LoadOrCreate(dir)
	if err != nil || again != k || protection2 != protection {
		t.Fatalf("second load: %v, same key %t", err, again == k)
	}

	data, err := os.ReadFile(filepath.Join(dir, KeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	var f keyFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	blob, _ := base64.StdEncoding.DecodeString(f.Key)
	if protection == ProtectionDPAPI && bytes.Contains(blob, k.b[:]) {
		t.Error("the DPAPI key file holds the key in clear")
	}
	if info, err := os.Stat(filepath.Join(dir, KeyFileName)); err == nil && runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %v", info.Mode().Perm())
	}

	if err := os.WriteFile(filepath.Join(dir, KeyFileName), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrCreate(dir); err == nil {
		t.Error("an invalid key file was accepted")
	}
}
