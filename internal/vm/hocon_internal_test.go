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

// hoconRun runs a Ruby program with `require "hocon"` prepended.
func hoconRun(t *testing.T, body string) string {
	t.Helper()
	return runSrc(t, "require \"hocon\"\n"+body)
}

// hoconDoc is a document exercising every value type and unit suffix.
const hoconDoc = `a { b = 1, c = "hi", d = true, e = 3.5, list = [1, "two", false], nested { x = 9 }, nul = null }, dur = 5s, size = 10kB`

// TestHoconTypedAccessors covers the typed path accessors, get_config,
// has_path?, get_duration and get_bytes.
func TestHoconTypedAccessors(t *testing.T) {
	got := hoconRun(t, "c = Hocon.parse('"+hoconDoc+"')\n"+`
puts c.get_int("a.b")
puts c.get_string("a.c")
puts c.get_boolean("a.d")
puts c.get_double("a.e")
puts c.get_list("a.list").inspect
puts c.get_config("a.nested").class
puts c.get_config("a.nested").get_int("x")
puts c.has_path?("a.b")
puts c.has_path?("a.zzz")
puts c.get_duration("dur")
puts c.get_bytes("size")
puts c.render.class
`)
	want := "1\nhi\ntrue\n3.5\n" + `[1, "two", false]` + "\nHocon::Config\n9\ntrue\nfalse\n5000000000\n10000\nString"
	if got != want {
		t.Fatalf("typed accessors:\n got=%q\nwant=%q", got, want)
	}
}

// TestHoconIndexAndRoot covers [](path) over every value type (converted to a
// native Ruby value, nil for an absent path), and root as a native Hash.
func TestHoconIndexAndRoot(t *testing.T) {
	got := hoconRun(t, "c = Hocon.parse('"+hoconDoc+"')\n"+`
puts c["a.c"]
puts c["a.b"]
puts c["a.d"]
puts c["a.e"]
puts c["a.list"].inspect
puts c["a.nested"].inspect
puts c["a.nul"].inspect
puts c["a.zzz"].inspect
puts c.root.class
puts c.root["a"].class
`)
	want := "hi\n1\ntrue\n3.5\n" + `[1, "two", false]` + "\n" + `{"x" => 9}` + "\nnil\nnil\nHash\nHash"
	if got != want {
		t.Fatalf("index/root:\n got=%q\nwant=%q", got, want)
	}
}

// TestHoconConfigFactory covers ConfigFactory.parse_string / parse_file and
// with_fallback.
func TestHoconConfigFactory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.conf")
	if err := os.WriteFile(path, []byte("server { port = 8080 }"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := hoconRun(t, fmt.Sprintf(`
puts Hocon::ConfigFactory.parse_string("x = 1").get_int("x")
puts Hocon::ConfigFactory.parse_file(%q).get_int("server.port")
base = Hocon.parse("a = 1")
m = Hocon.parse("b = 2").with_fallback(base)
puts m.get_int("a")
puts m.get_int("b")
`, path))
	want := "1\n8080\n1\n2"
	if got != want {
		t.Fatalf("config factory:\n got=%q\nwant=%q", got, want)
	}
}

// TestHoconErrors covers the Hocon::ConfigError raising paths (a parse error, a
// missing-path accessor, a parse_file read failure) and the ArgumentErrors
// (missing document / path arguments, with_fallback without / with a non-Config
// argument).
func TestHoconErrors(t *testing.T) {
	cfgErr := []string{
		`Hocon.parse("a { b ")`,
		`Hocon.parse("a = 1").get_int("nope")`,
		`Hocon::ConfigFactory.parse_file("/no/such/file.conf")`,
	}
	for _, expr := range cfgErr {
		got := hoconRun(t, "begin; "+expr+"; rescue => e; puts e.class; end")
		if got != "Hocon::ConfigError" {
			t.Fatalf("%s expected Hocon::ConfigError, got %q", expr, got)
		}
	}

	argErr := []string{
		"Hocon.parse",
		`Hocon::ConfigFactory.parse_string`,
		`Hocon.parse("a=1").get_int`,
		`Hocon.parse("a=1").with_fallback`,
		`Hocon.parse("a=1").with_fallback(42)`,
	}
	for _, expr := range argErr {
		got := hoconRun(t, "begin; "+expr+"; rescue => e; puts e.class; end")
		if got != "ArgumentError" {
			t.Fatalf("%s expected ArgumentError, got %q", expr, got)
		}
	}
}

// TestHoconStringers covers the object.Value marker methods on the wrapper.
func TestHoconStringers(t *testing.T) {
	o := &HoconConfig{}
	if o.ToS() != "#<Hocon::Config>" || o.Inspect() != o.ToS() || !o.Truthy() {
		t.Errorf("hocon stringers = %q / %q / %v", o.ToS(), o.Inspect(), o.Truthy())
	}
}
