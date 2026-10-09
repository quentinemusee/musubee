// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mitText = `MIT License

Copyright (c) 2026 Example

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`

func testPolicy(t *testing.T, exceptions ...Exception) *Policy {
	t.Helper()
	p := &Policy{
		Allowed:    []string{"MIT", "ISC", "Apache-2.0", "BSD-3-Clause", "GPL-2.0-or-later", "LGPL-3.0-or-later", "MPL-2.0"},
		Exceptions: exceptions,
	}
	if err := p.init(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEvalExpression(t *testing.T) {
	p := testPolicy(t)
	tests := []struct {
		expr    string
		want    bool
		wantErr bool
	}{
		{expr: "MIT", want: true},
		{expr: "GPL-2.0-only", want: false},
		{expr: "GPL-2.0+", want: true},
		{expr: "MIT OR GPL-2.0-only", want: true},
		{expr: "MIT or GPL-2.0", want: true},
		{expr: "MIT AND GPL-2.0-only", want: false},
		{expr: "(MIT AND ISC) OR SSPL-1.0", want: true},
		{expr: "Apache-2.0 AND LGPL-3.0-or-later", want: true},
		{expr: "GPL-2.0-or-later WITH Classpath-exception-2.0", want: true},
		{expr: "SSPL-1.0", want: false},
		{expr: "SEE LICENSE IN LICENSE.txt", wantErr: true},
		{expr: "(MIT", wantErr: true},
		{expr: "MIT OR", wantErr: true},
		{expr: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := EvalExpression(tt.expr, p.IsAllowed)
			if (err != nil) != tt.wantErr {
				t.Fatalf("EvalExpression(%q) error = %v, wantErr %v", tt.expr, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("EvalExpression(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}

func TestPolicyRequiresJustifiedExceptions(t *testing.T) {
	for _, e := range []Exception{
		{Ecosystem: "npm", Package: "x", License: "MIT"},              // no reason
		{Ecosystem: "pip", Package: "x", License: "MIT", Reason: "r"}, // bad ecosystem
		{Ecosystem: "go", Package: "", License: "MIT", Reason: "r"},   // no package
		{Ecosystem: "npm", Package: "x", License: " ", Reason: "r"},   // no license
	} {
		p := &Policy{Exceptions: []Exception{e}}
		if err := p.init(); err == nil {
			t.Errorf("exception %+v accepted, want an error", e)
		}
	}
}

func TestExceptionMatching(t *testing.T) {
	p := testPolicy(t,
		Exception{Ecosystem: "npm", Package: "@img/sharp-*", License: "Apache-2.0", Reason: "r"},
		Exception{Ecosystem: "npm", Package: "css-value", License: "MIT", Reason: "r"},
	)
	cases := map[string]bool{"@img/sharp-linux-x64": true, "css-value": true, "css-value-extra": false, "@img/other": false}
	for name, want := range cases {
		if _, got := p.ExceptionFor("npm", name); got != want {
			t.Errorf("ExceptionFor(npm, %q) = %v, want %v", name, got, want)
		}
	}
	if _, got := p.ExceptionFor("go", "css-value"); got {
		t.Error("an npm exception matched a Go module")
	}
}

func TestDetectLicenses(t *testing.T) {
	mit := t.TempDir()
	writeFile(t, filepath.Join(mit, "LICENSE"), mitText)
	if got := DetectLicenses(mit); strings.Join(got, ",") != "MIT" {
		t.Errorf("MIT license text detected as %v", got)
	}

	// REUSE-IgnoreStart
	tagged := t.TempDir()
	writeFile(t, filepath.Join(tagged, "COPYING.md"), "SPDX-License-Identifier: GPL-2.0-only\n")
	if got := DetectLicenses(tagged); strings.Join(got, ",") != "GPL-2.0-only" {
		t.Errorf("SPDX tag detected as %v", got)
	}
	// REUSE-IgnoreEnd

	none := t.TempDir()
	writeFile(t, filepath.Join(none, "README.md"), "no license here\n")
	if got := DetectLicenses(none); len(got) != 0 {
		t.Errorf("directory without license detected as %v", got)
	}

	garbage := t.TempDir()
	writeFile(t, filepath.Join(garbage, "LICENSE"), "All rights reserved. Do not copy.\n")
	if got := DetectLicenses(garbage); len(got) != 0 {
		t.Errorf("unrecognizable license detected as %v", got)
	}
}

// A module replaced by a directory of the repository is the repository's
// code; any other module needs a license file.
func TestGoDependencyLicense(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "core", "replace", "webp")
	writeFile(t, filepath.Join(inside, "go.mod"), "module go.mau.fi/webp\n")
	if dep := goDependency(root, "go.mau.fi/webp", "", inside, "go.mod"); dep.License != RepositoryLicense || !strings.Contains(dep.Source, "core/replace/webp") {
		t.Errorf("replaced module: %+v", dep)
	}

	// A sibling directory whose name starts with the root's is outside.
	outside := root + "-cache"
	writeFile(t, filepath.Join(outside, "LICENSE"), mitText)
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	if dep := goDependency(root, "example.com/mit", "v1.0.0", outside, "go.mod"); dep.License != "MIT" {
		t.Errorf("module with a license file: %+v", dep)
	}
	none := t.TempDir()
	if dep := goDependency(root, "example.com/none", "v1.0.0", none, "go.mod"); dep.License != "" {
		t.Errorf("module without a license: %+v", dep)
	}
}

func TestNPMDependencies(t *testing.T) {
	// The npm registry is replaced by a local fake (pure unit test): it knows
	// the license of "ghost", a package not installed on this platform.
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/ghost/5.0.0":
			_, _ = w.Write([]byte(`{"name":"ghost","version":"5.0.0","license":"Apache-2.0"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer registry.Close()
	previous := NPMRegistry
	NPMRegistry = registry.URL
	defer func() { NPMRegistry = previous }()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package-lock.json"), `{
  "lockfileVersion": 3,
  "packages": {
    "": {"name": "fixture"},
    "node_modules/ok-lib": {"version": "1.0.0", "license": "MIT"},
    "node_modules/gpl-lib": {"version": "2.0.0", "license": "GPL-2.0-only"},
    "node_modules/old-style": {"version": "0.1.0", "license": {"type": "ISC"}},
    "node_modules/dual": {"version": "0.2.0", "license": [{"type": "MIT"}, {"type": "Apache-2.0"}]},
    "node_modules/from-files": {"version": "3.0.0"},
    "node_modules/a/node_modules/nested": {"version": "4.0.0", "license": "BSD-3-Clause"},
    "node_modules/ghost": {"version": "5.0.0", "optional": true},
    "node_modules/@scope/unknown": {"version": "6.0.0", "devOptional": true},
    "node_modules/linked": {"link": true}
  }
}`)
	writeFile(t, filepath.Join(dir, "node_modules", "from-files", "LICENSE"), mitText)
	// Old npm versions leave empty directories for skipped optional
	// packages: "ghost" must still be looked up in the registry.
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "ghost"), 0o755); err != nil {
		t.Fatal(err)
	}

	deps, err := NPMDependencies(filepath.Join(dir, "package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Dependency{}
	for _, d := range deps {
		got[d.Name] = d
	}
	want := map[string]string{
		"ok-lib": "MIT", "gpl-lib": "GPL-2.0-only", "old-style": "ISC", "dual": "(MIT OR Apache-2.0)",
		"from-files": "MIT", "nested": "BSD-3-Clause", "ghost": "Apache-2.0", "@scope/unknown": "",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d dependencies %v, want %d", len(got), got, len(want))
	}
	for name, license := range want {
		if got[name].License != license {
			t.Errorf("%s: license = %q, want %q", name, got[name].License, license)
		}
	}
	if !strings.Contains(got["ghost"].Source, "npm registry") {
		t.Errorf("ghost: source = %q, want the npm registry", got["ghost"].Source)
	}
	if !strings.Contains(got["@scope/unknown"].Source, "HTTP 404") {
		t.Errorf("@scope/unknown: source = %q, want the registry error", got["@scope/unknown"].Source)
	}
}

func TestNPMDependenciesRejectsOldLockfiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package-lock.json"), `{"lockfileVersion": 1, "dependencies": {}}`)
	if _, err := NPMDependencies(filepath.Join(dir, "package-lock.json")); err == nil {
		t.Fatal("lockfile v1 accepted")
	}
}

func TestAudit(t *testing.T) {
	p := testPolicy(t,
		Exception{Ecosystem: "npm", Package: "documented", License: "MIT", Reason: "README says MIT"},
		Exception{Ecosystem: "npm", Package: "gone", License: "MIT", Reason: "stale"},
	)
	deps := []Dependency{
		{Ecosystem: "go", Name: "example.com/ok", Version: "v1.0.0", License: "BSD-3-Clause"},
		{Ecosystem: "npm", Name: "gpl", Version: "1.0.0", License: "GPL-2.0-only"},
		{Ecosystem: "npm", Name: "mystery", Version: "1.0.0", Source: "no license declared or detected"},
		{Ecosystem: "npm", Name: "documented", Version: "1.0.0"},
		{Ecosystem: "npm", Name: "documented", Version: "1.0.0"}, // duplicate entry
	}
	r := Audit(deps, p)
	if len(r.Verdicts) != 4 {
		t.Fatalf("got %d verdicts, want 4 (duplicates merged)", len(r.Verdicts))
	}
	problems := strings.Join(r.Problems, "\n")
	for _, want := range []string{"npm gpl@1.0.0", "npm mystery@1.0.0", "license unknown", "exception for npm gone matches no dependency"} {
		if !strings.Contains(problems, want) {
			t.Errorf("problems lack %q:\n%s", want, problems)
		}
	}
	for _, unwanted := range []string{"example.com/ok", "documented@"} {
		if strings.Contains(problems, unwanted) {
			t.Errorf("problems wrongly mention %q:\n%s", unwanted, problems)
		}
	}
}

func TestRepositoryPolicyIsValid(t *testing.T) {
	p, err := LoadPolicy(filepath.Join("..", "license-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"GPL-2.0", "GPL-2.0-only", "SSPL-1.0", "BUSL-1.1", "CC-BY-NC-4.0", "Elastic-2.0"} {
		if p.IsAllowed(forbidden) {
			t.Errorf("the policy allows %s, which is not compatible with AGPL-3.0-or-later", forbidden)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
