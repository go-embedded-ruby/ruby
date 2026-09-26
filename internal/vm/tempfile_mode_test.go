package vm_test

import (
	"runtime"
	"testing"
)

// TestTempfileModeIsAnIntegerFlagSet covers #670. Tempfile#initialize passed its
// mode: argument straight to File.open, so `mode: File::RDONLY` — flag 0 — opened
// read-only on a path that does not exist yet and raised Errno::ENOENT. That call
// is the `before :each` of core/io/copy_stream_spec.rb's "to a Tempfile" block,
// so eight examples never ran.
//
// lib/tempfile.rb v3_4_0 Tempfile#initialize defaults mode: to 0 and ORs it:
//
//	@mode = mode|File::RDWR|File::CREAT|File::EXCL
//
// Every expectation below was taken from MRI 4.0.5 running the same program.
func TestTempfileModeIsAnIntegerFlagSet(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// The reproducer from the issue: RDONLY still yields a file that exists and
		// round-trips a write. Before the fix this raised Errno::ENOENT.
		{
			name: "rdonly_still_creates_a_writable_file",
			src: `require "tempfile"
t = Tempfile.new("rbgo-mode", encoding: Encoding::BINARY, mode: File::RDONLY)
t.write("abc"); t.rewind
p [File.exist?(t.path), t.read, t.external_encoding.to_s]
t.close!`,
			want: "[true, \"abc\", \"ASCII-8BIT\"]\n",
		},
		// The default is now 0, not "w+", so it goes down the same integer path.
		{
			name: "default_mode_is_read_write",
			src: `require "tempfile"
t = Tempfile.new("rbgo-dflt")
t.write("xy"); t.rewind
p t.read
t.close!`,
			want: "\"xy\"\n",
		},
		// An integer mode is OR'd, so the flags it adds survive: APPEND means the
		// write lands after the existing bytes whatever the position.
		{
			name: "given_flags_are_kept_not_replaced",
			src: `require "tempfile"
t = Tempfile.new("rbgo-app", mode: File::APPEND)
t.write("one"); t.rewind; t.write("two"); t.rewind
p t.read
t.close!`,
			want: "\"onetwo\"\n",
		},
		// A String mode reaches Integer#| and raises, which is what
		// library/tempfile/create_spec.rb asserts for Tempfile.create(mode: "wb").
		{
			name: "string_mode_raises_nomethoderror",
			src: `require "tempfile"
begin
  Tempfile.create(mode: "wb")
rescue NoMethodError => e
  p e.message
end`,
			want: "\"undefined method '|' for an instance of String\"\n",
		},
		// max_try: is consumed by Dir::Tmpname.create in MRI and must not reach
		// File.open, where it would be an unknown option.
		{
			name: "max_try_is_consumed",
			src: `require "tempfile"
t = Tempfile.new("rbgo-mt", max_try: 3)
t.write("k"); t.rewind
p t.read
t.close!`,
			want: "\"k\"\n",
		},
		// anonymous: belongs to Tempfile.create (it picks create_anonymous), so it
		// is consumed there rather than forwarded to #initialize and on to the open.
		{
			name: "anonymous_is_consumed_by_create",
			src: `require "tempfile"
t = Tempfile.create("rbgo-anon", anonymous: false)
t.write("q"); t.rewind
p t.read
path = t.path; t.close; File.unlink(path)`,
			want: "\"q\"\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTempfilePermissionsAreOwnerOnly pins the perm: 0600 that MRI's Tempfile
// forces on the open (lib/tempfile.rb sets opts[:perm] = 0600 inside
// Dir::Tmpname.create's block, after the caller's options) — a temporary file is
// the caller's alone. Gated on the OS because Windows has no POSIX mode bits to
// assert.
func TestTempfilePermissionsAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on windows")
	}
	const src = `require "tempfile"
t = Tempfile.new("rbgo-perm")
p (File.stat(t.path).mode & 0777).to_s(8)
t.close!`
	if got := eval(t, src); got != "\"600\"\n" {
		t.Errorf("Tempfile permissions = %s, want \"600\"", got)
	}
}

// TestCopyStreamWorksThroughToIO covers the three examples that still failed once
// #670 let the "to a Tempfile" block run. copyStreamRead/copyStreamWrite took the
// String/#to_path branch for a Tempfile and wrote the file behind the handle's
// back. io.c v3_4_0 copy_stream_body runs rb_io_check_io FIRST on both arguments,
// so an object answering #to_io IS its IO and the copy goes through that handle:
// at its current position, leaving the position at the last write.
//
// A Tempfile answers both #to_io and #to_path, which is why the order decides.
func TestCopyStreamWorksThroughToIO(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// "leaves the destination IO position at the last write" — #pos stayed 0.
		{
			name: "position_follows_the_copy",
			src: `require "tempfile"
src = Tempfile.new("rbgo-cs-src"); src.write("Line one\nLine two\n"); src.flush
dst = Tempfile.new("rbgo-cs-dst", mode: File::RDONLY)
n = IO.copy_stream(src.path, dst)
p [n, dst.pos]
src.close!; dst.close!`,
			want: "[18, 18]\n",
		},
		// "copies the entire IO contents to the IO" — the handle's own buffer used
		// to overwrite the copied bytes on the next flush, leaving the file empty.
		{
			name: "copied_bytes_survive_a_later_flush",
			src: `require "tempfile"
src = Tempfile.new("rbgo-cs-src2"); src.write("payload"); src.flush
dst = Tempfile.new("rbgo-cs-dst2", mode: File::RDONLY)
IO.copy_stream(src.path, dst)
dst.flush
p File.read(dst.path)
src.close!; dst.close!`,
			want: "\"payload\"\n",
		},
		// "starts writing at the destination IO's current position" — the preceding
		// write was lost because the path branch truncated the file.
		{
			name: "starts_at_the_destination_position",
			src: `require "tempfile"
src = Tempfile.new("rbgo-cs-src3"); src.write("body"); src.flush
dst = Tempfile.new("rbgo-cs-dst3", mode: File::RDONLY)
dst.write("prelude ")
IO.copy_stream(src.path, dst)
dst.flush
p File.read(dst.path)
src.close!; dst.close!`,
			want: "\"prelude body\"\n",
		},
		// The SOURCE side takes the same rb_io_check_io step, and it is observable:
		// read through the handle the copy starts at its current position and leaves
		// it advanced, where the path branch would have re-read from byte 0.
		{
			name: "source_is_read_through_its_handle",
			src: `require "tempfile"
src = Tempfile.new("rbgo-cs-src6", mode: File::RDONLY)
src.write("HEADbody"); src.flush
src.rewind; src.read(4)
dst = Tempfile.new("rbgo-cs-dst6", mode: File::RDONLY)
n = IO.copy_stream(src, dst)
dst.flush
p [n, File.read(dst.path), src.pos]
src.close!; dst.close!`,
			want: "[4, \"body\", 8]\n",
		},
		// The conversion must stay inside the String/#to_path branch: an object that
		// answers #to_io but NOT #to_path has no fptr in MRI and goes to
		// copy_stream_fallback's duck-typed #write instead.
		{
			name: "to_io_without_to_path_uses_the_write_fallback",
			src: `require "stringio"
class Sink
  def initialize; @seen = ""; end
  attr_reader :seen
  def write(s); @seen << s; s.bytesize; end
  def to_io; $stdout; end
end
s = Sink.new
IO.copy_stream(StringIO.new("ducked"), s)
p s.seen`,
			want: "\"ducked\"\n",
		},
		// A plain path destination is still opened and truncated, as before: the new
		// check must not divert a String.
		{
			name: "path_destination_is_still_opened",
			src: `require "tempfile"
require "stringio"
holder = Tempfile.new("rbgo-cs-path"); path = holder.path; holder.close
File.write(path, "stale")
IO.copy_stream(StringIO.new("fresh"), path)
p File.read(path)
File.unlink(path)`,
			want: "\"fresh\"\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
