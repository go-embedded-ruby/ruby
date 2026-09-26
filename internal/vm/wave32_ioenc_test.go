// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIOOpenTimeEncodingResolution pins io.c rb_io_ext_int_to_encs — the single
// place MRI turns a (named external, named internal) pair into the two
// encodings it stores on a stream, reached from rb_io_extract_modeenc at open,
// from parse_mode_enc for a ":ext:int" mode suffix, from
// rb_io_extract_encoding_option for the :encoding family, and from
// io_encoding_set for #set_encoding.
//
// The behaviour it exists to hold is that the resolution happens ONCE, when the
// stream is opened: rb_io_extract_modeenc opens with
// `rb_io_ext_int_to_encs(NULL, NULL, &enc, &enc2, 0)`, so a stream opened while
// Encoding.default_internal is set carries that internal encoding for the rest
// of its life whatever the defaults do afterwards. The one thing NOT frozen is a
// defaulted external with no internal encoding: MRI then records nothing at all
// (enc == enc2 == NULL) and io_read_encoding keeps answering
// Encoding.default_external as it changes.
//
// Every expectation below was taken from MRI ruby 4.0.5 running the same source.
func TestIOOpenTimeEncodingResolution(t *testing.T) {
	dir := ioScratchDir(t)
	path := filepath.Join(dir, "enc.txt")
	if err := os.WriteFile(path, []byte("line\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	lit := `"` + strings.ReplaceAll(path, `\`, `\\`) + `"`

	cases := []struct{ name, body, want string }{
		{
			// default_internal set and different: the pair is snapshotted.
			"snapshot_pair",
			`Encoding.default_external = Encoding::IBM437
			 Encoding.default_internal = Encoding::IBM866
			 f = File.open(P); p [f.external_encoding.name, f.internal_encoding.name]; f.close`,
			"[\"IBM437\", \"IBM866\"]\n",
		},
		{
			// …and it does not move when the default does afterwards.
			"snapshot_is_frozen",
			`Encoding.default_external = Encoding::IBM437
			 Encoding.default_internal = Encoding::IBM866
			 f = File.open(P)
			 Encoding.default_internal = Encoding::UTF_8
			 p [f.external_encoding.name, f.internal_encoding.name]; f.close`,
			"[\"IBM437\", \"IBM866\"]\n",
		},
		{
			// intern == ext: no transcoding, but the external is still recorded.
			"internal_equal_external",
			`Encoding.default_external = Encoding::IBM866
			 Encoding.default_internal = Encoding::IBM866
			 f = File.open(P); p [f.external_encoding.name, f.internal_encoding]; f.close`,
			"[\"IBM866\", nil]\n",
		},
		{
			// A BINARY external drops the internal encoding outright.
			"binary_external_drops_internal",
			`Encoding.default_external = Encoding::BINARY
			 Encoding.default_internal = Encoding::IBM866
			 f = File.open(P); p [f.external_encoding.name, f.internal_encoding]; f.close`,
			"[\"ASCII-8BIT\", nil]\n",
		},
		{
			// The one case that is NOT frozen: nothing was recorded, so the stream
			// keeps following Encoding.default_external.
			"defaulted_external_stays_dynamic",
			`Encoding.default_internal = nil
			 f = File.open(P)
			 Encoding.default_external = Encoding::IBM437
			 p [f.external_encoding.name, f.internal_encoding]; f.close`,
			"[\"IBM437\", nil]\n",
		},
		{
			// A mode suffix names only the external: parse_mode_enc still hands
			// rb_io_ext_int_to_encs a NULL internal, so default_internal applies.
			"mode_suffix_picks_up_default_internal",
			`Encoding.default_external = Encoding::IBM437
			 Encoding.default_internal = Encoding::IBM866
			 f = File.open(P, "r:utf-8"); p [f.external_encoding.name, f.internal_encoding.name]; f.close`,
			"[\"UTF-8\", \"IBM866\"]\n",
		},
		{
			// A ":-" suffix is parse_mode_enc's Qnil: transcoding refused.
			"mode_suffix_dash_refuses_internal",
			`Encoding.default_external = Encoding::IBM437
			 Encoding.default_internal = Encoding::IBM866
			 f = File.open(P, "r:utf-8:-"); p [f.external_encoding.name, f.internal_encoding]; f.close`,
			"[\"UTF-8\", nil]\n",
		},
		{
			// internal_encoding: nil is Qnil too — and, unlike external_encoding:
			// nil, it is NOT the same as omitting the option.
			"internal_encoding_nil_refuses_the_default",
			`Encoding.default_external = Encoding::IBM437
			 Encoding.default_internal = Encoding::IBM866
			 f = File.open(P, internal_encoding: nil); p [f.external_encoding.name, f.internal_encoding]; f.close`,
			"[\"IBM437\", nil]\n",
		},
		{
			// external_encoding: alone leaves the internal NULL, so the default
			// still applies.
			"external_encoding_option_picks_up_default_internal",
			`Encoding.default_external = Encoding::IBM437
			 Encoding.default_internal = Encoding::IBM866
			 f = File.open(P, external_encoding: "utf-8"); p [f.external_encoding.name, f.internal_encoding.name]; f.close`,
			"[\"UTF-8\", \"IBM866\"]\n",
		},
		{
			// io_encoding_set runs the same resolution: an external named ALONE
			// still picks up default_internal, which is what makes
			// `gets.encoding` UTF-8 here rather than IBM866.
			"set_encoding_external_only_picks_up_default_internal",
			`Encoding.default_external = Encoding::BINARY
			 Encoding.default_internal = Encoding::UTF_8
			 f = File.open(P, "r"); f.set_encoding Encoding::IBM866
			 p [f.external_encoding.name, f.internal_encoding.name, f.gets.encoding.name]; f.close`,
			"[\"IBM866\", \"UTF-8\", \"UTF-8\"]\n",
		},
		{
			"set_encoding_external_only_no_default_internal",
			`Encoding.default_internal = nil
			 f = File.open(P); f.set_encoding Encoding::IBM866
			 p [f.external_encoding.name, f.internal_encoding]; f.close`,
			"[\"IBM866\", nil]\n",
		},
		{
			// set_encoding(nil, nil) is rb_io_ext_int_to_encs(NULL, NULL) again.
			"set_encoding_nil_nil_resnapshots",
			`Encoding.default_external = Encoding::UTF_8
			 Encoding.default_internal = Encoding::IBM866
			 f = File.open(P); f.set_encoding(nil, nil)
			 p [f.external_encoding.name, f.internal_encoding.name]; f.close`,
			"[\"UTF-8\", \"IBM866\"]\n",
		},
		{
			// A "b" mode resolves ASCII-8BIT as a NAMED external, so — unlike a
			// defaulted BINARY external — the name is recorded rather than left NULL.
			"binmode_names_ascii_8bit",
			`Encoding.default_external = Encoding::IBM437
			 Encoding.default_internal = Encoding::IBM866
			 f = File.open(P, "rb"); p [f.external_encoding.name, f.internal_encoding]; f.close`,
			"[\"ASCII-8BIT\", nil]\n",
		},
		{
			// IO.readlines reaches rb_io_open through open_key_args, so it gets the
			// same snapshot: the lines are TRANSCODED, not merely tagged.
			"readlines_transcodes_to_default_internal",
			`Encoding.default_external = Encoding::UTF_8
			 Encoding.default_internal = Encoding::UTF_16LE
			 p IO.readlines(P).map { |s| s.encoding.name }`,
			"[\"UTF-16LE\"]\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "P = " + lit + "\n" + c.body + "\n"
			if got := eval(t, src); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
