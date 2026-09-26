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

// TestIOSysWriteGoesToTheDescriptor pins io.c rb_io_syswrite and
// io_write_nonblock, which differ from IO#write in four ways that all follow
// from writing to the DESCRIPTOR rather than through the stream:
// rb_obj_as_string on a non-String argument, rb_io_check_writable with no
// zero-length shortcut, the string's OWN bytes (no econv, so an external
// encoding does not transcode them), and nothing left in the write buffer.
//
// Every expectation was taken from MRI ruby 4.0.5 running the same source.
func TestIOSysWriteGoesToTheDescriptor(t *testing.T) {
	dir := ioScratchDir(t)
	q := func(name string) string {
		return `"` + strings.ReplaceAll(filepath.Join(dir, name), `\`, `\\`) + `"`
	}

	cases := []struct{ name, body, want string }{
		{
			// The bytes are on disk before the stream is closed.
			"syswrite_does_not_buffer",
			`File.write(A, "0123456789")
			 f = File.open(A, "r+"); n = f.syswrite("abcde")
			 p [n, File.read(A)]; f.close`,
			"[5, \"abcde56789\"]\n",
		},
		{
			"write_nonblock_does_not_buffer",
			`File.write(A, "0123456789")
			 f = File.open(A, "r+"); n = f.write_nonblock("abcde")
			 p [n, File.read(A)]; f.close`,
			"[5, \"abcde56789\"]\n",
		},
		{
			// rb_obj_as_string, before the stream is even looked at.
			"syswrite_coerces_with_to_s",
			`class Q; def to_s; "QQ"; end; end
			 f = File.open(A, "w"); n = f.syswrite(Q.new); f.close
			 p [n, File.read(A)]`,
			"[2, \"QQ\"]\n",
		},
		{
			// No econv on this path: the stream's external encoding is ignored.
			"syswrite_does_not_transcode",
			`f = File.open(A, "w", external_encoding: Encoding::UTF_16BE)
			 f.syswrite("hello"); f.close
			 p File.binread(A).bytes`,
			"[104, 101, 108, 108, 111]\n",
		},
		{
			"write_nonblock_does_not_transcode",
			`f = File.open(A, "w", external_encoding: Encoding::UTF_16BE)
			 f.write_nonblock("hello"); f.close
			 p File.binread(A).bytes`,
			"[104, 101, 108, 108, 111]\n",
		},
		{
			// io_write returns early for an all-empty write; these two do not, so
			// the writability check is still reached.
			"empty_write_still_checks_writable",
			`File.write(A, "x")
			 f = File.open(A, "r")
			 r = []
			 begin; f.syswrite(""); rescue IOError => e; r << e.message; end
			 begin; f.write_nonblock(""); rescue IOError => e; r << e.message; end
			 f.close; p r`,
			"[\"not opened for writing\", \"not opened for writing\"]\n",
		},
		{
			// write(2) on a NON-BLOCKING pipe takes what fits and returns that
			// count; the same call on a blocking one takes the lot.
			"short_write_on_a_nonblocking_pipe",
			`require "io/nonblock"
			 big = 2 * 1024 * 1024
			 r, w = IO.pipe; w.nonblock = true
			 n = w.syswrite("a" * big)
			 p [n > 0, n < big]`,
			"[true, true]\n",
		},
		{
			// rb_scan_args "1": exactly one argument, checked before anything else.
			"syswrite_arity",
			`f = File.open(A, "w"); r = []
			 begin; f.syswrite("a", "b"); rescue ArgumentError => e; r << e.message; end
			 begin; f.syswrite; rescue ArgumentError => e; r << e.message; end
			 f.close; p r`,
			"[\"wrong number of arguments (given 2, expected 1)\", \"wrong number of arguments (given 0, expected 1)\"]\n",
		},
		{
			// A write that fits takes the whole string and the bytes arrive intact.
			"pipe_write_that_fits_is_whole",
			`r, w = IO.pipe
			 p [w.syswrite("abc"), r.read(3)]`,
			"[3, \"abc\"]\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "A = " + q(c.name+".txt") + "\n" + c.body + "\n"
			if got := eval(t, src); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestIOSysWriteWarnsAfterBufferedWrite covers rb_io_syswrite's
// `if (fptr->wbuf.len) rb_warn("syswrite for buffered IO")` — bytes a buffered
// #write accepted but has not put on disk, which is what wbufDirty stands in
// for. A #read leaves nothing buffered, so it must NOT warn.
func TestIOSysWriteWarnsAfterBufferedWrite(t *testing.T) {
	dir := ioScratchDir(t)
	q := func(name string) string {
		return `"` + strings.ReplaceAll(filepath.Join(dir, name), `\`, `\\`) + `"`
	}
	for _, c := range []struct{ name, body, want string }{
		{"after_buffered_write", `f = File.open(A, "w"); f.write("abcde"); f.syswrite("fg"); f.close`, "syswrite for buffered IO"},
		{"after_read", `File.write(A, "0123456789")
		                f = File.open(A, "r+"); f.read(5); f.syswrite("fg"); f.close`, ""},
		{"when_sync", `f = File.open(A, "w"); f.sync = true; f.write("abcde"); f.syswrite("fg"); f.close`, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			// eval's VM sends $stderr to the same buffer as $stdout, so the
			// warning lands in the captured output.
			src := "A = " + q(c.name+".txt") + "\n$VERBOSE = true\n" + c.body + "\n"
			got := eval(t, src)
			switch {
			case c.want == "" && strings.Contains(got, "syswrite"):
				t.Errorf("warned when it should not: %q", got)
			case c.want != "" && !strings.Contains(got, c.want):
				t.Errorf("got stderr %q, want it to contain %q", got, c.want)
			}
		})
	}
}

// TestCopyStreamSrcOffsetOnAPipe covers the two DIFFERENT refusals io.c makes
// for IO.copy_stream's src_offset. copy_stream_fallback raises the ArgumentError
// only when the source has no fptr at all — a StringIO, or a duck-typed object.
// A pipe IS an IO, so it reaches maygvl_copy_stream_read and preads, which on a
// pipe fails with ESPIPE.
func TestCopyStreamSrcOffsetOnAPipe(t *testing.T) {
	dir := ioScratchDir(t)
	out := `"` + strings.ReplaceAll(filepath.Join(dir, "copy.out"), `\`, `\\`) + `"`
	src := `require "stringio"
O = ` + out + `
r, w = IO.pipe; w.write("Line one\nLine two\n"); w.close
res = []
begin; IO.copy_stream(r, O, 8, 4); rescue SystemCallError => e; res << [e.class.name, e.message]; end
begin; IO.copy_stream(StringIO.new("abcdefghijkl"), O, 8, 4); rescue ArgumentError => e; res << e.message; end
p res
`
	want := "[[\"Errno::ESPIPE\", \"Illegal seek - pread\"], \"cannot specify src_offset for non-IO\"]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestDirEntryEncoding pins dir.c's handling of the names a directory hands
// back. dir_initialize records dp->enc — the `encoding:` keyword, else
// rb_filesystem_encoding() — and dir_read / dir_each_entry pass every name
// through rb_external_str_new_with_enc, which tags it with that encoding and
// then converts it to Encoding.default_internal when one is set.
//
// The conversion is rb_str_conv_enc, not String#encode: its last line is
// "/* some error, return original */ return str;", so a name that cannot be
// represented in the internal encoding comes back AS IS rather than raising.
//
// Every expectation was taken from MRI ruby 4.0.5 running the same source.
func TestDirEntryEncoding(t *testing.T) {
	dir := ioScratchDir(t)
	// One plain ASCII name and one that no Korean encoding can represent, so both
	// halves of the best-effort conversion are exercised by the same listing.
	for _, n := range []string{"a.txt", "\U0001F389.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o644); err != nil {
			t.Fatalf("fixture %q: %v", n, err)
		}
	}
	lit := `"` + strings.ReplaceAll(dir, `\`, `\\`) + `"`

	for _, c := range []struct{ name, body, want string }{
		{
			"children_encoding_keyword",
			`p Dir.children(D, encoding: "euc-jp").sort.map { |s| s.encoding.name }`,
			"[\"EUC-JP\", \"EUC-JP\"]\n",
		},
		{
			"entries_encoding_keyword",
			`p Dir.entries(D, encoding: Encoding::ISO_8859_1).sort.first.encoding.name`,
			"\"ISO-8859-1\"\n",
		},
		{
			// The keyword must survive the BLOCK-LESS form too, which goes through
			// an Enumerator rather than the yield loop.
			"foreach_enumerator_keeps_the_keyword",
			`p Dir.foreach(D, encoding: "iso-8859-1").to_a.map { |s| s.encoding.name }.uniq`,
			"[\"ISO-8859-1\"]\n",
		},
		{
			"each_child_enumerator_keeps_the_keyword",
			`p Dir.each_child(D, encoding: "iso-8859-1").to_a.map { |s| s.encoding.name }.uniq`,
			"[\"ISO-8859-1\"]\n",
		},
		{
			"handle_carries_its_encoding",
			`d = Dir.new(D, encoding: "euc-jp")
			 r = [d.children.sort.first.encoding.name, d.read.encoding.name]
			 d.close; p r`,
			"[\"EUC-JP\", \"EUC-JP\"]\n",
		},
		{
			// dp->path is the argument as it was passed, kept before
			// rb_str_encode_ospath, so #to_path round-trips its encoding.
			"to_path_keeps_the_arguments_encoding",
			`d = Dir.open(D.dup.force_encoding(Encoding::IBM866))
			 r = d.to_path.encoding.name; d.close; p r`,
			"\"IBM866\"\n",
		},
		{
			// rb_external_str_with_enc's own special case: a US-ASCII handle
			// encoding over bytes that are not ASCII gives ASCII-8BIT instead.
			"us_ascii_handle_falls_back_to_binary",
			`p Dir.children(D, encoding: "us-ascii").sort.map { |s| [s.encoding.name, s.bytesize] }`,
			"[[\"US-ASCII\", 5], [\"ASCII-8BIT\", 8]]\n",
		},
		{
			// FilePathValue coerces first, so dp->path is the COERCED String: a
			// Pathname argument leaves a plain UTF-8 path, not its own encoding.
			"to_path_of_a_coerced_argument",
			`require "pathname"
			 d = Dir.open(Pathname.new(D)); r = [d.to_path == D, d.to_path.encoding.name]
			 d.close; p r`,
			"[true, \"UTF-8\"]\n",
		},
		{
			// The convertible name becomes EUC-KR; the one that cannot be
			// represented comes back untouched, in the filesystem encoding.
			"unconvertible_name_comes_back_as_is",
			`Encoding.default_internal = Encoding::EUC_KR
			 r = Dir.children(D).sort.map { |s| s.encoding.name }
			 Encoding.default_internal = nil
			 p r`,
			"[\"EUC-KR\", \"UTF-8\"]\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := "D = " + lit + "\n" + c.body + "\n"
			if got := eval(t, src); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestIOBufferNullEmptyAndResize pins the parts of io_buffer.c that turn on
// what `buffer->base` and `buffer->size` are:
//
//   - rb_io_buffer_null_p is `base == NULL` and rb_io_buffer_empty_p is
//     `size == 0`. They are different questions, and a buffer that gave up its
//     memory (#free, #resize(0), #transfer) answers true to BOTH;
//   - a slice takes `parent->base + offset` ONCE, when it is cut, so a slice of
//     a null buffer is null while a slice whose parent is freed afterwards is
//     not null, only invalid;
//   - rb_io_buffer_resize re-initializes a NULL-base buffer with
//     io_flags_for_size(size), which is why a resized 0-sized buffer can come
//     back MAPPED, and (without mremap) copies a mapped one into a buffer whose
//     kind is likewise recomputed from the new size.
//
// Every expectation was taken from MRI ruby 4.0.5 running the same source.
func TestIOBufferNullEmptyAndResize(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"freed", `b = IO::Buffer.new(4); b.free; p [b.null?, b.empty?, b.size]`, "[true, true, 0]\n"},
		{"resized_to_zero", `b = IO::Buffer.new(4); b.resize(0); p [b.null?, b.empty?, b.size]`, "[true, true, 0]\n"},
		{"transferred", `b = IO::Buffer.new(4); b.transfer; p [b.null?, b.empty?, b.size]`, "[true, true, 0]\n"},
		{"slice_of_a_null_buffer", `s = IO::Buffer.new(0).slice(0, 0); p [s.null?, s.empty?, s.size]`, "[true, true, 0]\n"},
		{
			// The slice's base was taken while the parent still had memory, so
			// freeing the parent leaves it DANGLING, not null.
			"slice_of_a_freed_parent_dangles",
			`b = IO::Buffer.new(4); s = b.slice(0, 2); b.free; p [s.null?, s.valid?]`,
			"[false, false]\n",
		},
		{
			// io_flags_for_size on the NEW size: a page or more is mapped.
			"resizing_a_null_buffer_reinitializes_it",
			`b = IO::Buffer.new(0); b.resize(IO::Buffer::PAGE_SIZE)
			 p [b.size, b.internal?, b.mapped?, b.null?, b.empty?]`,
			"[16384, false, true, false, false]\n",
		},
		{
			// …and shrinking a mapped buffer below a page makes it internal.
			"resizing_a_mapped_buffer_recomputes_its_kind",
			`b = IO::Buffer.new(IO::Buffer::PAGE_SIZE, IO::Buffer::MAPPED); b.resize(4)
			 p [b.size, b.internal?, b.mapped?]`,
			"[4, true, false]\n",
		},
		{
			// An internal buffer is realloc'd in place and keeps its kind, even
			// when it grows past a page.
			"resizing_an_internal_buffer_keeps_its_kind",
			`b = IO::Buffer.new(4); b.resize(IO::Buffer::PAGE_SIZE)
			 p [b.size, b.internal?, b.mapped?]`,
			"[16384, true, false]\n",
		},
		{
			// The NULL-base test comes BEFORE the external one, so a freed
			// String-backed buffer can be resized into a fresh internal one.
			"resizing_a_freed_external_buffer_is_allowed",
			`b = IO::Buffer.for("test"); b.free; b.resize(10)
			 p [b.size, b.internal?, b.external?]`,
			"[10, true, false]\n",
		},
		{
			// io_flags_for_size(0) is INTERNAL, but io_buffer_initialize takes no
			// memory for a zero size, so the buffer stays null.
			"resizing_a_null_buffer_to_zero_keeps_it_null",
			`b = IO::Buffer.new(0); b.resize(0); p [b.null?, b.empty?, b.size, b.internal?]`,
			"[true, true, 0, false]\n",
		},
		{
			"resizing_a_freed_buffer_to_zero_keeps_it_null",
			`b = IO::Buffer.new(4); b.free; b.resize(0); p [b.null?, b.empty?, b.size]`,
			"[true, true, 0]\n",
		},
		{
			"resizing_a_live_external_buffer_is_refused",
			`b = IO::Buffer.for("test")
			 begin; b.resize(10); rescue IO::Buffer::AccessError => e; p e.message; end`,
			"\"Cannot resize external buffer!\"\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.body+"\n"); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestIOBufferStringAndFree pins rb_io_buffer_type_string and
// rb_io_buffer_free. The string form builds the String FIRST —
// `rb_str_new(NULL, RB_NUM2LONG(length))` — and the buffer only aliases it, so
// both of the size errors precede the block lookup and a #free inside the block
// cannot lose what was already written. #free itself refuses a locked buffer.
func TestIOBufferStringAndFree(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{
			"string_survives_a_free_inside_the_block",
			`p(IO::Buffer.string(4) { |b| b.set_string("meat"); b.free })`,
			"\"meat\"\n",
		},
		{
			// rb_str_new's own refusal, reached before the block is looked for.
			"negative_size",
			`begin; IO::Buffer.string(-1) {}; rescue ArgumentError => e; p e.message; end`,
			"\"negative string size (or size too big)\"\n",
		},
		{
			// RB_NUM2LONG's refusal, likewise before the block.
			"bignum_size",
			`begin; IO::Buffer.string(2 ** 232) {}; rescue RangeError => e; p e.message; end`,
			"\"bignum too big to convert into 'long'\"\n",
		},
		{
			// The block is reached through rb_ensure, so the message is the bare
			// "no block given" rather than rb_yield's "no block given (yield)".
			"no_block",
			`begin; IO::Buffer.string(7); rescue LocalJumpError => e; p e.message; end`,
			"\"no block given\"\n",
		},
		{
			"free_is_refused_while_locked",
			`b = IO::Buffer.new(4)
			 begin; b.locked { b.free }; rescue IO::Buffer::LockedError => e; p e.message; end`,
			"\"Buffer is locked!\"\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.body+"\n"); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestFileExpandPathEncoding pins the encoding of File.expand_path's answer.
// rb_file_expand_path_internal starts from `enc = rb_enc_get(fname)`; a relative
// path grows a directory in front of it and append_fspath then runs
// rb_enc_check(fname, dirname), where the directory carries the filesystem
// encoding. That one call both refuses an incompatible pair and names the
// result's encoding.
//
// Every expectation was taken from MRI ruby 4.0.5 running the same source.
func TestFileExpandPathEncoding(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{
			// The ARGUMENT's encoding wins over the default: both are ASCII-only, so
			// rb_enc_compatible answers the first one's.
			"argument_encoding_survives",
			`Encoding.default_external = Encoding::SHIFT_JIS
			 p File.expand_path("./a".dup.force_encoding(Encoding::CP1251)).encoding.name`,
			"\"Windows-1251\"\n",
		},
		{
			// Raw bytes are ASCII-8BIT, and ASCII-8BIT combined with an ASCII-only
			// directory is ASCII-8BIT.
			"binary_path_stays_binary",
			`Encoding.default_external = Encoding::SHIFT_JIS
			 p File.expand_path([222, 173, 190, 175].pack("C*")).encoding.name`,
			"\"ASCII-8BIT\"\n",
		},
		{
			// An ASCII-INCOMPATIBLE filesystem encoding cannot be spliced in front of
			// the path at all, so rb_enc_check raises.
			"incompatible_default_external_raises",
			`Encoding.default_external = Encoding::UTF_16BE
			 begin
			   File.expand_path("./a")
			   p :no_raise
			 rescue Encoding::CompatibilityError
			   p :compat_error
			 end`,
			":compat_error\n",
		},
		{
			// An ABSOLUTE path never grows a directory, so the filesystem encoding is
			// never consulted and the argument's encoding is simply kept.
			"absolute_path_never_consults_the_default",
			`Encoding.default_external = Encoding::UTF_16BE
			 p File.expand_path("/a/b").encoding.name`,
			"\"UTF-8\"\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := eval(t, c.body+"\n"); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestIONewlineWriteDecorator pins the write half of io.c's newline decorators,
// which the :newline option asks for (ECONV_CRLF_NEWLINE_DECORATOR /
// ECONV_CR_NEWLINE_DECORATOR, set by rb_io_extract_modeenc and applied by the
// write converter). :lf and :universal have no write-side effect — universal is
// a READ decorator.
//
// #syswrite and #write_nonblock go straight to the descriptor, so they are NOT
// decorated: that is the same boundary rb_io_write_memory sits on.
func TestIONewlineWriteDecorator(t *testing.T) {
	dir := ioScratchDir(t)
	q := func(name string) string {
		return `"` + strings.ReplaceAll(filepath.Join(dir, name), `\`, `\\`) + `"`
	}
	for _, c := range []struct{ name, opts, want string }{
		{"crlf", `, newline: :crlf`, "\"a\\r\\nb\\r\\n\\r\\nc\\r\\n\"\n"},
		{"cr", `, newline: :cr`, "\"a\\rb\\r\\rc\\r\"\n"},
		{"lf_is_a_no_op", `, newline: :lf`, "\"a\\nb\\n\\nc\\n\"\n"},
		{"universal_is_a_no_op_on_write", `, newline: :universal`, "\"a\\nb\\n\\nc\\n\"\n"},
		{"no_option", ``, "\"a\\nb\\n\\nc\\n\"\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := "A = " + q(c.name+".txt") + "\n" +
				`File.open(A, "wt"` + c.opts + `) { |f| f.write("a\nb\n"); f.puts; f.print "c\n" }` + "\n" +
				"p File.binread(A)\n"
			if got := eval(t, src); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
	t.Run("syswrite_is_not_decorated", func(t *testing.T) {
		src := "A = " + q("sysnl.txt") + "\n" +
			`File.open(A, "wt", newline: :crlf) { |f| f.syswrite("a\nb\n") }` + "\n" +
			"p File.binread(A)\n"
		if got, want := eval(t, src), "\"a\\nb\\n\"\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

// TestFileRealpathRelativeUsesTheCwd covers fileExpand's working-directory
// branch, which File.expand_path no longer reaches: fileExpandValue supplies its
// own directory String because it needs that directory's ENCODING as well as its
// name (see fileExpandValue). File.realpath still goes through fileExpand, and a
// relative path there is resolved against the working directory — so the
// relative and absolute spellings must name the same file.
func TestFileRealpathRelativeUsesTheCwd(t *testing.T) {
	// The Go test runs with the package directory as its working directory, so
	// this file is reachable by its bare name.
	const src = `rel = File.realpath("wave32_ioenc_test.go")
p [File.absolute_path?(rel), File.basename(rel), rel == File.realpath(File.expand_path("wave32_ioenc_test.go"))]
`
	want := "[true, \"wave32_ioenc_test.go\", true]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
