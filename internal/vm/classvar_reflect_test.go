package vm_test

import "testing"

// Module class-variable reflection mirrors MRI: get/set/defined?/class_variables,
// with inheritance and the same NameError/TypeError on bad names.
func TestClassVariableReflection(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"defined_true", `class C; @@x = 1; end; p C.class_variable_defined?(:@@x)`, "true\n"},
		{"defined_false", `class C; @@x = 1; end; p C.class_variable_defined?(:@@y)`, "false\n"},
		{"get", `class C; @@x = 1; end; p C.class_variable_get(:@@x)`, "1\n"},
		{"get_string", `class C; @@x = 7; end; p C.class_variable_get("@@x")`, "7\n"},
		{"set_new", `class C; end; C.class_variable_set(:@@z, 9); p C.class_variable_get(:@@z)`, "9\n"},
		{"set_returns_value", `class C; end; p C.class_variable_set(:@@z, 9)`, "9\n"},
		{"set_existing_walks_up", `class C; @@x = 1; end; class D < C; end; D.class_variable_set(:@@x, 5); p C.class_variable_get(:@@x)`, "5\n"},
		{"variables", `class C; @@x = 1; @@y = 2; end; p C.class_variables.sort`, "[:@@x, :@@y]\n"},
		{"variables_inherited", `class C; @@x = 1; end; class D < C; @@w = 2; end; p D.class_variables.sort`, "[:@@w, :@@x]\n"},
		{"variables_no_inherit", `class C; @@x = 1; end; class D < C; @@w = 2; end; p D.class_variables(false)`, "[:@@w]\n"},
		{"variables_empty", `class C; end; p C.class_variables`, "[]\n"},
		{"defined_inherited", `class C; @@x = 1; end; class D < C; end; p D.class_variable_defined?(:@@x)`, "true\n"},
		{"get_inherited", `class C; @@x = 1; end; class D < C; end; p D.class_variable_get(:@@x)`, "1\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("eval(%q) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

func TestClassVariableErrors(t *testing.T) {
	tests := []struct {
		src       string
		wantClass string
	}{
		{`class C; @@x = 1; end; C.class_variable_get(:@@nope)`, "NameError"},
		{`class C; end; C.class_variable_get(:x)`, "NameError"},
		{`class C; end; C.class_variable_get("x")`, "NameError"},
		{`class C; end; C.class_variable_get(:@@)`, "NameError"},
		{`class C; end; C.class_variable_get(5)`, "TypeError"},
		{`class C; end; C.class_variable_defined?(:bad)`, "NameError"},
		{`class C; end; C.class_variable_set(:bad, 1)`, "NameError"},
		{`class C; end; C.class_variable_set(7, 1)`, "TypeError"},
	}
	for _, tc := range tests {
		class, _ := evalErr(t, tc.src)
		if class != tc.wantClass {
			t.Errorf("%q: got %q, want %q", tc.src, class, tc.wantClass)
		}
	}
}
