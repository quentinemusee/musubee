// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package apitest checks JSON documents against the core API schema, for the
// contract tests of the core and of its transports. It uses a JSON Schema
// validator that knows nothing of the generated types, so that a mistake in
// the generator cannot hide a mismatch. Only tests import it.
package apitest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/quentinemusee/musubee/core/api"
)

// schemaURL is the $id of the schema.
const schemaURL = "https://musubee.invalid/core-api/v1/core-api.schema.json"

// Validator checks documents against parts of the schema.
type Validator struct {
	compiler *jsonschema.Compiler
	mu       sync.Mutex
	cache    map[string]*jsonschema.Schema
}

// New loads the schema of package api.
func New() (*Validator, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(api.Schema))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	if err = c.AddResource(schemaURL, doc); err != nil {
		return nil, err
	}
	v := &Validator{compiler: c, cache: map[string]*jsonschema.Schema{}}
	// Compiling the root checks the schema against the JSON Schema
	// meta-schema.
	if _, err = v.schema(""); err != nil {
		return nil, err
	}
	return v, nil
}

// Pointer returns the JSON pointer of a $defs entry. Target is "Request",
// "Response", "Event" or any other $defs name, "command:NAME:params",
// "command:NAME:result" or "event:TYPE" (the data of the event).
func Pointer(target string) (string, error) {
	parts := strings.Split(target, ":")
	switch {
	case len(parts) == 1:
		return "/$defs/" + escape(parts[0]), nil
	case len(parts) == 3 && parts[0] == "command" && (parts[2] == "params" || parts[2] == "result"):
		return "/$defs/Commands/properties/" + escape(parts[1]) + "/properties/" + parts[2], nil
	case len(parts) == 2 && parts[0] == "event":
		return "/$defs/Events/properties/" + escape(parts[1]), nil
	}
	return "", fmt.Errorf("invalid target %q", target)
}

func escape(token string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(token)
}

func (v *Validator) schema(pointer string) (*jsonschema.Schema, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if s, ok := v.cache[pointer]; ok {
		return s, nil
	}
	s, err := v.compiler.Compile(schemaURL + "#" + pointer)
	if err != nil {
		return nil, err
	}
	v.cache[pointer] = s
	return s, nil
}

// Validate checks a JSON document against a target (see Pointer).
func (v *Validator) Validate(target string, data []byte) error {
	pointer, err := Pointer(target)
	if err != nil {
		return err
	}
	s, err := v.schema(pointer)
	if err != nil {
		return err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s: not JSON: %w", target, err)
	}
	if err = s.Validate(doc); err != nil {
		return fmt.Errorf("%s: %w", target, err)
	}
	return nil
}

// ValidateResponse checks the response of a command: the envelope, and the
// result against the command's result schema.
func (v *Validator) ValidateResponse(command string, data []byte) error {
	if err := v.Validate("Response", data); err != nil {
		return err
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return err
	}
	if resp.Result == nil {
		return nil
	}
	return v.Validate("command:"+command+":result", resp.Result)
}

// ValidateEvent checks an event: the envelope, and the data against the
// schema of its type. Unknown types are an error: the core must only send
// documented events.
func (v *Validator) ValidateEvent(data []byte) error {
	if err := v.Validate("Event", data); err != nil {
		return err
	}
	var evt struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &evt); err != nil {
		return err
	}
	for _, spec := range api.Events {
		if spec.Type == evt.Type {
			return v.Validate("event:"+evt.Type, evt.Data)
		}
	}
	return fmt.Errorf("undocumented event type %q", evt.Type)
}

// matrixID matches the Matrix identifiers of users, rooms and room aliases.
var matrixID = regexp.MustCompile(`^[@!#][^:\s]+:\S+$`)

// MatrixIDs returns the strings of a response or an event that show a
// Matrix identifier: no Matrix type may reach the user interface
// (CLAUDE.md §2). The core's opaque IDs encode Matrix IDs, but in base64,
// so they pass. The strings of the request, if any, are ignored, since an
// error message may quote them; short ones, such as "x", would hide too
// much, and stay checked.
func MatrixIDs(document, request []byte) ([]string, error) {
	var quoted []string
	if request != nil {
		var req any
		if err := json.Unmarshal(request, &req); err != nil {
			return nil, err
		}
		for _, q := range jsonStrings(req) {
			if len(q) >= 8 {
				quoted = append(quoted, q)
			}
		}
	}
	var doc any
	if err := json.Unmarshal(document, &doc); err != nil {
		return nil, err
	}
	var found []string
	for _, v := range jsonStrings(doc) {
		for _, q := range quoted {
			v = strings.ReplaceAll(v, q, "")
		}
		if matrixID.MatchString(v) || strings.HasPrefix(v, "$") || strings.Contains(v, "mxc://") || strings.Contains(v, "musubee.local") {
			found = append(found, v)
		}
	}
	return found, nil
}

// jsonStrings returns every string of a decoded JSON value, keys included.
func jsonStrings(v any) []string {
	switch v := v.(type) {
	case string:
		return []string{v}
	case []any:
		var all []string
		for _, item := range v {
			all = append(all, jsonStrings(item)...)
		}
		return all
	case map[string]any:
		var all []string
		for key, item := range v {
			all = append(all, key)
			all = append(all, jsonStrings(item)...)
		}
		return all
	}
	return nil
}
