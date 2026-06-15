package vm_test

import "testing"

// `name: value` label syntax in hash literals is sugar for `:name => value`.
func TestLabelHashLiterals(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"basic", `p({a: 1, b: 2})`, "{a: 1, b: 2}\n"},
		{"string_value", `p({name: "Bob"})`, "{name: \"Bob\"}\n"},
		{"index", "h = {x: 10, y: 20}\np h[:x]", "10\n"},
		{"equals_symbol_form", `p({a: 1} == {:a => 1})`, "true\n"},
		{"mixed_with_hashrocket", `p({a: 1, "b" => 2})`, "{a: 1, \"b\" => 2}\n"},
		{"trailing_comma", `p({a: 1,})`, "{a: 1}\n"},
		{"multiline", "p({a: 1,\n   b: 2})", "{a: 1, b: 2}\n"},
		{"keys", `p({a: 1, b: 2}.keys)`, "[:a, :b]\n"},
		{"enumerable", `p({a: 1, b: 2}.select { |k, v| v > 1 })`, "{b: 2}\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
