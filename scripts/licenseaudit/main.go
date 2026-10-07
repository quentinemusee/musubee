// SPDX-FileCopyrightText: 2026 Quentin Raimbaud
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command licenseaudit checks the licenses of every Go and npm dependency of
// the repository against scripts/license-policy.json, and fails when one is
// not compatible with AGPL-3.0-or-later, cannot be identified, or is only
// accepted through an exception that no longer matches anything.
//
//	go run ./scripts/licenseaudit            from the repository root
//	go run ./scripts/licenseaudit -v         also list every dependency
//
// Go dependencies are the modules compiled into the packages and tests of
// every module of go.work (with the integration and telegram build tags);
// their licenses are detected from the license files in the module cache.
// npm dependencies come from every package-lock.json tracked by Git; their
// declared SPDX expression is used, or their license files when there is none
// (run "npm ci" first to let the audit read installed packages).
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("licenseaudit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	policyPath := flags.String("policy", "", "policy file (default: <root>/scripts/license-policy.json)")
	verbose := flags.Bool("v", false, "list every dependency")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *policyPath == "" {
		*policyPath = filepath.Join(*root, "scripts", "license-policy.json")
	}
	policy, err := LoadPolicy(*policyPath)
	if err != nil {
		fmt.Fprintln(stderr, "licenseaudit:", err)
		return 2
	}

	var deps []Dependency
	modules, err := goWorkModules(*root)
	if err != nil {
		fmt.Fprintln(stderr, "licenseaudit:", err)
		return 2
	}
	goDeps, err := GoDependencies(modules)
	if err != nil {
		fmt.Fprintln(stderr, "licenseaudit:", err)
		return 2
	}
	deps = append(deps, goDeps...)
	locks, err := trackedFiles(*root, "package-lock.json")
	if err != nil {
		fmt.Fprintln(stderr, "licenseaudit:", err)
		return 2
	}
	for _, lock := range locks {
		npmDeps, err := NPMDependencies(lock)
		if err != nil {
			fmt.Fprintln(stderr, "licenseaudit:", err)
			return 2
		}
		deps = append(deps, npmDeps...)
	}

	report := Audit(deps, policy)
	report.Print(stdout, *verbose)
	if len(report.Problems) > 0 {
		return 1
	}
	return 0
}

// Verdict is the decision for one dependency.
type Verdict struct {
	Dependency
	OK        bool
	Exception *Exception
	Reason    string
}

// Report is the result of an audit.
type Report struct {
	Verdicts []Verdict
	Problems []string
}

// Audit judges every dependency against the policy. Exceptions that match
// no dependency are reported too, so that the list stays short and true.
func Audit(deps []Dependency, policy *Policy) Report {
	var r Report
	used := map[int]bool{}
	seen := map[string]bool{}
	for _, d := range deps {
		id := d.Ecosystem + " " + d.Name + "@" + d.Version
		if seen[id] {
			continue
		}
		seen[id] = true
		v := Verdict{Dependency: d}
		if e, ok := policy.ExceptionFor(d.Ecosystem, d.Name); ok {
			for i := range policy.Exceptions {
				if policy.Exceptions[i] == e {
					used[i] = true
				}
			}
			v.OK, v.Exception, v.Reason = true, &e, "exception: "+e.Reason
		} else if d.License == "" {
			v.Reason = "license unknown (" + d.Source + ")"
		} else if ok, err := EvalExpression(d.License, policy.IsAllowed); err != nil {
			v.Reason = fmt.Sprintf("unreadable license expression %q: %v", d.License, err)
		} else if !ok {
			v.Reason = fmt.Sprintf("license %q is not compatible with AGPL-3.0-or-later (or not in the policy)", d.License)
		} else {
			v.OK = true
		}
		if !v.OK {
			r.Problems = append(r.Problems, fmt.Sprintf("%s %s@%s: %s [%s]", d.Ecosystem, d.Name, d.Version, v.Reason, d.From))
		}
		r.Verdicts = append(r.Verdicts, v)
	}
	for i, e := range policy.Exceptions {
		if !used[i] {
			r.Problems = append(r.Problems, fmt.Sprintf("exception for %s %s matches no dependency: remove it from the policy", e.Ecosystem, e.Package))
		}
	}
	return r
}

// Print writes a summary, and every problem.
func (r Report) Print(w io.Writer, verbose bool) {
	counts := map[string]int{}
	ecosystems := map[string]int{}
	exceptions := 0
	for _, v := range r.Verdicts {
		ecosystems[v.Ecosystem]++
		if v.Exception != nil {
			exceptions++
			continue
		}
		license := v.License
		if license == "" {
			license = "(unknown)"
		}
		counts[license]++
	}
	fmt.Fprintf(w, "licenseaudit: %d Go modules, %d npm packages, %d accepted by exception\n", ecosystems["go"], ecosystems["npm"], exceptions)
	licenses := make([]string, 0, len(counts))
	for l := range counts {
		licenses = append(licenses, l)
	}
	sort.Slice(licenses, func(i, j int) bool {
		if counts[licenses[i]] != counts[licenses[j]] {
			return counts[licenses[i]] > counts[licenses[j]]
		}
		return licenses[i] < licenses[j]
	})
	for _, l := range licenses {
		fmt.Fprintf(w, "  %4d  %s\n", counts[l], l)
	}
	if verbose {
		for _, v := range r.Verdicts {
			status := "ok"
			if !v.OK {
				status = "PROBLEM"
			} else if v.Exception != nil {
				status = "exception"
			}
			fmt.Fprintf(w, "%-9s %s %s@%s  %s  (%s)\n", status, v.Ecosystem, v.Name, v.Version, v.License, v.Source)
		}
	}
	if len(r.Problems) == 0 {
		fmt.Fprintln(w, "licenseaudit: OK, every dependency is compatible with AGPL-3.0-or-later.")
		return
	}
	fmt.Fprintf(w, "licenseaudit: FAILED, %d problem(s):\n", len(r.Problems))
	for _, p := range r.Problems {
		fmt.Fprintln(w, "  - "+p)
	}
	fmt.Fprintln(w, "Remove the dependency, or add a justified exception to scripts/license-policy.json (see docs/ADR/0008-dependency-license-audit.md).")
}

// goWorkModules returns the module directories listed in go.work.
func goWorkModules(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.work"))
	if err != nil {
		return nil, fmt.Errorf("reading go.work: %w", err)
	}
	var dirs []string
	inUse := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case line == "use (":
			inUse = true
		case inUse && line == ")":
			inUse = false
		case inUse && line != "":
			dirs = append(dirs, filepath.Join(root, filepath.FromSlash(line)))
		case strings.HasPrefix(line, "use "):
			dirs = append(dirs, filepath.Join(root, filepath.FromSlash(strings.TrimSpace(strings.TrimPrefix(line, "use ")))))
		}
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("go.work lists no module")
	}
	return dirs, nil
}

// trackedFiles returns the files with the given base name that Git tracks
// or would commit.
func trackedFiles(root, base string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	var files []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" && filepath.Base(p) == base {
			files = append(files, filepath.Join(root, filepath.FromSlash(p)))
		}
	}
	sort.Strings(files)
	return files, nil
}
