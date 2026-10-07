// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"strings"
	"testing"
)

// Usage errors are reported before Docker is ever called.
func TestRunRejectsBadUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "no command", args: nil, want: "missing command"},
		{name: "unknown command", args: []string{"bogus"}, want: `unknown command "bogus"`},
		{name: "user without password", args: []string{"user", "alice"}, want: "usage: testenv user NAME PASSWORD"},
		{name: "unknown flag", args: []string{"-nope", "up"}, want: "flag provided but not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("run(%q) error = %v, want it to contain %q", tt.args, err, tt.want)
			}
		})
	}
}
