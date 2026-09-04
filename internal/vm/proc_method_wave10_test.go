package vm_test

import "testing"

// TestUnboundBindModuleOwner covers that an UnboundMethod whose owner is a
// Module (not a Class) may be bound to any object — MRI only enforces the
// kind_of? rule for methods owned by a Class. Exercises the isModule short
// circuit in checkBindable.
func TestUnboundBindModuleOwner(t *testing.T) {
	src := `
module M
  def greeting; "hi"; end
end
um = M.instance_method(:greeting)
p um.bind(Object.new).call
`
	if got := eval(t, src); got != "\"hi\"\n" {
		t.Errorf("module-owned bind = %q, want %q", got, "\"hi\"\n")
	}
}

// TestUnboundBindModuleOwnerBindCall covers the same relaxation on bind_call.
func TestUnboundBindModuleOwnerBindCall(t *testing.T) {
	src := `
module M
  def echo(x); x; end
end
p M.instance_method(:echo).bind_call(Object.new, 42)
`
	if got := eval(t, src); got != "42\n" {
		t.Errorf("module-owned bind_call = %q, want %q", got, "42\n")
	}
}

// TestProcToSBinaryEncoding covers that Proc#to_s (and its #inspect alias) is
// tagged ASCII-8BIT (BINARY), as MRI reports.
func TestProcToSBinaryEncoding(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"proc", `p proc { }.to_s.encoding`},
		{"lambda", `p ->() { }.to_s.encoding`},
		{"inspect", `p proc { }.inspect.encoding`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != "#<Encoding:BINARY (ASCII-8BIT)>\n" {
				t.Errorf("%s => %q, want BINARY encoding", tc.src, got)
			}
		})
	}
}

// TestProcPostSplatBindingLenient covers that a non-lambda proc with post-splat
// required parameters (|*a, b|, |a, *b, c|) is lenient: too few arguments fill
// the post parameters with nil and leave the splat empty rather than raising.
func TestProcPostSplatBindingLenient(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"star_a_b_none", `p (proc { |*a, b| [a, b] }).call`, "[[], nil]\n"},
		{"a_star_b_c_one", `p (proc { |a, *b, c| [a, b, c] }).call(1)`, "[1, [], nil]\n"},
		{"a_star_b_c_none", `p (proc { |a, *b, c| [a, b, c] }).call`, "[nil, [], nil]\n"},
		{"a_star_b_c_many", `p (proc { |a, *b, c| [a, b, c] }).call(1, 2, 3, 4)`, "[1, [2, 3], 4]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// missingSrc is a class that answers :ghost only through respond_to_missing? +
// method_missing, used by the Method-via-missing tests.
const missingSrc = `
class Ghosted
  def respond_to_missing?(name, priv) = name == :ghost
  def method_missing(name, *args)
    name == :ghost ? [:ghost, *args] : super
  end
end
`

// TestMethodViaRespondToMissing covers Object#method returning a callable Method
// for a name answered only via respond_to_missing?, whose body forwards to
// method_missing, plus that Method's name/owner/receiver reflection.
func TestMethodViaRespondToMissing(t *testing.T) {
	cases := []struct{ name, expr, want string }{
		{"call", `g.method(:ghost).call(1, 2)`, "[:ghost, 1, 2]\n"},
		{"name", `g.method(:ghost).name`, ":ghost\n"},
		{"owner", `g.method(:ghost).owner`, "Ghosted\n"},
		{"receiver", `g.method(:ghost).receiver.equal?(g)`, "true\n"},
		{"eq", `g.method(:ghost) == g.method(:ghost)`, "true\n"},
		{"eq_other_recv", `g.method(:ghost) == Ghosted.new.method(:ghost)`, "false\n"},
		{"hash_eq", `g.method(:ghost).hash == g.method(:ghost).hash`, "true\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := missingSrc + "g = Ghosted.new\np(" + tc.expr + ")\n"
			if got := eval(t, src); got != tc.want {
				t.Errorf("%s => %q, want %q", tc.expr, got, tc.want)
			}
		})
	}
}

// TestMethodViaRespondToMissingDynamic confirms the Method forwards to
// method_missing resolved at call time: redefining method_missing after the
// Method is built changes the result.
func TestMethodViaRespondToMissingDynamic(t *testing.T) {
	src := missingSrc + `
g = Ghosted.new
m = g.method(:ghost)
def g.method_missing(*)
  :changed
end
p m.call
`
	if got := eval(t, src); got != ":changed\n" {
		t.Errorf("dynamic method_missing => %q, want %q", got, ":changed\n")
	}
}

// TestMethodMissingUnknownStillRaises confirms a name that respond_to_missing?
// rejects still raises NameError from Object#method.
func TestMethodMissingUnknownStillRaises(t *testing.T) {
	src := missingSrc + `
g = Ghosted.new
begin
  g.method(:nope)
  puts "no-raise"
rescue NameError
  puts "raised"
end
`
	if got := eval(t, src); got != "raised\n" {
		t.Errorf("unknown method => %q, want %q", got, "raised\n")
	}
}

// TestUnboundBindClassOwnerStillChecked confirms a Class-owned UnboundMethod
// still rejects an incompatible receiver (the raise path is preserved).
func TestUnboundBindClassOwnerStillChecked(t *testing.T) {
	src := `
class A; def foo; end; end
begin
  A.instance_method(:foo).bind(Object.new)
  puts "no-raise"
rescue TypeError
  puts "raised"
end
`
	if got := eval(t, src); got != "raised\n" {
		t.Errorf("class-owned bind of wrong receiver = %q, want %q", got, "raised\n")
	}
}
