// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package codegen

import (
	"strings"
	"testing"
)

func TestGoName(t *testing.T) {
	for in, want := range map[string]string{
		"account_id":   "AccountID",
		"core.hello":   "CoreHello",
		"2fa_code":     "2FACode",
		"timestamp_ms": "TimestampMS",
		"qr":           "QR",
		"login_flows":  "LoginFlows",
	} {
		if got := goName(in); got != want {
			t.Errorf("goName(%q) = %q, want %q", in, got, want)
		}
	}
}

// minimal wraps $defs entries into a schema with one command and one event.
func minimal(defs string) []byte {
	return []byte(`{"x-musubee-api-version": "1.0", "$defs": {
		"Commands": {"properties": {"x.y": {"properties": {"params": {"$ref": "#/$defs/Empty"}, "result": {"$ref": "#/$defs/Empty"}}}}},
		"Events": {"properties": {"x.happened": {"$ref": "#/$defs/Empty"}}},
		"Empty": {"type": "object", "additionalProperties": false}` + defs + `}}`)
}

func TestParseMinimal(t *testing.T) {
	s, err := Parse(minimal(""))
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != "1.0" || len(s.Commands) != 1 || len(s.Events) != 1 || len(s.Types) != 1 {
		t.Fatalf("unexpected schema: %+v", s)
	}
	goCode, tsCode, err := Generate(minimal(""))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goCode), `CommandXY = "x.y"`) || !strings.Contains(string(tsCode), `"x.y": { params: Empty; result: Empty };`) {
		t.Errorf("unexpected output:\n%s\n%s", goCode, tsCode)
	}
}

// The generators refuse what they would not translate faithfully.
func TestParseRejects(t *testing.T) {
	for name, defs := range map[string]string{
		"unknown keyword":    `, "A": {"type": "object", "additionalProperties": false, "properties": {"a": {"type": "string", "nullable": true}}}`,
		"inline enum":        `, "A": {"type": "object", "additionalProperties": false, "properties": {"a": {"type": "string", "enum": ["x"]}}}`,
		"inline object":      `, "A": {"type": "object", "additionalProperties": false, "properties": {"a": {"type": "object", "properties": {}}}}`,
		"open object":        `, "A": {"type": "object", "properties": {"a": {"type": "string"}}}`,
		"undefined ref":      `, "A": {"type": "object", "additionalProperties": false, "properties": {"a": {"$ref": "#/$defs/Nope"}}}`,
		"undefined required": `, "A": {"type": "object", "additionalProperties": false, "required": ["b"]}`,
		"oneOf":              `, "A": {"oneOf": [{"type": "string"}]}`,
		"string alias":       `, "A": {"type": "string"}`,
	} {
		if _, err := Parse(minimal(defs)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Parse([]byte(`{"$defs": {}}`)); err == nil {
		t.Error("a schema without a version was accepted")
	}
	if _, err := Parse([]byte(`{"x-musubee-api-version": "1.0", "x-musubee-api-version": "2.0", "$defs": {}}`)); err == nil {
		t.Error("duplicate keys were accepted")
	}
}

func TestOptionalFields(t *testing.T) {
	goCode, tsCode, err := Generate(minimal(`, "A": {"type": "object", "additionalProperties": false,
		"properties": {"req": {"type": "integer"}, "opt": {"$ref": "#/$defs/Empty"}, "tags": {"type": "array", "items": {"type": "string"}}, "m": {"type": "object", "additionalProperties": {"type": "boolean"}}},
		"required": ["req"]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Req  int64           `json:\"req\"`", "Opt  *Empty          `json:\"opt,omitempty\"`", "Tags []string        `json:\"tags,omitempty\"`", "M    map[string]bool `json:\"m,omitempty\"`"} {
		if !strings.Contains(string(goCode), want) {
			t.Errorf("Go code lacks %q:\n%s", want, goCode)
		}
	}
	for _, want := range []string{"req: number;", "opt?: Empty;", "tags?: string[];", "m?: Record<string, boolean>;"} {
		if !strings.Contains(string(tsCode), want) {
			t.Errorf("TypeScript code lacks %q:\n%s", want, tsCode)
		}
	}
}
