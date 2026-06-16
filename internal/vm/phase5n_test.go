package vm_test

import "testing"

func TestDupCloneFreeze(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"array_dup_independent", "a = [1, 2, 3]\nb = a.dup\nb << 4\np a\np b", "[1, 2, 3]\n[1, 2, 3, 4]\n"},
		{"hash_clone_independent", "h = {x: 1}\nh2 = h.clone\nh2[:y] = 2\np h\np h2", "{x: 1}\n{x: 1, y: 2}\n"},
		{"object_dup_independent", "class C\ndef initialize\n@v = 1\nend\ndef v\n@v\nend\ndef v=(x)\n@v = x\nend\nend\nc = C.new\nc2 = c.dup\nc2.v = 99\np c.v\np c2.v", "1\n99\n"},
		{"string_dup", `p "abc".dup`, "\"abc\"\n"},
		{"int_dup", `p 7.dup`, "7\n"},
		{"frozen_int", `p 5.frozen?`, "true\n"},
		{"frozen_symbol", `p :sym.frozen?`, "true\n"},
		{"frozen_nil", `p nil.frozen?`, "true\n"},
		{"frozen_bool", `p true.frozen?`, "true\n"},
		{"frozen_float", `p 3.14.frozen?`, "true\n"},
		{"unfrozen_string", `p "str".frozen?`, "false\n"},
		{"unfrozen_array", `p [1].frozen?`, "false\n"},
		{"unfrozen_hash", `p({}.frozen?)`, "false\n"},
		{"unfrozen_object", "class C\nend\np C.new.frozen?", "false\n"},
		{"freeze_returns_self", `p 5.freeze`, "5\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
