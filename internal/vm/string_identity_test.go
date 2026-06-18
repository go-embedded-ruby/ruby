package vm_test

import "testing"

// With String a reference type, each string literal evaluates to a distinct
// object (Ruby semantics): structural == is content-based, but equal? / object
// identity distinguishes separate instances.
func TestStringIdentity(t *testing.T) {
	tests := []struct{ name, src, want string }{
		// Content equality is unchanged.
		{"content_eq", `p("x" == "x")`, "true\n"},
		// Two literals are distinct objects.
		{"distinct_literals", `p("x".equal?("x"))`, "false\n"},
		// The same binding is identical to itself.
		{"same_binding", "s = \"x\"\np s.equal?(s)", "true\n"},
		// dup produces a distinct, content-equal object.
		{"dup_distinct", "s = \"x\"\np s.dup.equal?(s)", "false\n"},
		{"dup_eq", "s = \"x\"\np(s.dup == s)", "true\n"},
		// A string used as a hash key is matched by content, not identity.
		{"hash_key_by_content", `h = {"k" => 1}; p h["k"]`, "1\n"},
		{"hash_literal_eq", `p({"a" => 1} == {"a" => 1})`, "true\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
