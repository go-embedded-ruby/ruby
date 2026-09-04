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
