// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestKernelCallerArguments covers Kernel#caller's argument handling — the shared
// callerSlice over caller(0): the default start, an explicit Integer start, a
// length cap (including a nil length), a Range, Float coercion, and the nil result
// when the start overshoots the stack (distinct from an empty array at the exact
// end). The Range/(start,length) results are checked for internal consistency
// against Array#[] on caller(0), which holds regardless of the frame count.
func TestKernelCallerArguments(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// caller(0) keeps the current frame; the default (== caller(1)) drops it.
		{"start_zero_has_frame", `p caller(0).length >= 1`, "true\n"},
		{"default_drops_one", `p caller == caller(1)`, "true\n"},
		{"start_one_equals_slice", `p caller(1) == caller(0)[1..]`, "true\n"},
		// Length cap: caller(0, 1) is one frame; caller(0, 0) is empty.
		{"length_cap_one", `p caller(0, 1).length`, "1\n"},
		{"length_cap_zero", `p caller(0, 0).length`, "0\n"},
		// A nil length behaves like no length at all.
		{"nil_length", `p caller(0, nil) == caller(0)`, "true\n"},
		// A Range slices caller(0) exactly like Array#[], nested so the stack is deep.
		{"range_matches_slice", `def a; [caller(0), caller(1..2)]; end
def b; a; end
f, r = b
p f[1..2] == r`, "true\n"},
		{"start_length_matches_slice", `def a; [caller(0), caller(1, 1)]; end
def b; a; end
f, l = b
p f[1, 1] == l`, "true\n"},
		// Float start/length truncate toward zero via #to_int.
		{"float_coercion", `p caller(0.9, 1.9) == caller(0, 1)`, "true\n"},
		// Overshooting the stack returns nil (not []); the exact end returns [].
		{"overshoot_nil", `p caller(1000)`, "nil\n"},
		{"overshoot_range_nil", `p caller(1000..-1)`, "nil\n"},
		// The frames are Strings.
		{"frames_are_strings", `p caller(0)[0].class`, "String\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Fatalf("src=%q: got %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestKernelCallerErrors covers the negative-argument guards in callerSlice, which
// raise ArgumentError with MRI's exact messages (a Range is exempt — its negative
// bounds are legal).
func TestKernelCallerErrors(t *testing.T) {
	tests := []struct{ name, src, class, msg string }{
		{"negative_start", `caller(-1)`, "ArgumentError", "negative level (-1)"},
		{"negative_length", `caller(0, -1)`, "ArgumentError", "negative size (-1)"},
		{"negative_start_loc", `caller_locations(-1)`, "ArgumentError", "negative level (-1)"},
		{"negative_length_loc", `caller_locations(0, -1)`, "ArgumentError", "negative size (-1)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			class, msg := evalErr(t, tc.src)
			if class != tc.class || msg != tc.msg {
				t.Fatalf("src=%q: got %s/%q, want %s/%q", tc.src, class, msg, tc.class, tc.msg)
			}
		})
	}
}

// TestKernelCallerLocations covers Kernel#caller_locations: each level is a
// Thread::Backtrace::Location answering #to_s / #path / #lineno, it shares
// #caller's argument handling (a Range and the nil-overshoot result), and it is a
// Kernel module function (private instance + public module method).
func TestKernelCallerLocations(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"is_location", `p caller_locations(0, 1)[0].kind_of?(Thread::Backtrace::Location)`, "true\n"},
		{"location_to_s", `p caller_locations(0, 1)[0].to_s.class`, "String\n"},
		{"location_path", `p caller_locations(0, 1)[0].path.class`, "String\n"},
		{"location_lineno", `p caller_locations(0, 1)[0].lineno.class`, "Integer\n"},
		{"range_form", `p caller_locations(0..0).length`, "1\n"},
		{"overshoot_nil", `p caller_locations(1000)`, "nil\n"},
		{"empty_at_end", `p caller_locations(caller_locations(0).length).length`, "0\n"},
		{"private_on_kernel", `p Kernel.private_instance_methods(false).include?(:caller_locations)`, "true\n"},
		{"public_on_kernel", `p Kernel.public_methods(false).include?(:caller_locations)`, "true\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Fatalf("src=%q: got %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestKernelRaiseExceptionProtocol covers raiseExceptionObject's MRI make_exception
// path: a lone String becomes a RuntimeError; an exception instance is returned by
// its own #exception (identity); an exception class is instantiated; a non-Exception
// object's #exception(msg) is honoured; a bare object is a TypeError; and an
// #exception that returns a non-Exception is the "exception object expected" TypeError.
func TestKernelRaiseExceptionProtocol(t *testing.T) {
	valueTests := []struct{ name, src, want string }{
		{"string_shorthand", `begin; raise "boom"; rescue => e; p [e.class, e.message]; end`, "[RuntimeError, \"boom\"]\n"},
		{"instance_identity", `x = RuntimeError.new("z")
begin; raise(x); rescue => e; p e.equal?(x); end`, "true\n"},
		{"class_instantiated", `begin; raise(ArgumentError, "m"); rescue => e; p [e.class, e.message]; end`, "[ArgumentError, \"m\"]\n"},
		{"exception_protocol_with_msg", `e = Object.new
def e.exception(m); StandardError.new(m); end
begin; raise e, "foo"; rescue => x; p [x.class, x.message]; end`, "[StandardError, \"foo\"]\n"},
		{"exception_protocol_no_msg", `e = Object.new
def e.exception; StandardError.new("z"); end
begin; raise e; rescue => x; p x.class; end`, "StandardError\n"},
	}
	for _, tc := range valueTests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Fatalf("src=%q: got %q, want %q", tc.src, got, tc.want)
			}
		})
	}

	errTests := []struct{ name, src, class, msg string }{
		{"bare_object", `raise Object.new`, "TypeError", "exception class/object expected"},
		{"true_value", `raise true`, "TypeError", "exception class/object expected"},
		{"exception_returns_nonexception", `e = Object.new
def e.exception; Array; end
raise e`, "TypeError", "exception object expected"},
	}
	for _, tc := range errTests {
		t.Run(tc.name, func(t *testing.T) {
			class, msg := evalErr(t, tc.src)
			if class != tc.class || msg != tc.msg {
				t.Fatalf("src=%q: got %s/%q, want %s/%q", tc.src, class, msg, tc.class, tc.msg)
			}
		})
	}
}

// TestKernelRaiseBareAndCause covers nativeRaise's edges: a bare raise with nothing
// being handled yields RuntimeError with an empty message; a cause: that loops back
// to the raised exception is rejected with "circular causes"; a cause: equal to the
// raised exception itself is NOT circular; and a non-Exception cause: is a TypeError.
func TestKernelRaiseBareAndCause(t *testing.T) {
	t.Run("bare_empty_message", func(t *testing.T) {
		class, msg := evalErr(t, `raise`)
		if class != "RuntimeError" || msg != "" {
			t.Fatalf("got %s/%q, want RuntimeError/\"\"", class, msg)
		}
	})
	t.Run("circular_cause", func(t *testing.T) {
		src := `begin
  raise "1"
rescue => e1
  begin
    raise "2"
  rescue => e2
    begin
      raise "3"
    rescue => e3
      raise(e1, cause: e3)
    end
  end
end`
		class, msg := evalErr(t, src)
		if class != "ArgumentError" || msg != "circular causes" {
			t.Fatalf("got %s/%q, want ArgumentError/\"circular causes\"", class, msg)
		}
	})
	t.Run("cause_equal_self_not_circular", func(t *testing.T) {
		src := `c = StandardError.new("c")
begin; raise(c, cause: c); rescue => e; p e.class; end`
		if got := eval(t, src); got != "StandardError\n" {
			t.Fatalf("got %q, want StandardError\\n", got)
		}
	})
	t.Run("bad_cause_type", func(t *testing.T) {
		class, msg := evalErr(t, `raise("m", cause: Object.new)`)
		if class != "TypeError" || msg != "exception object expected" {
			t.Fatalf("got %s/%q, want TypeError/\"exception object expected\"", class, msg)
		}
	})
	t.Run("explicit_cause_chained", func(t *testing.T) {
		src := `c = StandardError.new("x")
begin; raise("y", cause: c); rescue => e; p e.cause.message; end`
		if got := eval(t, src); got != "\"x\"\n" {
			t.Fatalf("got %q, want \"x\"\\n", got)
		}
	})
}
