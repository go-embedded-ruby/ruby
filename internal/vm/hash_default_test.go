package vm_test

import "testing"

// Hash#default / default= / default_proc / default_proc= mirror MRI, including
// the mutual exclusion between the static default and the default block.
func TestHashDefault(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"set_default", `h = {}; h.default = 5; p h.default`, "5\n"},
		{"missing_key_uses_default", `h = {}; h.default = 5; p h[:nope]`, "5\n"},
		{"new_then_set", `h = Hash.new(1); h.default = 2; p h[:x]`, "2\n"},
		{"default_from_new", `h = Hash.new(7); p h.default`, "7\n"},
		{"default_with_key_no_proc", `h = Hash.new(7); p h.default(:k)`, "7\n"},
		{"default_proc_nil_by_default", `p Hash.new(7).default_proc`, "nil\n"},
		{"default_proc_class", `h = Hash.new { |hash, k| k }; p h.default_proc.class`, "Proc\n"},
		{"default_nil_when_proc", `h = Hash.new { |hash, k| k }; p h.default`, "nil\n"},
		{"default_key_calls_proc", `h = Hash.new { |hash, k| k.to_s * 2 }; p h.default(:ab)`, "\"abab\"\n"},
		{"set_default_clears_proc", `h = Hash.new { |hash, k| 9 }; h.default = 5; p h.default_proc`, "nil\n"},
		{"set_default_after_proc", `h = Hash.new { |hash, k| 9 }; h.default = 5; p h.default`, "5\n"},
		{"set_proc", `h = {}; h.default_proc = ->(hash, k) { 0 }; p h.default_proc.class`, "Proc\n"},
		{"set_proc_clears_default", `h = Hash.new(7); h.default_proc = ->(hash, k) { 0 }; p h.default`, "nil\n"},
		{"set_proc_nil", `h = Hash.new { |hash, k| 9 }; h.default_proc = nil; p h.default_proc`, "nil\n"},
		{"plain_default", `p({}.default)`, "nil\n"},
		{"plain_default_proc", `p({}.default_proc)`, "nil\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("eval(%q) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

func TestHashDefaultProcTypeError(t *testing.T) {
	class, _ := evalErr(t, `h = {}; h.default_proc = 5`)
	if class != "TypeError" {
		t.Errorf("expected TypeError, got %q", class)
	}
}
