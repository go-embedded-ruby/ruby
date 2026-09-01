package vm_test

import (
	"strings"
	"testing"
)

// TestHashMethodResiduals covers the MRI 4.0.6 conformance fixes for Hash
// residual (non-equality) methods: genuine built-in aliases, two-argument
// block yields, compare_by_identity / default propagation, sized enumerators,
// #inspect element dispatch, and assorted per-method behaviour.
func TestHashMethodResiduals(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		// --- genuine built-in aliases (shared method records) ---
		{"alias_has_key", `puts Hash.instance_method(:has_key?) == Hash.instance_method(:key?)`, "true\n"},
		{"alias_include", `puts Hash.instance_method(:include?) == Hash.instance_method(:key?)`, "true\n"},
		{"alias_member", `puts Hash.instance_method(:member?) == Hash.instance_method(:key?)`, "true\n"},
		{"alias_has_value", `puts Hash.instance_method(:has_value?) == Hash.instance_method(:value?)`, "true\n"},
		{"alias_length", `puts Hash.instance_method(:length) == Hash.instance_method(:size)`, "true\n"},
		{"alias_store", `puts Hash.instance_method(:store) == Hash.instance_method(:[]=)`, "true\n"},
		{"alias_update", `puts Hash.instance_method(:update) == Hash.instance_method(:merge!)`, "true\n"},
		{"alias_filter", `puts Hash.instance_method(:filter) == Hash.instance_method(:select)`, "true\n"},
		{"alias_filter_bang", `puts Hash.instance_method(:filter!) == Hash.instance_method(:select!)`, "true\n"},
		{"alias_to_s", `puts Hash.instance_method(:to_s) == Hash.instance_method(:inspect)`, "true\n"},
		{"alias_func_has_key", `puts({a: 1}.has_key?(:a))`, "true\n"},
		{"alias_func_length", `puts({a: 1, b: 2}.length)`, "2\n"},
		{"alias_func_store", `h = {}; h.store(:a, 9); puts h[:a]`, "9\n"},
		{"alias_func_filter", `p({1=>2, 3=>4}.filter { |k, v| k == 1 })`, "{1 => 2}\n"},

		// --- select/reject/delete_if/keep_if yield key and value as two args ---
		{"select_splat", `a = []; {1=>2, 3=>4}.select { |*x| a << x }; p a`, "[[1, 2], [3, 4]]\n"},
		{"select_single", `a = []; {1=>2, 3=>4}.select { |k| a << k; true }; p a`, "[1, 3]\n"},
		{"reject_splat", `a = []; {1=>2}.reject { |*x| a << x; false }; p a`, "[[1, 2]]\n"},
		{"delete_if_splat", `a = []; {1=>2}.delete_if { |*x| a << x; false }; p a`, "[[1, 2]]\n"},
		{"keep_if_splat", `a = []; {1=>2}.keep_if { |*x| a << x; true }; p a`, "[[1, 2]]\n"},
		{"select_bang_single", `a = []; {1=>2, 3=>4}.select! { |k| a << k; true }; p a`, "[1, 3]\n"},
		{"reject_bang_single", `a = []; {1=>2, 3=>4}.reject! { |k| a << k; false }; p a`, "[1, 3]\n"},

		// --- compare_by_identity flag propagation ---
		{"cbi_select", `h = {a: 1}.compare_by_identity; puts h.select { true }.compare_by_identity?`, "true\n"},
		{"cbi_reject", `h = {a: 1}.compare_by_identity; puts h.reject { false }.compare_by_identity?`, "true\n"},
		{"cbi_merge", `h = {}.compare_by_identity; puts h.merge({}).compare_by_identity?`, "true\n"},
		{"cbi_slice", `h = {a: 1}.compare_by_identity; puts h.slice(:a).compare_by_identity?`, "true\n"},
		{"cbi_except", `h = {a: 1}.compare_by_identity; puts h.except(:z).compare_by_identity?`, "true\n"},
		{"cbi_compact", `h = {a: 1}.compare_by_identity; puts h.compact.compare_by_identity?`, "true\n"},
		{"cbi_transform_values", `h = {a: 1}.compare_by_identity; puts h.transform_values { |v| v }.compare_by_identity?`, "true\n"},
		{"cbi_off", `puts({a: 1}.select { true }.compare_by_identity?)`, "false\n"},

		// --- merge retains default / default_proc, and coerces via #to_hash ---
		{"merge_default", `h = Hash.new(9); h[:a] = 1; puts h.merge({b: 2}).default`, "9\n"},
		{"merge_default_proc", `h = Hash.new { 42 }; m = h.merge({b: 2}); puts m[:zzz]`, "42\n"},
		{"merge_to_hash", `class C; def to_hash; {x: 1}; end; end; p({a: 0}.merge(C.new))`, "{a: 0, x: 1}\n"},
		{"merge_block", `p({a: 1}.merge({a: 2}) { |k, o, n| o + n })`, "{a: 3}\n"},

		// --- sized enumerators (#size does not run/mutate the method) ---
		{"enum_size_select", `puts({a: 1, b: 2, c: 3}.select.size)`, "3\n"},
		{"enum_size_each", `puts({a: 1, b: 2}.each.size)`, "2\n"},
		{"enum_size_tv_bang", `h = {a: 1, b: 2, c: 3}; e = h.transform_values!; puts e.size; e.each(&:succ); p h`, "3\n{a: 2, b: 3, c: 4}\n"},
		{"enum_size_tk", `puts({a: 1, b: 2}.transform_keys.size)`, "2\n"},

		// --- inspect / to_s ---
		// These call #inspect explicitly (rather than via p) so they route through
		// Hash#inspect, exercising every symbol-label branch of symIsPlainLabel.
		{"inspect_basic", `puts({a: 1, "b" => 2}.inspect)`, "{a: 1, \"b\" => 2}\n"},
		{"inspect_empty", `puts({}.inspect)`, "{}\n"},
		{"inspect_labels", `puts({a: 1, a!: 2, a?: 3, :"a=" => 4}.inspect)`, "{a: 1, a!: 2, a?: 3, \"a=\": 4}\n"},
		{"inspect_quoted_labels", `puts({:"<=>" => 1, :"@a" => 2, :"[]=" => 3, :"" => 4}.inspect)`, "{\"<=>\": 1, \"@a\": 2, \"[]=\": 3, \"\": 4}\n"},
		{"inspect_recursive", `h = {}; h[0] = h; puts h.inspect`, "{0 => {...}}\n"},
		{"inspect_custom", `class M; def inspect; "MI"; end; end; puts({a: M.new}.inspect)`, "{a: MI}\n"},
		{"to_s_alias_func", `puts({a: 1}.to_s)`, "{a: 1}\n"},

		// --- dig respects the default ---
		{"dig_default", `d = {bar: 42}; h = Hash.new(d); puts h.dig(:foo, :bar)`, "42\n"},
		{"dig_default_nil", `p({a: 1}.dig(:missing))`, "nil\n"},
		{"dig_present", `p({a: {b: 5}}.dig(:a, :b))`, "5\n"},

		// --- delete with a block for an absent key ---
		{"delete_block_absent", `puts({a: 1}.delete(:z) { |k| 5 })`, "5\n"},
		{"delete_block_present", `puts({a: 1}.delete(:a) { |k| 5 })`, "1\n"},
		{"delete_noblock", `p({a: 1}.delete(:z))`, "nil\n"},

		// --- to_h with a block coerces the pair via #to_ary ---
		{"to_h_to_ary", `class P; def to_ary; [:k, :v]; end; end; p({a: 1}.to_h { |k| P.new })`, "{k: :v}\n"},
		{"to_h_pair", `p({a: 1}.to_h { |k, v| [k, v + 1] })`, "{a: 2}\n"},

		// --- to_hash returns self ---
		{"to_hash_self", `h = {a: 1}; puts h.to_hash.equal?(h)`, "true\n"},

		// --- Hash[] coercion and the nil element message ---
		{"bracket_pairs", `p Hash[[[:a, 1], [:b, 2]]]`, "{a: 1, b: 2}\n"},
		{"bracket_kv", `p Hash[:a, 1, :b, 2]`, "{a: 1, b: 2}\n"},
		{"bracket_to_hash", `class C; def to_hash; {x: 1}; end; end; p Hash[C.new]`, "{x: 1}\n"},
		{"bracket_to_ary", `class C; def to_ary; [[:y, 2]]; end; end; p Hash[C.new]`, "{y: 2}\n"},

		// --- initialize resets the default and is private ---
		{"init_private", `puts Hash.private_instance_methods(false).include?(:initialize)`, "true\n"},
		{"init_reset_value", `h = {}; h.default = 42; h.send(:initialize, 1); puts h.default; h.send(:initialize); p h.default`, "1\nnil\n"},
		{"init_reset_proc", `h = Hash.new { 1 }; h.send(:initialize); p h.default_proc`, "nil\n"},

		// --- default_proc= coercion and lambda-arity checking ---
		{"dp_lambda2", `h = {}; h.default_proc = ->(a, b) { }; puts h.default_proc.lambda?`, "true\n"},
		{"dp_proc1", `h = {}; h.default_proc = proc { |x| }; puts h.default_proc.nil?`, "false\n"},
		{"dp_nil", `h = Hash.new { 1 }; h.default_proc = nil; p h.default_proc`, "nil\n"},
		{"dp_to_proc", `h = Hash.new { 'x' }; obj = Object.new; def obj.to_proc; proc { 'M' }; end; h.default_proc = obj; puts h[:k]`, "M\n"},

		// --- ruby2_keywords_hash retains default / default_proc / identity ---
		{"r2k_default", `puts Hash.ruby2_keywords_hash(Hash.new(1)).default`, "1\n"},
		{"r2k_default_proc", `puts Hash.ruby2_keywords_hash(Hash.new { 2 }).default_proc.nil?`, "false\n"},
		{"r2k_identity", `puts Hash.ruby2_keywords_hash({}.compare_by_identity).compare_by_identity?`, "true\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", tc.src, got, tc.want)
			}
		})
	}
}

// TestHashMethodResidualErrors covers the error paths of the residual fixes.
func TestHashMethodResidualErrors(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"fetch_zero_args", `{}.fetch`, "wrong number of arguments (given 0, expected 1..2)"},
		{"fetch_three_args", `{}.fetch(1, 2, 3)`, "wrong number of arguments (given 3, expected 1..2)"},
		{"merge_non_hash", `{}.merge(1)`, "no implicit conversion of Integer into Hash"},
		{"bracket_nil_element", `Hash[[nil]]`, "wrong element type nil at 0 (expected array)"},
		{"bracket_int_element", `Hash[[1]]`, "wrong element type Integer at 0 (expected array)"},
		{"dp_lambda1", `{}.default_proc = ->(a) { }`, "default_proc takes two arguments (2 for 1)"},
		{"dp_lambda3", `{}.default_proc = ->(a, b, c) { }`, "default_proc takes two arguments (2 for 3)"},
		{"dp_not_proc", `{}.default_proc = 5`, "no implicit conversion of Integer into Proc"},
		{"init_arity", `{}.send(:initialize, 1, 2)`, "wrong number of arguments (given 2, expected 0..1)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runErr(t, tc.src)
			if err == nil {
				t.Fatalf("src=%q: expected error, got nil", tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("src=%q\n got=%q\nwant substring %q", tc.src, err.Error(), tc.want)
			}
		})
	}
}
