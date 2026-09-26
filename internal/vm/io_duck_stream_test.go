package vm_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-embedded-ruby/ruby/internal/vm"
	"github.com/go-ruby-parser/parser"
)

// evalDuck runs src on a VM whose program output and diagnostic stream are two
// DISTINCT buffers and returns both, plus any runtime error. Both are needed:
// the defect these tests witness did not lose the message, it sent it to the
// RAW DESCRIPTOR instead of to the Ruby object $stderr held, so a test that
// checked only "did the text appear somewhere" passed either way.
//
// The script is named so a warning's "path:lineno: " prefix matches what MRI
// prints for the same file.
func evalDuck(t *testing.T, src string) (out, errOut string, runErr error) {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	iseq, err := compiler.Compile(prog)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	iseq.Name = "duck.rb"
	var o, e bytes.Buffer
	machine := vm.NewWithStderr(&o, &e)
	machine.SetScriptName("duck.rb")
	_, runErr = machine.Run(iseq)
	return o.String(), e.String(), runErr
}

// duckClass defines, on ONE line, a stream double that answers ONLY #write.
//
// That is the whole point. MRI's $stdout/$stderr setters (io.c stdout_setter
// :9215, stderr_setter :9228) admit any object answering #write, and rb_io_write
// (io.c:2296) is an unconditional rb_funcallv(io, id_write, …). rbgo used to
// type-assert the global to *IOObj and fall back to the raw descriptor for
// anything else.
//
// A StringIO CANNOT witness that, because StringIO IS an *IOObj in rbgo: it
// satisfied the old type assertion and collected the output either way. The
// matchers that were supposed to catch this (mspec's `complain`/`output`) used a
// StringIO, which is why the defect survived. Every WITNESS test below therefore
// uses this class, never a StringIO.
//
// It keeps the bytes in @b and, separately, the ARGUMENT LISTS it was handed in
// @log, because the piece count rb_io_writev passes is observable and was also
// wrong.
const duckClass = `class Duck; def initialize; @b = +""; @log = []; end; def write(*a); @log << a.map(&:to_s); a.each { |x| @b << x.to_s }; a.sum { |x| x.to_s.bytesize }; end; def collected; @b; end; def log; @log; end; end` + "\n"

// TestDuckTypedStreamsCollectInternalOutput is the witness for the wave-40
// defect: internal warnings (error.c rb_warn / rb_warning, reached through
// rb_write_warning_str -> Warning.warn -> $stderr.write) and Kernel#p went to
// the raw descriptor whenever $stderr / $stdout held anything that was not an
// *IOObj, and MRI sends #write to whatever the global holds.
//
// Each name says WITNESS or GUARD, measured by running this exact file against
// the parent commit rather than assumed: eight cases fail there, two pass. The
// two GUARDs are the shapes rb_io_puts already got right (a line that already
// ends in a newline, and an empty line), and they are here because the fix
// rewrote the piece-splitting that produces all three shapes — a change that
// could easily have made "always two pieces" out of a rule that is not.
//
// Every case asserts BOTH streams. That is not decoration: the old code did not
// lose the text, it wrote it to the raw descriptor, so an assertion on one buffer
// alone passes either way. One case here originally did exactly that and passed
// on the parent commit for the wrong reason (see "Kernel#p reaches a write-only
// $stdout"); it now routes its verdict through the other stream.
//
// Expected values are byte-for-byte from MRI 4.0.5 (ruby 4.0.5 +PRISM
// arm64-darwin25) running the same script.
func TestDuckTypedStreamsCollectInternalOutput(t *testing.T) {
	cases := []struct {
		name             string
		src              string
		wantOut, wantErr string
	}{
		// error.c rb_warn via rbWarn1, reached from Array#fetch's
		// `if (block_given && argc == 2) rb_warn("block supersedes default value
		// argument")`. This is the case the wave was opened on.
		{
			name: "WITNESS rb_warn reaches a write-only $stderr",
			src: duckClass + `$VERBOSE = true
s = Duck.new
$stderr = s
[1, 2].fetch(5, 1) { |i| i }
$stderr = STDERR
print s.collected`,
			wantOut: "duck.rb:5: warning: block supersedes default value argument\n",
			wantErr: "",
		},
		// error.c rb_warning via rbWarning1 — the quieter sibling, which needs
		// $VERBOSE == true rather than merely non-nil. Reached from
		// rb_ary_initialize, which uses rb_warning rather than rb_warn for its
		// "given block not used".
		{
			name: "WITNESS rb_warning reaches a write-only $stderr",
			src: duckClass + `$VERBOSE = true
s = Duck.new
$stderr = s
Array.new { |i| i }
$stderr = STDERR
print s.collected`,
			wantOut: "duck.rb:5: warning: given block not used\n",
			wantErr: "",
		},
		// io.c rb_f_p -> rb_p_write, which writes to $stdout, not $stderr: the same
		// type assertion lived on that side too.
		//
		// The verdict goes to the DIAGNOSTIC stream, not to stdout. Writing it to
		// stdout made this case pass on the parent commit for the wrong reason: the
		// leak landed in the very buffer the assertion read, so "{a: 1}\n" appeared
		// there whether the duck had collected it or not. Splitting the two means an
		// empty duck now shows up as text in the wrong stream.
		{
			name: "WITNESS Kernel#p reaches a write-only $stdout",
			src: duckClass + `s = Duck.new
$stdout = s
p({a: 1})
$stdout = STDOUT
$stderr.write s.collected`,
			wantOut: "",
			wantErr: "{a: 1}\n",
		},
		// rb_p_write hands the inspected form and rb_default_rs to rb_io_writev as
		// TWO pieces (io.c:9020-9022). rbgo concatenated them into one argument.
		{
			name: "WITNESS Kernel#p writes value and separator as two pieces",
			src: duckClass + `s = Duck.new
$stdout = s
p 7
$stdout = STDOUT
p s.log`,
			wantOut: `[["7", "\n"]]` + "\n",
			wantErr: "",
		},
		// rb_io_puts builds args[] the same way (io.c:8979-8990): line, then
		// rb_default_rs, in one rb_io_writev call.
		{
			name: "WITNESS Kernel#puts writes line and separator as two pieces",
			src: duckClass + `s = Duck.new
$stdout = s
puts "hi"
$stdout = STDOUT
p s.log`,
			wantOut: `[["hi", "\n"]]` + "\n",
			wantErr: "",
		},
		// …and appends nothing when the line already ends in a newline (n stays 1),
		// so the piece count is not simply "always two".
		{
			name: "GUARD Kernel#puts writes one piece when the line already ends in a newline",
			src: duckClass + `s = Duck.new
$stdout = s
puts "hi\n"
$stdout = STDOUT
p s.log`,
			wantOut: `[["hi\n"]]` + "\n",
			wantErr: "",
		},
		// An EMPTY line writes the separator ALONE (`args[n++] = rb_default_rs`),
		// the third shape rb_io_puts builds.
		{
			name: "GUARD Kernel#puts writes the separator alone for an empty line",
			src: duckClass + `s = Duck.new
$stdout = s
puts ""
$stdout = STDOUT
p s.log`,
			wantOut: `[["\n"]]` + "\n",
			wantErr: "",
		},
		// rb_io_writev splits on the receiver's #write ARITY, not on the piece
		// count: a strictly one-argument #write gets one call per piece
		// (io.c:2304-2318).
		{
			name: "WITNESS an arity-one write receives one call per piece",
			src: `class One; def initialize; @log = []; end; def write(x); @log << [x.to_s]; 1; end; def log; @log; end; end
s = One.new
$stdout = s
puts "hi"
$stdout = STDOUT
p s.log`,
			wantOut: `[["hi"], ["\n"]]` + "\n",
			wantErr: "",
		},
		// error.c rb_write_warning_str is rb_warning_warn(rb_mWarning, str), a
		// DYNAMIC send, so an overridden Warning.warn intercepts the interpreter's
		// OWN warnings — not only the ones Kernel#warn raised. rbgo wrote past
		// Warning.warn entirely, so the override never ran and the text still
		// reached the descriptor.
		{
			name: "WITNESS an overridden Warning.warn intercepts an internal rb_warn",
			src: `seen = []
mod = Module.new do
  define_method(:warn) { |msg, category: nil, **kw| seen << msg; nil }
end
Warning.extend mod
$VERBOSE = true
[1, 2].fetch(5, 1) { |i| i }
p seen`,
			wantOut: `["duck.rb:7: warning: block supersedes default value argument\n"]` + "\n",
			wantErr: "",
		},
		// rb_io_writev's deprecation notice (io.c:2306-2315), whose three gates are
		// the receiver not being $stderr, $VERBOSE == true, and the :deprecated
		// category enabled — which is off by default, so this asserts the gates as
		// much as the message.
		{
			name: "WITNESS an arity-one write draws the outdated-interface notice",
			src: `class One; def initialize; @b = +""; end; def write(x); @b << x.to_s; 1; end; def collected; @b; end; end
out = One.new
err = Duck2 = Class.new { def initialize; @b = +""; end; def write(*a); a.each { |x| @b << x.to_s }; 1; end; def collected; @b; end }.new
$VERBOSE = true
Warning[:deprecated] = true
$stdout = out
$stderr = err
puts "hi"
$stdout = STDOUT
$stderr = STDERR
print err.collected`,
			wantOut: "duck.rb:8: warning: One#write is outdated interface which accepts just one argument\n",
			wantErr: "",
		},
		// …and the same setup with :deprecated left at its DEFAULT draws no notice at
		// all. rb_category_warning checks rb_warning_category_enabled_p, and
		// :deprecated is off by default in Ruby 3+, which is why the line above is
		// almost never seen in practice. This case is what makes that gate a
		// measured claim rather than a comment.
		{
			name: "GUARD the outdated-interface notice stays silent with :deprecated off",
			src: duckClass + `err = Duck.new
out = Object.new
def out.write(x); 1; end
$VERBOSE = true
$stdout = out
$stderr = err
puts "hi"
$stdout = STDOUT
$stderr = STDERR
print err.collected.inspect
print " "
print Warning[:deprecated].inspect`,
			wantOut: `"" false`,
			wantErr: "",
		},
		// The same notice when #write lives on the object's SINGLETON: MRI switches
		// the separator to '.' and names the object rather than its class
		// (`RCLASS_SINGLETON_P(klass) ? (klass = io, '.') : '#'`). The name is
		// rendered with rb_inspect -- the format's '+' flag -- which is why the
		// object carries an ivar and an overridden #to_s here: MRI shows
		// `#<Object:0x… @marker=42>` and ignores the #to_s, and the first version of
		// warnOutdatedWrite used #to_s and printed "TO_S_CALLED" instead.
		//
		// Only the prefix and suffix are asserted, because the middle is a heap
		// address; pinning it would be pinning the allocator.
		{
			name: "WITNESS a singleton write is named with '.' and its inspect form",
			src: duckClass + `err = Duck.new
out = Object.new
out.instance_variable_set(:@marker, 42)
def out.write(x); 1; end
def out.to_s; "TO_S_CALLED"; end
$VERBOSE = true
Warning[:deprecated] = true
$stdout = out
$stderr = err
puts "hi"
$stdout = STDOUT
$stderr = STDERR
c = err.collected
print c.start_with?("duck.rb:11: warning: #<Object:0x")
print " "
print c.end_with?(" @marker=42>.write is outdated interface which accepts just one argument\n")
print " "
print c.include?("TO_S_CALLED")`,
			wantOut: "true true false",
			wantErr: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, errOut, err := evalDuck(t, c.src)
			if err != nil {
				t.Fatalf("runtime error: %v", err)
			}
			if out != c.wantOut {
				t.Errorf("collected  = %q, want %q", out, c.wantOut)
			}
			// The second half of the witness: on the parent commit the message was
			// HERE instead, so a test asserting only `out` would pass both ways.
			if errOut != c.wantErr {
				t.Errorf("leaked to the raw descriptor = %q, want %q", errOut, c.wantErr)
			}
		})
	}
}

// TestDuckTypedStreamFailureDirections pins the two directions MRI defines for a
// stream that cannot do the job. Both matched on the parent commit for the
// explicit paths ($stderr.write) and neither did for the internal ones, so the
// rb_warn rows are WITNESSES and the $stderr.write rows are GUARDS that the
// dispatch change did not weaken them.
func TestDuckTypedStreamFailureDirections(t *testing.T) {
	cases := []struct {
		name, src, wantErrMsg, kind string
	}{
		// io.c must_respond_to (:9193), called by stderr_setter before the
		// assignment takes effect. Note the class: MRI raises TypeError HERE, at the
		// assignment, and never the NoMethodError a later #write would give.
		{
			kind: "GUARD",
			name: "an object answering neither #write nor #puts cannot be assigned",
			src: `class Mute; end
$stderr = Mute.new`,
			wantErrMsg: "$stderr must have write method, Mute given",
		},
		{
			kind: "GUARD",
			name: "the same refusal for $stdout",
			src: `class Mute; end
$stdout = Mute.new`,
			wantErrMsg: "$stdout must have write method, Mute given",
		},
		// A #write that raises must propagate: rb_io_write does not rescue, so the
		// exception escapes the warning site. On the parent commit the warning never
		// reached this #write at all, so nothing was raised — this is a WITNESS.
		{
			kind: "WITNESS",
			name: "a raising #write propagates out of an internal rb_warn",
			src: `class Boom; def write(*a); raise RuntimeError, "boom from write"; end; end
$VERBOSE = true
$stderr = Boom.new
[1, 2].fetch(5, 1) { |i| i }`,
			wantErrMsg: "boom from write",
		},
		{
			kind: "WITNESS",
			name: "a raising #write propagates out of Kernel#p",
			src: `class Boom; def write(*a); raise RuntimeError, "boom from write"; end; end
$stdout = Boom.new
p 1`,
			wantErrMsg: "boom from write",
		},
		{
			kind: "GUARD",
			name: "a raising #write propagates out of an explicit $stderr.write",
			src: `class Boom; def write(*a); raise RuntimeError, "boom from write"; end; end
$stderr = Boom.new
$stderr.write "x"`,
			wantErrMsg: "boom from write",
		},
	}
	for _, c := range cases {
		t.Run(c.kind+" "+c.name, func(t *testing.T) {
			_, _, err := evalDuck(t, c.src)
			if err == nil {
				t.Fatalf("expected an error containing %q, got none", c.wantErrMsg)
			}
			if !strings.Contains(err.Error(), c.wantErrMsg) {
				t.Errorf("error = %v, want one containing %q", err, c.wantErrMsg)
			}
		})
	}
}

// TestDuckTypedStreamsKeepWorkingForRealStreams is a GUARD, not a witness: it
// passes on the parent commit too. It exists because the change moved every write
// off the *IOObj fast path and onto a #write dispatch, and the thing most likely
// to break is the case that used to be the only one handled — the untouched
// standard streams, and a $stderr bound to a real IO.
func TestDuckTypedStreamsKeepWorkingForRealStreams(t *testing.T) {
	cases := []struct {
		name             string
		src              string
		wantOut, wantErr string
	}{
		{
			name:    "the original $stderr still receives an internal warning",
			src:     `$VERBOSE = true` + "\n" + `[1, 2].fetch(5, 1) { |i| i }` + "\n" + `print "data"`,
			wantOut: "data",
			wantErr: "duck.rb:2: warning: block supersedes default value argument\n",
		},
		{
			name:    "p and puts still reach the original $stdout",
			src:     "p 1\nputs \"two\"\nprint \"three\"",
			wantOut: "1\ntwo\nthree",
			wantErr: "",
		},
		{
			name: "a StringIO $stderr still collects a warning",
			src: `require "stringio"
$VERBOSE = true
s = StringIO.new
$stderr = s
[1, 2].fetch(5, 1) { |i| i }
$stderr = STDERR
print s.string`,
			wantOut: "duck.rb:5: warning: block supersedes default value argument\n",
			wantErr: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, errOut, err := evalDuck(t, c.src)
			if err != nil {
				t.Fatalf("runtime error: %v", err)
			}
			if out != c.wantOut {
				t.Errorf("stdout = %q, want %q", out, c.wantOut)
			}
			if errOut != c.wantErr {
				t.Errorf("stderr = %q, want %q", errOut, c.wantErr)
			}
		})
	}
}
