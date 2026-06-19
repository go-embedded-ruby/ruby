package vm_test

import "testing"

// Quoted symbols :"…" (plain and interpolated) and the symbol-inspect rules
// that decide bare `:name` vs quoted `:"…"`.
func TestQuotedSymbols(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"with_space", `p :"foo bar"`, ":\"foo bar\"\n"},
		{"simple_collapses_bare", `p :"simple"`, ":simple\n"},
		{"interp", "n = 2\np :\"a#{n}b\"", ":a2b\n"},
		{"interp_only", "n = 2\np :\"#{n}\"", ":\"2\"\n"},
		{"empty", `p :""`, ":\"\"\n"},
		{"interp_with_spaces", "n = 2\np :\"x #{n + 1}\"", ":\"x 3\"\n"},
		{"value_equals_plain", "n = 2\np(:\"a#{n}\" == :a2)", "true\n"},
		{"class", "n = 1\np :\"a#{n}\".class", "Symbol\n"},
		{"to_s", `p :"foo".to_s`, "\"foo\"\n"},
		{"in_array", `p [:"x y", :"z"]`, "[:\"x y\", :z]\n"},
		{"hashrocket_key", `p({:"key one" => 1})`, "{\"key one\": 1}\n"},
		{"escape", `p :"tab\there"`, ":\"tab\\there\"\n"},
		{"nested_brace_interp", `p :"v#{ {1 => 2}.size }"`, ":v1\n"},
		// Symbol#inspect: bare for identifiers/operators/ivars, quoted otherwise.
		{"setter_symbol", `p :name=`, ":name=\n"},
		{"predicate_symbol", `p :empty?`, ":empty?\n"},
		{"operator_symbol_bare", `p :<<`, ":<<\n"},
		{"ivar_symbol_bare", `p :@x`, ":@x\n"},
		{"cvar_symbol_bare", `p :@@x`, ":@@x\n"},
		{"gvar_symbol_bare", `p :$x`, ":$x\n"},
		{"const_symbol_bare", `p :Foo`, ":Foo\n"},
		{"digit_start_quoted", `p :"2x"`, ":\"2x\"\n"},
		{"at_only_quoted", `p :"@"`, ":\"@\"\n"},
		{"question_mid_quoted", `p :"a?b"`, ":\"a?b\"\n"},
		// A bare symbol hash key still uses the plain label form.
		{"plain_hash_label", `p({a: 1})`, "{a: 1}\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestQuotedSymbolUnterminated(t *testing.T) {
	if err := runErr(t, `x = :"unterminated`); err == nil {
		t.Error("expected error for unterminated quoted symbol")
	}
	// A trailing backslash with no following byte is also unterminated.
	if err := runErr(t, "x = :\"a\\"); err == nil {
		t.Error("expected error for unterminated quoted symbol ending in backslash")
	}
}
