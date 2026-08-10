package vm_test

import (
	"strings"
	"testing"
)

// TestExceptionProtocol covers the Exception instance/class protocol added for
// MRI 4.0 conformance: #exception, .exception, #inspect, #==, #cause (auto-set +
// cause: keyword), full_message/detailed_message keywords and the structured
// accessors of the specific exception classes. Each case prints a value that is
// compared verbatim against the equivalent ruby 4.0 output.
func TestExceptionProtocol(t *testing.T) {
	cases := []struct{ name, src, want string }{
		// #exception
		{"exception_self", `e = RuntimeError.new("x"); p e.exception.equal?(e)`, "true\n"},
		{"exception_dup", `e = RuntimeError.new("x"); e2 = e.exception("y"); p [e2.message, e.message, e2.equal?(e)]`, "[\"y\", \"x\", false]\n"},
		{"exception_coerce", `e = RuntimeError.new("x"); p e.exception(:sym).message`, "\"sym\"\n"},
		{"class_exception", `p RuntimeError.exception("z").message`, "\"z\"\n"},
		{"class_exception_inherit", `p ArgumentError.exception("a").class`, "ArgumentError\n"},
		// #inspect: normal, class-name message, empty, multiline
		{"inspect_msg", `p RuntimeError.new("boom").inspect`, "\"#<RuntimeError: boom>\"\n"},
		{"inspect_default", `p RuntimeError.new.inspect`, "\"#<RuntimeError: RuntimeError>\"\n"},
		{"inspect_empty", `p Exception.new("").inspect`, "\"Exception\"\n"},
		{"inspect_multiline", `p RuntimeError.new("a\nb").inspect`, "\"#<RuntimeError:\\\"a\\\\nb\\\">\"\n"},
		// #==
		{"eq_true", `p(RuntimeError.new("a") == RuntimeError.new("a"))`, "true\n"},
		{"eq_self", `e = RuntimeError.new("a"); p(e == e)`, "true\n"},
		{"eq_diff_msg", `p(RuntimeError.new("a") == RuntimeError.new("b"))`, "false\n"},
		{"eq_diff_class", `p(RuntimeError.new("a") == StandardError.new("a"))`, "false\n"},
		{"eq_diff_bt", `a = RuntimeError.new("a"); b = RuntimeError.new("a"); b.set_backtrace(["z"]); p(a == b)`, "false\n"},
		{"eq_non_exc", `p(RuntimeError.new("a") == 5)`, "false\n"},
		// #cause
		{"cause_nil", `p RuntimeError.new("x").cause`, "nil\n"},
		{"cause_auto", `begin; raise "i"; rescue; begin; raise "o"; rescue => e; p e.cause.message; end; end`, "\"i\"\n"},
		{"cause_kwarg", `c = ArgumentError.new("c"); begin; raise "o", cause: c; rescue => e; p e.cause.message; end`, "\"c\"\n"},
		{"cause_kwarg_nil", `begin; begin; raise "i"; rescue; raise "o", cause: nil; end; rescue => e; p e.cause; end`, "nil\n"},
		{"cause_chain", `begin; begin; begin; raise "a"; rescue; raise "b"; end; rescue; raise "c"; end; rescue => e; p [e.message, e.cause.message, e.cause.cause.message]; end`, "[\"c\", \"b\", \"a\"]\n"},
		// $! reverts across method frames: a fresh raise in a new frame after a
		// prior rescue has a nil cause (see the def/rescue then top-level raise).
		{"cause_reverts", `def swallow; begin; raise "x"; rescue; end; end; swallow; begin; raise "y"; rescue => e; p e.cause; end`, "nil\n"},
		// full_message / detailed_message
		{"detailed_plain", `begin; raise "boom"; rescue => e; p e.detailed_message; end`, "\"boom (RuntimeError)\"\n"},
		{"detailed_highlight", `begin; raise "boom"; rescue => e; puts e.detailed_message(highlight: true).bytes.length; end`, "39\n"},
		{"detailed_opts", `begin; raise "boom"; rescue => e; p e.detailed_message(foo: 1); end`, "\"boom (RuntimeError)\"\n"},
		{"full_no_bt", `p RuntimeError.new("x").full_message(highlight: false)`, "\"x (RuntimeError)\"\n"},
		{"full_highlight_default", `begin; raise "x"; rescue => e; p(e.full_message == e.full_message(highlight: false, order: :top)); end`, "true\n"},
		// backtrace_locations
		{"bt_loc_nil", `p RuntimeError.new("x").backtrace_locations`, "nil\n"},
		{"bt_loc_class", `begin; raise "x"; rescue => e; p e.backtrace_locations.first.class; end`, "Thread::Backtrace::Location\n"},
		{"bt_loc_str", `e = RuntimeError.new("x"); e.set_backtrace(["a:1:in 'foo'"]); l = e.backtrace_locations.first; p [l.path, l.lineno, l.label, l.to_s]`, "[\"a\", 1, \"foo\", \"a:1:in 'foo'\"]\n"},
		{"bt_loc_nolabel", `e = RuntimeError.new("x"); e.set_backtrace(["plain"]); l = e.backtrace_locations.first; p [l.path, l.lineno, l.label]`, "[\"plain\", 0, \"\"]\n"},
		{"bt_loc_badnum", `e = RuntimeError.new("x"); e.set_backtrace(["a:z:in 'm'"]); l = e.backtrace_locations.first; p [l.path, l.lineno, l.label]`, "[\"a:z\", 0, \"m\"]\n"},
		{"bt_loc_extra", `e = RuntimeError.new("x"); e.set_backtrace(["a:1:in 'm'"]); l = e.backtrace_locations.first; p [l.absolute_path, l.base_label, l.inspect]`, "[\"a\", \"m\", \"\\\"a:1:in 'm'\\\"\"]\n"},
		// message coercion / nil
		{"new_nil", `p RuntimeError.new(nil).message`, "\"RuntimeError\"\n"},
		{"new_sym", `p RuntimeError.new(:sym).message`, "\"sym\"\n"},
		// specific-class accessors
		{"nme_name", `begin; "s".no_such(1,2); rescue NoMethodError => e; p [e.name, e.args]; end`, "[:no_such, [1, 2]]\n"},
		{"nme_recv", `o = Object.new; begin; o.nope; rescue NoMethodError => e; p [e.name, e.receiver.equal?(o), e.args]; end`, "[:nope, true, []]\n"},
		{"key_error", `begin; {a: 1}.fetch(:x); rescue KeyError => e; p [e.key, e.receiver]; end`, "[:x, {a: 1}]\n"},
		{"key_error_fv", `begin; {a: 1}.fetch_values(:a, :x); rescue KeyError => e; p e.key; end`, ":x\n"},
		{"frozen_recv", `s = "x".freeze; begin; s << "y"; rescue FrozenError => e; p e.receiver.equal?(s); end`, "true\n"},
		{"frozen_recv_obj", `o = Object.new; o.freeze; begin; o.instance_variable_set(:@x, 1); rescue FrozenError => e; p e.receiver.equal?(o); end`, "true\n"},
		{"stopiter_result", `e = [1, 2].each; e.next; e.next; begin; e.next; rescue StopIteration => se; p se.result; end`, "[1, 2]\n"},
		{"stopiter_gen", `en = Enumerator.new { |y| y << 1; :DONE }; en.next; begin; en.next; rescue StopIteration => se; p se.result; end`, ":DONE\n"},
		{"lje_reason_default", `en = [1].each; begin; en.next; en.next; rescue StopIteration; end; p LocalJumpError.new.reason`, ":noreturn\n"},
		{"systemexit", `p [SystemExit.new(3).status, SystemExit.new.success?, SystemExit.new(1).success?]`, "[3, true, false]\n"},
		{"errno_const", `p [Errno::ENOENT::Errno, Errno::ENOENT.new("f").errno]`, "[2, 2]\n"},
		{"syscall_no_errno", `p SystemCallError.new("x").errno`, "nil\n"},
		// throw / catch
		{"catch_match", `p catch(:t) { throw :t, 9 }`, "9\n"},
		{"catch_nested", `p catch(:a) { catch(:b) { throw :a, 1 }; 99 }`, "1\n"},
		{"catch_default_tag", `p catch { |t| throw t, 7 }`, "7\n"},
		{"uncaught_rescuable", `begin; throw :zzz, 42; rescue UncaughtThrowError => e; p [e.tag, e.value, e.message]; end`, "[:zzz, 42, \"uncaught throw :zzz\"]\n"},
		{"uncaught_novalue", `begin; throw :q; rescue UncaughtThrowError => e; p [e.tag, e.value]; end`, "[:q, nil]\n"},
		// autoCause branches: re-raising the exception being handled adds no cause
		// (it would be its own cause); an exception already carrying a cause keeps it.
		{"cause_reraise_self", `begin; raise "x"; rescue => e; begin; raise e; rescue => r; p r.cause; end; end`, "nil\n"},
		{"cause_preset_kept", `c = ArgumentError.new("c"); e = RuntimeError.new("e"); begin; raise e, cause: c; rescue; end; begin; raise "z"; rescue; begin; raise e; rescue => r; p r.cause.message; end; end`, "\"c\"\n"},
		// parseFullMessageOpts branches: a non-hash positional argument and a
		// non-symbol order: value both fall back to the defaults (highlight off, top).
		{"full_nonhash_arg", `e = RuntimeError.new("m"); e.set_backtrace(["a:1:in 'f'"]); p e.full_message(:whatever)`, "\"a:1:in 'f': m (RuntimeError)\\n\"\n"},
		{"full_order_nonsym", `e = RuntimeError.new("m"); e.set_backtrace(["a:1:in 'f'"]); p e.full_message(order: "bottom")`, "\"a:1:in 'f': m (RuntimeError)\\n\"\n"},
		// popCauseKwarg branches: a trailing 1-key hash that is not :cause, and a
		// trailing multi-key hash, are both left as the raise's message argument.
		{"raise_hash_msg", `begin; raise StandardError, {foo: 1}; rescue => e; p e.message; end`, "\"{foo: 1}\"\n"},
		{"raise_hash_multi", `begin; raise StandardError, {a: 1, b: 2}; rescue => e; p e.message; end`, "\"{a: 1, b: 2}\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestExceptionFullMessageOrder covers full_message with an explicit backtrace so
// the top/bottom shaping, the numbered outer frames and the trailing newline are
// asserted deterministically (a live raise carries line 0 frames).
func TestExceptionFullMessageOrder(t *testing.T) {
	setup := `e = RuntimeError.new("boom"); e.set_backtrace(["a.rb:1:in 'f'", "b.rb:2:in 'g'", "c.rb:3:in 'main'"]); `
	top := eval(t, setup+`puts e.full_message(highlight: false, order: :top)`)
	wantTop := "a.rb:1:in 'f': boom (RuntimeError)\n\tfrom b.rb:2:in 'g'\n\tfrom c.rb:3:in 'main'\n"
	if top != wantTop {
		t.Errorf("top:\n got=%q\nwant=%q", top, wantTop)
	}
	bottom := eval(t, setup+`puts e.full_message(highlight: false, order: :bottom)`)
	wantBottom := "Traceback (most recent call last):\n\t2: from c.rb:3:in 'main'\n\t1: from b.rb:2:in 'g'\na.rb:1:in 'f': boom (RuntimeError)\n"
	if bottom != wantBottom {
		t.Errorf("bottom:\n got=%q\nwant=%q", bottom, wantBottom)
	}
	// highlight true wraps the message + class in ANSI on the raise-site line.
	hl := eval(t, setup+`puts e.full_message(highlight: true, order: :top)`)
	if !strings.Contains(hl, "\x1b[1mboom (\x1b[1;4mRuntimeError\x1b[m\x1b[1m)\x1b[m") {
		t.Errorf("highlight full_message missing ANSI: %q", hl)
	}
}

// TestExceptionRaiseErrors covers the error branches of Kernel#raise's cause:
// keyword, Exception#exception arity, and Kernel#throw arity.
func TestExceptionRaiseErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"cause_non_exc", `raise "o", cause: 5`, "TypeError"},
		{"cause_no_args", `raise(cause: ArgumentError.new("c"))`, "only cause is given with no arguments"},
		{"exception_arity", `RuntimeError.new("x").exception(1, 2)`, "wrong number of arguments"},
		{"throw_no_args", `throw`, "wrong number of arguments"},
		{"catch_no_block", `catch(:x)`, "no block given"},
		// A raw throwSignal that escapes to the Run boundary (Find.prune outside a
		// Find.find block) is turned into an UncaughtThrowError there; Kernel#throw
		// with no matching catch now raises it directly instead.
		{"prune_run_boundary", `require "find"; Find.prune`, "uncaught throw"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q: got err=%v, want containing %q", tc.src, err, tc.want)
			}
		})
	}
}

// TestExceptionLocalJumpReason covers LocalJumpError#reason/#exit_value for an
// unexpected return (a proc outliving its home method), where reason is :return
// and exit_value the returned value.
func TestExceptionLocalJumpReason(t *testing.T) {
	src := `
def make; Proc.new { return 5 }; end
begin
  make.call
rescue LocalJumpError => e
  p [e.reason, e.exit_value]
end
`
	if got := eval(t, src); got != "[:return, 5]\n" {
		t.Errorf("got=%q, want [:return, 5]", got)
	}
}
