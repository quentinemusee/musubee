// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package api_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/quentinemusee/musubee/core/api"
	"github.com/quentinemusee/musubee/core/api/apitest"
	"github.com/quentinemusee/musubee/core/api/internal/codegen"
)

// The generated files must match the schema: "go generate ./core/api"
// rewrites them. This is the CI check that nobody edited the schema without
// regenerating, or edited a generated file by hand.
func TestGeneratedFilesAreUpToDate(t *testing.T) {
	goCode, tsCode, err := codegen.Generate(api.Schema)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string][]byte{"types.gen.go": goCode, "../../ui/src/core-api/types.gen.ts": tsCode} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// Git may check files out with CRLF line endings on Windows.
		got = bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
		if !bytes.Equal(got, want) {
			t.Errorf("%s is out of date: run go generate ./core/api", path)
		}
	}
}

func TestVersionMatchesTheSchema(t *testing.T) {
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+$`).MatchString(api.APIVersion) {
		t.Errorf("APIVersion %q is not major.minor", api.APIVersion)
	}
}

type example struct {
	Target string          `json:"target"`
	Why    string          `json:"why"`
	Value  json.RawMessage `json:"value"`
}

func loadExamples(t *testing.T) (valid, invalid []example) {
	t.Helper()
	data, err := os.ReadFile("schema/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Valid   []example `json:"valid"`
		Invalid []example `json:"invalid"`
	}
	if err = json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	return file.Valid, file.Invalid
}

func newValidator(t *testing.T) *apitest.Validator {
	t.Helper()
	v, err := apitest.New()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// goType returns the generated Go type of a command or event target.
func goType(t *testing.T, target string) reflect.Type {
	t.Helper()
	parts := strings.Split(target, ":")
	switch parts[0] {
	case "command":
		for _, c := range api.Commands {
			if c.Name == parts[1] {
				if parts[2] == "params" {
					return c.Params
				}
				return c.Result
			}
		}
	case "event":
		for _, e := range api.Events {
			if e.Type == parts[1] {
				return e.Data
			}
		}
	default:
		return nil // an envelope
	}
	t.Fatalf("%s: no such command or event", target)
	return nil
}

// Every valid example passes the validator and goes through the generated
// Go type unchanged: decoding rejects unknown fields, and encoding again
// gives the same document. This checks that the Go types and the schema
// agree, independently of the generator.
func TestValidExamples(t *testing.T) {
	v := newValidator(t)
	valid, _ := loadExamples(t)
	for i, ex := range valid {
		if err := v.Validate(ex.Target, ex.Value); err != nil {
			t.Errorf("valid example %d: %v", i, err)
			continue
		}
		typ := goType(t, ex.Target)
		if typ == nil {
			continue
		}
		value := reflect.New(typ)
		dec := json.NewDecoder(bytes.NewReader(ex.Value))
		dec.DisallowUnknownFields()
		if err := dec.Decode(value.Interface()); err != nil {
			t.Errorf("valid example %d (%s): decoding into %s: %v", i, ex.Target, typ, err)
			continue
		}
		again, err := json.Marshal(value.Interface())
		if err != nil {
			t.Fatal(err)
		}
		if !sameJSON(t, ex.Value, again) {
			t.Errorf("valid example %d (%s): changed by %s:\n  was %s\n  now %s", i, ex.Target, typ, compact(t, ex.Value), again)
		}
	}
}

func TestInvalidExamples(t *testing.T) {
	v := newValidator(t)
	_, invalid := loadExamples(t)
	for i, ex := range invalid {
		if err := v.Validate(ex.Target, ex.Value); err == nil {
			t.Errorf("invalid example %d (%s, %s) passed", i, ex.Target, ex.Why)
		}
	}
}

// Every command (params and result) and every event has a valid example.
func TestExamplesCoverTheSchema(t *testing.T) {
	valid, _ := loadExamples(t)
	covered := map[string]bool{}
	for _, ex := range valid {
		covered[ex.Target] = true
	}
	for _, c := range api.Commands {
		for _, part := range []string{"params", "result"} {
			if target := "command:" + c.Name + ":" + part; !covered[target] {
				t.Errorf("no valid example for %s", target)
			}
		}
	}
	for _, e := range api.Events {
		if target := "event:" + e.Type; !covered[target] {
			t.Errorf("no valid example for %s", target)
		}
	}
}

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(va, vb)
}

func compact(t *testing.T, data []byte) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, data); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
