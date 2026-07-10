// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	stdbinary "encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// fgRun runs a Ruby program with `require "fast_gettext"` prepended.
func fgRun(t *testing.T, body string) string {
	t.Helper()
	return runSrc(t, "require \"fast_gettext\"\n"+body)
}

// fgBuildMO assembles a big-endian GNU .mo file from ordered (orig, trans) pairs.
// The empty msgid carries the metadata header (charset + Plural-Forms); a plural
// entry stores msgid + "\x00" + msgid_plural against form0 + "\x00" + form1.
func fgBuildMO(pairs [][2]string) []byte {
	order := stdbinary.BigEndian
	n := len(pairs)
	header := 28
	origTab := header
	transTab := header + 8*n
	dataStart := header + 16*n

	var blob []byte
	origLO := make([][2]uint32, n)
	transLO := make([][2]uint32, n)
	for i, p := range pairs {
		origLO[i] = [2]uint32{uint32(len(p[0])), uint32(dataStart + len(blob))}
		blob = append(blob, p[0]...)
		blob = append(blob, 0)
	}
	for i, p := range pairs {
		transLO[i] = [2]uint32{uint32(len(p[1])), uint32(dataStart + len(blob))}
		blob = append(blob, p[1]...)
		blob = append(blob, 0)
	}

	buf := make([]byte, dataStart)
	copy(buf[0:4], []byte{0x95, 0x04, 0x12, 0xde})
	order.PutUint32(buf[4:], 0)
	order.PutUint32(buf[8:], uint32(n))
	order.PutUint32(buf[12:], uint32(origTab))
	order.PutUint32(buf[16:], uint32(transTab))
	order.PutUint32(buf[20:], 0)
	order.PutUint32(buf[24:], 0)
	for i := 0; i < n; i++ {
		order.PutUint32(buf[origTab+i*8:], origLO[i][0])
		order.PutUint32(buf[origTab+i*8+4:], origLO[i][1])
		order.PutUint32(buf[transTab+i*8:], transLO[i][0])
		order.PutUint32(buf[transTab+i*8+4:], transLO[i][1])
	}
	return append(buf, blob...)
}

// fgWriteFile writes data under dir/name, creating parent directories.
func fgWriteFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestFastGettextMoFile covers the on-disk .mo loading path: a real .mo written to
// <path>/<locale>/LC_MESSAGES/<domain>.mo is discovered, parsed and its singular
// and plural msgids translate.
func TestFastGettextMoFile(t *testing.T) {
	dir := t.TempDir()
	mo := fgBuildMO([][2]string{
		{"", "Content-Type: text/plain; charset=UTF-8\nPlural-Forms: nplurals=2; plural=(n != 1);\n"},
		{"Hello", "Bonjour"},
		{"car\x00cars", "Auto\x00Autos"},
	})
	fgWriteFile(t, dir, "fr/LC_MESSAGES/app.mo", mo)

	got := fgRun(t, `
FastGettext.available_locales = ["fr"]
FastGettext.add_text_domain("app", path: "`+filepath.ToSlash(dir)+`")
FastGettext.text_domain = "app"
FastGettext.locale = "fr"
puts FastGettext._("Hello")
puts FastGettext._("Missing")
puts FastGettext.n_("car", "cars", 1)
puts FastGettext.n_("car", "cars", 3)
`)
	want := "Bonjour\nMissing\nAuto\nAutos"
	if got != want {
		t.Fatalf("mo file:\n got=%q\nwant=%q", got, want)
	}
}

// TestFastGettextPoFile covers the on-disk .po loading path (type: :po): a real
// .po written to <path>/<locale>/<domain>.po is discovered and translates.
func TestFastGettextPoFile(t *testing.T) {
	dir := t.TempDir()
	po := "msgid \"\"\nmsgstr \"Content-Type: text/plain; charset=UTF-8\\n\"\n\n" +
		"msgid \"Hello\"\nmsgstr \"Hallo\"\n"
	fgWriteFile(t, dir, "de/app.po", []byte(po))

	got := fgRun(t, `
FastGettext.available_locales = ["de"]
FastGettext.add_text_domain("app", path: "`+filepath.ToSlash(dir)+`", type: :po)
FastGettext.text_domain = "app"
FastGettext.locale = "de"
puts FastGettext._("Hello")
`)
	if got != "Hallo" {
		t.Fatalf("po file: got=%q want=Hallo", got)
	}
}

// TestFastGettextFileMissing covers the load-failure path: a path with no locale
// directory (a bare regular file at the root cannot be read as a directory tree)
// raises a RuntimeError.
func TestFastGettextFileMissing(t *testing.T) {
	missing := filepath.ToSlash(filepath.Join(t.TempDir(), "does-not-exist"))
	for _, typ := range []string{":mo", ":po"} {
		got := fgRun(t, `
begin
  FastGettext.add_text_domain("app", path: "`+missing+`", type: `+typ+`)
rescue RuntimeError => e
  puts e.class
end
`)
		if got != "RuntimeError" {
			t.Fatalf("missing %s path: got=%q want=RuntimeError", typ, got)
		}
	}
}

// TestFastGettextFlow drives the headline flow: register an in-memory domain
// (singular + plural translations), select it and a locale, then translate
// through _ / n_ / s_ / p_ and switch locale.
func TestFastGettextFlow(t *testing.T) {
	got := fgRun(t, `
FastGettext.available_locales = ["en", "de"]
puts FastGettext.available_locales.inspect
FastGettext.add_text_domain("app",
  translations: {"en" => {"Hello" => "Hello!", "menu|File" => "File!"}, "de" => {"Hello" => "Hallo"}},
  plurals: {"en" => {["apple", "apples"] => ["apple", "apples"]}})
FastGettext.text_domain = "app"
puts FastGettext.text_domain
FastGettext.locale = "en"
puts FastGettext.locale
puts FastGettext._("Hello")
puts FastGettext._("Missing")
puts FastGettext.n_("apple", "apples", 1)
puts FastGettext.n_("apple", "apples", 2)
puts FastGettext.s_("menu|File")
puts FastGettext.s_("nomenu|Bar")
puts FastGettext.p_("ctx", "key")
puts FastGettext.key_exist?("Hello")
puts FastGettext.key_exist?("Nope")
FastGettext.locale = "de"
puts FastGettext._("Hello")
r = FastGettext.with_locale("en") { FastGettext::Translation._("Hello") }
puts r
puts FastGettext.locale
puts FastGettext.set_locale("en")
`)
	want := `["en", "de"]` + "\napp\nen\nHello!\nMissing\napple\napples\nFile!\nBar\nkey\ntrue\nfalse\nHallo\nHello!\nde\nen"
	if got != want {
		t.Fatalf("flow:\n got=%q\nwant=%q", got, want)
	}
}

// TestFastGettextDefaults covers the default_locale / default_text_domain
// setters and readers (nil when unset), an empty domain registration, and
// with_domain returning its block value.
func TestFastGettextDefaults(t *testing.T) {
	got := fgRun(t, `
puts FastGettext.text_domain.inspect
puts FastGettext.default_locale.inspect
puts FastGettext.default_text_domain.inspect
FastGettext.default_locale = "en"
puts FastGettext.default_locale
FastGettext.default_text_domain = "app"
puts FastGettext.default_text_domain
FastGettext.add_text_domain("empty")
puts FastGettext.with_domain("empty") { 42 }
`)
	want := "nil\nnil\nnil\nen\napp\n42"
	if got != want {
		t.Fatalf("defaults:\n got=%q\nwant=%q", got, want)
	}
}

// TestFastGettextStringKeys covers keyword Hashes with string keys (the fgKwGet
// string fallback), the s_ separator form, and the fgIntArg non-integer count
// (treated as zero, selecting the plural form under the default rule).
func TestFastGettextStringKeys(t *testing.T) {
	got := fgRun(t, `
FastGettext.available_locales = ["en"]
FastGettext.add_text_domain("d",
  "translations" => {"en" => {"menu/File" => "F2"}},
  "plurals" => {"en" => {["a", "b"] => ["a", "b"]}})
FastGettext.text_domain = "d"
FastGettext.locale = "en"
puts FastGettext.s_("menu/File", "/")
puts FastGettext.n_("a", "b", "x")
`)
	want := "F2\nb"
	if got != want {
		t.Fatalf("string keys:\n got=%q\nwant=%q", got, want)
	}
}

// TestFastGettextMalformedDomain covers the defensive skip branches in the
// translation / plural loaders: a non-Hash translations / plurals value, a
// non-Hash per-locale entry, and a plural entry whose key or forms is not an
// Array. None of these register anything, so the keys stay untranslated.
func TestFastGettextMalformedDomain(t *testing.T) {
	got := fgRun(t, `
FastGettext.available_locales = ["en"]
FastGettext.add_text_domain("bad1", translations: "nope", plurals: "nope")
FastGettext.add_text_domain("bad2", translations: {"en" => "nope"}, plurals: {"en" => "nope"})
FastGettext.add_text_domain("bad3", plurals: {"en" => {"k" => "v", ["s", "p"] => "notarray"}})
FastGettext.text_domain = "bad3"
FastGettext.locale = "en"
puts FastGettext._("Hello")
`)
	if got != "Hello" {
		t.Fatalf("malformed domain: got=%q want=Hello", got)
	}
}

// TestFastGettextArgErrors covers the ArgumentError paths: _ / n_ / p_ with too
// few arguments, available_locales= with a non-Array, and available_locales=
// invoked with no argument.
func TestFastGettextArgErrors(t *testing.T) {
	cases := []string{
		"FastGettext._",
		`FastGettext.n_("a", "b")`,
		`FastGettext.p_("ctx")`,
		`FastGettext.available_locales = 42`,
		`FastGettext.send(:available_locales=)`,
	}
	for _, expr := range cases {
		got := fgRun(t, "begin; "+expr+"; rescue => e; puts e.class; end")
		if got != "ArgumentError" {
			t.Fatalf("%s expected ArgumentError, got %q", expr, got)
		}
	}
}
