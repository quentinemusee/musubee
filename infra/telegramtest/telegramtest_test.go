// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package telegramtest

import (
	"bytes"
	"errors"
	"testing"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
)

func TestCredentialsFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		hash    string
		want    Credentials
		wantErr bool
	}{
		{name: "defaults to the public test app", want: Credentials{APIID: telegram.TestAppID, APIHash: telegram.TestAppHash}},
		{name: "own app", id: "12345", hash: "abcdef", want: Credentials{APIID: 12345, APIHash: "abcdef"}},
		{name: "id without hash", id: "12345", wantErr: true},
		{name: "hash without id", hash: "abcdef", wantErr: true},
		{name: "invalid id", id: "twelve", hash: "abcdef", wantErr: true},
		{name: "negative id", id: "-1", hash: "abcdef", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(APIIDEnv, tt.id)
			t.Setenv(APIHashEnv, tt.hash)
			got, err := CredentialsFromEnv()
			if (err != nil) != tt.wantErr {
				t.Fatalf("CredentialsFromEnv() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("CredentialsFromEnv() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestIsPublicTestApp(t *testing.T) {
	if !(Credentials{APIID: telegram.TestAppID, APIHash: telegram.TestAppHash}).IsPublicTestApp() {
		t.Error("public test credentials not recognized")
	}
	if (Credentials{APIID: 1, APIHash: "x"}).IsPublicTestApp() {
		t.Error("other credentials taken for the public test app")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	ctx := t.Context()
	original := &session.StorageMemory{}
	payload := []byte(`{"Version":1,"Data":{"DC":2}}`)
	if err := original.StoreSession(ctx, payload); err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeSession(ctx, original)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.ContainsAny([]byte(encoded), "\r\n") {
		t.Error("encoded session spans several lines")
	}
	decoded, err := DecodeSession(ctx, encoded+"\n")
	if err != nil {
		t.Fatal(err)
	}
	got, err := decoded.LoadSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("round trip = %s, want %s", got, payload)
	}
}

func TestDecodeSessionRejectsBadInput(t *testing.T) {
	for _, in := range []string{"", "   ", "not base64 !"} {
		if _, err := DecodeSession(t.Context(), in); err == nil {
			t.Errorf("DecodeSession(%q) succeeded, want an error", in)
		}
	}
}

func TestSessionFromEnvWithoutVariable(t *testing.T) {
	t.Setenv(SessionEnv, "")
	if _, err := SessionFromEnv(t.Context()); !errors.Is(err, ErrNoSession) {
		t.Errorf("SessionFromEnv() error = %v, want ErrNoSession", err)
	}
}
