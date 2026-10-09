// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package telegram

import (
	"errors"
	"image"
	"io"
	"testing"

	"go.mau.fi/webp"
)

func TestNewNeedsTheApplicationCredentials(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New accepted a configuration without api_id")
	}
	if _, err := New(Config{APIID: 1}); err == nil {
		t.Fatal("New accepted a configuration without api_hash")
	}
}

func TestNewConfiguresTheConnector(t *testing.T) {
	c, err := New(Config{APIID: 42, APIHash: "0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := c.Config
	if cfg.APIID != 42 || cfg.APIHash != "0123456789abcdef0123456789abcdef" {
		t.Errorf("credentials not set: %d %q", cfg.APIID, cfg.APIHash)
	}
	if cfg.DeviceInfo.DeviceModel != DeviceModel {
		t.Errorf("device model %q, want %q", cfg.DeviceInfo.DeviceModel, DeviceModel)
	}
	if cfg.AnimatedSticker.Target != "disable" {
		t.Errorf("animated sticker target %q, want disable", cfg.AnimatedSticker.Target)
	}
	// The upstream defaults are loaded, including the display name template.
	if cfg.Ping.IntervalSeconds == 0 || cfg.Sync.UpdateLimit == 0 {
		t.Errorf("upstream defaults not loaded: ping %d, sync %d", cfg.Ping.IntervalSeconds, cfg.Sync.UpdateLimit)
	}
	if name := cfg.FormatDisplayname("Ada", "Lovelace", "", false, 1); name != "Ada Lovelace" {
		t.Errorf("display name %q", name)
	}
}

func TestManualLoginIsNotOffered(t *testing.T) {
	c, err := New(Config{APIID: 42, APIHash: "0123456789abcdef0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, f := range c.GetLoginFlows() {
		ids = append(ids, f.ID)
	}
	want := []string{"phone", "qr", "bot"}
	if len(ids) != len(want) {
		t.Fatalf("flows %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("flows %v, want %v", ids, want)
		}
	}
	if _, err := c.CreateLogin(t.Context(), nil, "manual"); !errors.Is(err, ErrFlowNotOffered) {
		t.Fatalf("CreateLogin(manual) = %v, want ErrFlowNotOffered", err)
	}
}

// The connector links the pure-Go stand-in of go.mau.fi/webp (core/replace/webp),
// not libwebp through cgo: encoding WebP is unavailable.
func TestWebPIsTheStandIn(t *testing.T) {
	err := webp.Encode(io.Discard, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil)
	if !errors.Is(err, webp.ErrUnsupported) {
		t.Fatalf("webp.Encode = %v, want the stand-in's ErrUnsupported", err)
	}
}
