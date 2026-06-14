package vm_test

import (
	"strings"
	"testing"
)

func TestHashes(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"literal_inspect", `p({ :a => 1, "b" => 2 })`, "{:a=>1, \"b\"=>2}\n"},
		{"empty_literal", `p({})`, "{}\n"},
		{"trailing_comma", `p({ :a => 1, })`, "{:a=>1}\n"},
		{"int_keys", `p({ 1 => "one", 2 => "two" })`, "{1=>\"one\", 2=>\"two\"}\n"},
		{"index_get", "h = { :a => 1 }\nputs h[:a]", "1\n"},
		{"index_missing", "h = { :a => 1 }\np h[:z]", "nil\n"},
		{"index_set_new", "h = {}\nh[:a] = 5\np h", "{:a=>5}\n"},
		{"index_set_update", "h = { :a => 1 }\nh[:a] = 9\np h", "{:a=>9}\n"},
		{"index_assign_value", "h = {}\nputs(h[:a] = 7)", "7\n"},
		{"size", `puts({ :a => 1, :b => 2 }.size)`, "2\n"},
		{"length", `puts({ :a => 1 }.length)`, "1\n"},
		{"empty_true", `puts({}.empty?)`, "true\n"},
		{"empty_false", `puts({ :a => 1 }.empty?)`, "false\n"},
		{"key_true", `puts({ :a => 1 }.key?(:a))`, "true\n"},
		{"key_false", `puts({ :a => 1 }.key?(:z))`, "false\n"},
		{"has_key", `puts({ :a => 1 }.has_key?(:a))`, "true\n"},
		{"include", `puts({ :a => 1 }.include?(:a))`, "true\n"},
		{"keys", `p({ :a => 1, :b => 2 }.keys)`, "[:a, :b]\n"},
		{"values", `p({ :a => 1, :b => 2 }.values)`, "[1, 2]\n"},
		{"each", "s = 0\n{ :a => 1, :b => 2 }.each { |k, v| s = s + v }\nputs s", "3\n"},
		{"class", `puts({}.class)`, "Hash\n"},
		{"eq_true", `puts({ :a => 1 } == { :a => 1 })`, "true\n"},
		{"eq_diff_value", `puts({ :a => 1 } == { :a => 2 })`, "false\n"},
		{"eq_diff_len", `puts({ :a => 1 } == { :a => 1, :b => 2 })`, "false\n"},
		{"eq_diff_key", `puts({ :a => 1 } == { :b => 1 })`, "false\n"},
		{"eq_non_hash", `puts({ :a => 1 } == "x")`, "false\n"},
		{"insertion_order", "h = {}\nh[:z] = 1\nh[:a] = 2\np h.keys", "[:z, :a]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestHashEachNoBlock(t *testing.T) {
	if err := runErr(t, `({}).each`); err == nil || !strings.Contains(err.Error(), "LocalJumpError") {
		t.Fatalf("got %v want LocalJumpError", err)
	}
}
