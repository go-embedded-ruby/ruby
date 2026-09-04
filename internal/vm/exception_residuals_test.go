package vm_test

import (
	"strings"
	"testing"
)

// TestExceptionDetailedMessage exercises Exception#detailed_message across MRI's
// empty-message and anonymous-class special cases, in both plain and highlight
// forms.
func TestExceptionDetailedMessage(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"named_nonempty", `print RuntimeError.new("x").detailed_message`, "x (RuntimeError)"},
		{"runtimeerror_empty", `print RuntimeError.new("").detailed_message`, "unhandled exception"},
		{"other_empty", `print StandardError.new("").detailed_message`, "StandardError"},
		{"named_nonempty_highlight", `print RuntimeError.new("x").detailed_message(highlight: true)`,
			"\x1b[1mx (\x1b[1;4mRuntimeError\x1b[m\x1b[1m)\x1b[m"},
		{"runtimeerror_empty_highlight", `print RuntimeError.new("").detailed_message(highlight: true)`,
			"\x1b[1;4munhandled exception\x1b[m"},
		{"other_empty_highlight", `print StandardError.new("").detailed_message(highlight: true)`,
			"\x1b[1;4mStandardError\x1b[m"},
		{"anon_nonempty", `print Class.new(RuntimeError).new("m").detailed_message`, "m"},
		{"anon_nonempty_highlight", `print Class.new(RuntimeError).new("m").detailed_message(highlight: true)`,
			"\x1b[1mm"},
		{"ignores_other_kwargs", `print RuntimeError.new("x").detailed_message(foo: true)`, "x (RuntimeError)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestExceptionDetailedMessageAnonEmpty checks the anonymous-class empty-message
// path renders the class's "#<Class:0x…>" representation.
func TestExceptionDetailedMessageAnonEmpty(t *testing.T) {
	got := eval(t, `print Class.new(RuntimeError).new("").detailed_message`)
	if !strings.HasPrefix(got, "#<Class:0x") || !strings.HasSuffix(got, ">") {
		t.Errorf("anonymous empty detailed_message = %q, want #<Class:0x…>", got)
	}
	gotH := eval(t, `print Class.new(RuntimeError).new("").detailed_message(highlight: true)`)
	if !strings.HasPrefix(gotH, "\x1b[1;4m#<Class:0x") || !strings.HasSuffix(gotH, ">\x1b[m") {
		t.Errorf("anonymous empty highlight detailed_message = %q", gotH)
	}
}

// TestExceptionFullMessageDispatch checks that Exception#full_message dispatches
// to the (overridable) #detailed_message: it honours a singleton override,
// coerces a non-String return via #to_str, and falls back to the class name when
// the override yields nil, in both plain and highlight forms.
func TestExceptionFullMessageDispatch(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"default", `print RuntimeError.new("x").full_message.lines.first`, "x (RuntimeError)"},
		{"unhandled_empty", `print RuntimeError.new("").full_message`, "unhandled exception"},
		{"override_string", `e = RuntimeError.new("x")
e.define_singleton_method(:detailed_message) { |**opts| "OVERRIDDEN" }
print e.full_message.lines.first`, "OVERRIDDEN"},
		{"override_to_str", `msg = Object.new
def msg.to_str; "COERCED"; end
e = RuntimeError.new("x")
e.define_singleton_method(:detailed_message) { |**opts| msg }
print e.full_message.lines.first`, "COERCED"},
		{"override_nil", `e = RuntimeError.new("x")
e.define_singleton_method(:detailed_message) { |**opts| nil }
print e.full_message(highlight: false).lines.first`, "RuntimeError"},
		{"override_nil_highlight", `e = RuntimeError.new("x")
e.define_singleton_method(:detailed_message) { |**opts| nil }
print e.full_message(highlight: true).lines.first`, "\x1b[1;4mRuntimeError\x1b[m"},
		{"undef_fallback", `e = RuntimeError.new("x")
class << e
  undef :detailed_message
end
print e.full_message(highlight: false).lines.first`, "RuntimeError"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}
