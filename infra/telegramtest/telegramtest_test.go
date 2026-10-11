// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package telegramtest

import (
	"testing"
	"time"
)

func TestCredentialsFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		hash    string
		want    Credentials
		wantErr bool
	}{
		{name: "defaults to the public test app", want: Credentials{APIID: PublicTestAppID, APIHash: PublicTestAppHash}},
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
	if !(Credentials{APIID: PublicTestAppID, APIHash: PublicTestAppHash}).IsPublicTestApp() {
		t.Error("public test credentials not recognized")
	}
	if (Credentials{APIID: 1, APIHash: "x"}).IsPublicTestApp() {
		t.Error("other credentials taken for the public test app")
	}
}

func TestFloodWait(t *testing.T) {
	for message, want := range map[string]time.Duration{
		"rpc error code 420: FLOOD_WAIT (10)":        10 * time.Second,
		"login failed: FLOOD_WAIT_120":               120 * time.Second,
		"rpc error code 420: FLOOD_PREMIUM_WAIT (5)": -1,
		"rpc error code 400: API_ID_PUBLISHED_FLOOD": -1,
		"": -1,
	} {
		got, ok := FloodWait(message)
		if (want < 0 && ok) || (want >= 0 && (!ok || got != want)) {
			t.Errorf("FloodWait(%q) = %v, %t; want %v", message, got, ok, want)
		}
	}
}
