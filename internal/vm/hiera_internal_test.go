// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const hieraYAML = `version: 5
defaults:
  datadir: data
  data_hash: yaml_data
hierarchy:
  - name: Common
    path: common.yaml
  - name: Node
    path: node.yaml
`

// common.yaml is the higher-priority level; node.yaml the lower. The keys are
// chosen so priority / unique / hash / deep merges each produce a distinct result.
const hieraCommonYAML = `greeting: hello
servers:
  - a
  - b
config:
  timeout: 30
  retries: 3
interp: "value is %{myvar}"
a: "%{lookup('b')}"
b: "%{lookup('a')}"
`

const hieraNodeYAML = `servers:
  - b
  - c
config:
  retries: 5
  verbose: true
`

// hieraFixture writes a Hiera 5 config + two data files into a temp dir and
// returns the hiera.yaml path.
func hieraFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("hiera.yaml", hieraYAML)
	write(filepath.Join("data", "common.yaml"), hieraCommonYAML)
	write(filepath.Join("data", "node.yaml"), hieraNodeYAML)
	return filepath.Join(dir, "hiera.yaml")
}

// hieraRun runs a Ruby program with `require "hiera"` and the config path bound to
// the local variable `cfg`.
func hieraRun(t *testing.T, cfg, body string) string {
	t.Helper()
	return runSrc(t, fmt.Sprintf("require \"hiera\"\ncfg = %q\n%s", cfg, body))
}

// TestHieraLookupFlow drives the headline flow: construct from a hiera.yaml and
// resolve keys with the default merge (priority), a missing key, and a default.
func TestHieraLookupFlow(t *testing.T) {
	cfg := hieraFixture(t)
	got := hieraRun(t, cfg, `
h = Hiera.new(config: cfg)
puts h.lookup("greeting")
puts h.lookup("missing").inspect
puts h.lookup("missing", "dflt")
puts h.lookup("servers").inspect
puts h.class
`)
	want := "hello\nnil\ndflt\n[\"a\", \"b\"]\nHiera"
	if got != want {
		t.Fatalf("lookup flow:\n got=%q\nwant=%q", got, want)
	}
}

// TestHieraResolutionTypes proves each resolution_type / merge behaviour changes
// the result, and that an unrecognised value degrades to priority.
func TestHieraResolutionTypes(t *testing.T) {
	cfg := hieraFixture(t)
	got := hieraRun(t, cfg, `
h = Hiera.new(config: cfg)
puts h.lookup("servers", nil, resolution_type: :array).inspect
puts h.lookup("config", nil, merge: :hash)["retries"]
puts h.lookup("config", nil, merge: :hash)["verbose"]
puts h.lookup("config", nil, resolution_type: :deep)["retries"]
puts h.lookup("greeting", nil, resolution_type: :bogus)
`)
	want := "[\"a\", \"b\", \"c\"]\n3\ntrue\n3\nhello"
	if got != want {
		t.Fatalf("resolution types:\n got=%q\nwant=%q", got, want)
	}
}

// TestHieraScopeInterpolation covers %{var} interpolation against the scope built
// from the :scope keyword, including the string-keyed kwargs form.
func TestHieraScopeInterpolation(t *testing.T) {
	cfg := hieraFixture(t)
	got := hieraRun(t, cfg, `
h = Hiera.new(config: cfg, scope: { "myvar" => "world" })
puts h.lookup("interp")
h2 = Hiera.new("config" => cfg)
puts h2.lookup("greeting")
`)
	want := "value is world\nhello"
	if got != want {
		t.Fatalf("scope interp:\n got=%q\nwant=%q", got, want)
	}
}

// TestHieraDefaultHash covers a Hash default argument (no resolution_type/merge
// keyword, so the trailing Hash is treated as the default value, not kwargs).
func TestHieraDefaultHash(t *testing.T) {
	cfg := hieraFixture(t)
	got := hieraRun(t, cfg, `
h = Hiera.new(config: cfg)
puts h.lookup("missing", { "x" => 1 })["x"]
`)
	if got != "1" {
		t.Fatalf("default hash: got=%q want=1", got)
	}
}

// TestHieraErrors covers the raising paths: missing :config, a bad config path,
// a lookup with no key, and an interpolation-loop lookup error.
func TestHieraErrors(t *testing.T) {
	cfg := hieraFixture(t)
	cases := []struct{ body, want string }{
		{`begin; Hiera.new; rescue => e; puts e.class; end`, "ArgumentError"},
		{`begin; Hiera.new(config: "/no/such/hiera.yaml"); rescue => e; puts e.class; end`, "RuntimeError"},
		{`h = Hiera.new(config: cfg); begin; h.lookup; rescue => e; puts e.class; end`, "ArgumentError"},
		{`h = Hiera.new(config: cfg); begin; h.lookup("a"); rescue => e; puts e.class; end`, "RuntimeError"},
	}
	for _, c := range cases {
		if got := hieraRun(t, cfg, c.body); got != c.want {
			t.Fatalf("%s\n got=%q want=%q", c.body, got, c.want)
		}
	}
}

// TestHieraStringers covers the object.Value marker methods on the wrapper.
func TestHieraStringers(t *testing.T) {
	o := &HieraObj{}
	if o.ToS() != "#<Hiera>" || o.Inspect() != o.ToS() || !o.Truthy() {
		t.Errorf("Hiera stringers = %q / %q / %v", o.ToS(), o.Inspect(), o.Truthy())
	}
}
