package vm_test

import "testing"

// NilClass conversions and boolean operators mirror MRI 4.0.
func TestNilClassConversions(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"to_i", `p nil.to_i`, "0\n"},
		{"to_f", `p nil.to_f`, "0.0\n"},
		{"to_a", `p nil.to_a`, "[]\n"},
		{"to_h", `p nil.to_h`, "{}\n"},
		{"to_r", `p nil.to_r`, "(0/1)\n"},
		{"to_c", `p nil.to_c`, "(0+0i)\n"},
		{"to_s", `p nil.to_s`, "\"\"\n"},
		{"inspect", `p nil.inspect`, "\"nil\"\n"},
		{"nil?", `p nil.nil?`, "true\n"},
		{"and_true", `p(nil & true)`, "false\n"},
		{"and_false", `p(nil & false)`, "false\n"},
		{"and_obj", `p(nil & 7)`, "false\n"},
		{"or_true", `p(nil | true)`, "true\n"},
		{"or_false", `p(nil | false)`, "false\n"},
		{"or_obj", `p(nil | 7)`, "true\n"},
		{"or_nil", `p(nil | nil)`, "false\n"},
		{"xor_true", `p(nil ^ true)`, "true\n"},
		{"xor_false", `p(nil ^ false)`, "false\n"},
		{"xor_obj", `p(nil ^ 7)`, "true\n"},
		{"xor_nil", `p(nil ^ nil)`, "false\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("eval(%q) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// The boolean operators reject a wrong argument count (MRI raises ArgumentError).
func TestNilClassBooleanArity(t *testing.T) {
	for _, src := range []string{
		`nil.send(:&)`,
		`nil.send(:|)`,
		`nil.send(:^)`,
	} {
		class, _ := evalErr(t, src)
		if class != "ArgumentError" {
			t.Errorf("%q: expected ArgumentError, got %q", src, class)
		}
	}
}
