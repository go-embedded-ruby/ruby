// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// writeJekyllFile writes rel (a slash path) under dir with the given contents,
// creating parent directories.
func writeJekyllFile(t *testing.T, dir, rel, contents string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readTree returns the concatenated contents of every file under dir, so a test can
// assert on the rendered output regardless of the exact output filename.
func readTree(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		b.Write(raw)
		return nil
	})
	return b.String()
}

// tinySite writes a minimal one-page site (front matter + Liquid) into a fresh temp
// dir and returns it.
func tinySite(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	writeJekyllFile(t, src, "index.html", "---\ntitle: Hello\n---\n<h1>{{ page.title }}</h1>\n<p>{{ \"world\" | capitalize }}</p>\n")
	return src
}

// TestJekyllBuildEndToEnd is the headline scenario: require the library, build a
// tiny site through Jekyll.build, and assert the rendered output carries the
// front-matter title and the filtered Liquid value.
func TestJekyllBuildEndToEnd(t *testing.T) {
	src := tinySite(t)
	dest := t.TempDir()
	prog := "\nrequire \"jekyll\"\nraise \"version\" if Jekyll::VERSION.empty?\nout = Jekyll.build(" + rubyStr(src) + ", " + rubyStr(dest) + ")\nputs out\n"
	if got := runSrc(t, prog); got != dest {
		t.Fatalf("build returned %q, want %q", got, dest)
	}
	rendered := readTree(t, dest)
	if !strings.Contains(rendered, "Hello") || !strings.Contains(rendered, "World") {
		t.Fatalf("rendered output missing expected content:\n%s", rendered)
	}
}

// TestJekyllSiteProcess covers the gem-faithful path: Jekyll.configuration returns
// the resolved config Hash (round-tripping every value type), Jekyll::Site.new(cfg)
// wraps it, #source / #dest expose the directories, and #process builds it.
func TestJekyllSiteProcess(t *testing.T) {
	src := tinySite(t)
	dest := t.TempDir()
	prog := "\nrequire \"jekyll\"\ncfg = Jekyll.configuration({\"source\" => " + rubyStr(src) + ", \"destination\" => " + rubyStr(dest) + "})\nraise \"collections\" unless cfg[\"collections\"][\"posts\"][\"output\"] == true\nsite = Jekyll::Site.new(cfg)\nraise \"dest\" unless site.dest == " + rubyStr(dest) + "\nraise \"source\" if site.source.empty?\nraise \"process\" unless site.process.nil?\nputs \"ok\"\n"
	if got := runSrc(t, prog); got != "ok" {
		t.Fatalf("site process output = %q", got)
	}
	rendered := readTree(t, dest)
	if !strings.Contains(rendered, "Hello") || !strings.Contains(rendered, "World") {
		t.Fatalf("rendered output missing expected content:\n%s", rendered)
	}
}

// TestJekyllConfiguration drives the configuration/override marshalling branches:
// no argument (default source), a nil argument, an override Hash carrying every
// value type (nil/bool/int/float/string/symbol/array/nested-hash) plus a
// non-Symbol/String key, and the TypeError guards for a non-Hash override and a
// non-Hash Site.new argument.
func TestJekyllConfiguration(t *testing.T) {
	src := `
require "jekyll"

# No argument: the built-in defaults (source ".").
raise "default" unless Jekyll.configuration["markdown"] == "kramdown"
# A nil argument is the empty override.
raise "nil" unless Jekyll.configuration(nil)["markdown"] == "kramdown"

# An override Hash exercising every config value type and a non-symbol/string key.
cfg = Jekyll.configuration({
  "a" => nil, "b" => true, "c" => 3, "d" => 1.5,
  "e" => "s", "f" => :sym, "g" => [1, "x"], "h" => {"k" => 1},
  7 => "seven", :sk => 9,
})
raise "nil val" unless cfg["a"].nil?
raise "bool val" unless cfg["b"] == true
raise "int val" unless cfg["c"] == 3
raise "float val" unless cfg["d"] == 1.5
raise "str val" unless cfg["e"] == "s"
raise "sym val" unless cfg["f"] == "sym"
raise "arr val" unless cfg["g"] == [1, "x"]
raise "hash val" unless cfg["h"]["k"] == 1
raise "int key" unless cfg["7"] == "seven"
raise "sym key" unless cfg["sk"] == 9

# Arity + TypeError guards.
raise "site arity" unless (begin; Jekyll::Site.new; "no"; rescue ArgumentError; "raised"; end) == "raised"
raise "override type" unless (begin; Jekyll.configuration(5); "no"; rescue TypeError; "raised"; end) == "raised"
raise "site type" unless (begin; Jekyll::Site.new(5); "no"; rescue TypeError; "raised"; end) == "raised"

puts "ok"
`
	if got := runSrc(t, src); got != "ok" {
		t.Fatalf("configuration output = %q", got)
	}
}

// TestJekyllErrors covers the raise paths reachable from Ruby: the build arity
// guard, a config-load failure (a malformed _config.yml), and a build/process
// failure (a page requesting a missing layout) through both Jekyll.build and
// Jekyll::Site#process.
func TestJekyllErrors(t *testing.T) {
	// Malformed _config.yml -> LoadConfig error -> Jekyll::Error.
	badCfg := t.TempDir()
	writeJekyllFile(t, badCfg, "_config.yml", "\t: : bad yaml : :\n")

	// A page requesting a layout that does not exist -> Build error.
	badLayout := t.TempDir()
	writeJekyllFile(t, badLayout, "index.html", "---\nlayout: nope\n---\nhi\n")
	dest := t.TempDir()

	prog := "\nrequire \"jekyll\"\n" +
		"raise \"arity\" unless (begin; Jekyll.build(\"only-one\"); \"no\"; rescue ArgumentError; \"raised\"; end) == \"raised\"\n" +
		"raise \"config\" unless (begin; Jekyll.configuration({\"source\" => " + rubyStr(badCfg) + "}); \"no\"; rescue Jekyll::Error; \"raised\"; end) == \"raised\"\n" +
		"raise \"build\" unless (begin; Jekyll.build(" + rubyStr(badLayout) + ", " + rubyStr(dest) + "); \"no\"; rescue Jekyll::Error; \"raised\"; end) == \"raised\"\n" +
		"raise \"process\" unless (begin; Jekyll::Site.new(Jekyll.configuration({\"source\" => " + rubyStr(badLayout) + ", \"destination\" => " + rubyStr(dest) + "})).process; \"no\"; rescue Jekyll::Error; \"raised\"; end) == \"raised\"\n" +
		"puts \"ok\"\n"
	if got := runSrc(t, prog); got != "ok" {
		t.Fatalf("errors output = %q", got)
	}
}

// TestJekyllSiteShell covers the native handle's ToS/Inspect/Truthy and the
// uninitialized-handle raise branch that Ruby cannot reach (new always sets it).
func TestJekyllSiteShell(t *testing.T) {
	h := &jekyllSite{}
	if h.ToS() != "#<Jekyll::Site>" || h.Inspect() != h.ToS() || !h.Truthy() {
		t.Errorf("shell: %q / %q / %v", h.ToS(), h.Inspect(), h.Truthy())
	}
	inst := &RObject{class: newClass("X", nil), ivars: map[string]object.Value{}}
	if cls := bindRecover(func() { jekyllSiteHandle(inst) }); cls != "Jekyll::Error" {
		t.Errorf("uninitialized handle class = %q", cls)
	}
}

// TestJekyllMarshalUnreachable covers the marshalling branches no Ruby program
// reaches: jekyllValueToConfig's TypeError on an unmappable value (a Range),
// jekyllKey's to_s fallback for a non-String/Symbol key, and jekyllAnyToValue's
// String fallback for a Go value with no config peer (a time.Time).
func TestJekyllMarshalUnreachable(t *testing.T) {
	rng := object.NewRange(object.Integer(1), object.Integer(2), false)
	if cls := bindRecover(func() { jekyllValueToConfig(rng) }); cls != "TypeError" {
		t.Errorf("unmappable value class = %q", cls)
	}
	if got := jekyllKey(object.Integer(7)); got != "7" {
		t.Errorf("integer key -> %q", got)
	}
	when := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	if s, ok := jekyllAnyToValue(when).(*object.String); !ok || !strings.Contains(s.Str(), "2026") {
		t.Errorf("time value -> %#v", jekyllAnyToValue(when))
	}
}
