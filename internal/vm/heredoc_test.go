package vm_test

import "testing"

// Heredocs: <<ID, <<-ID (indented terminator), <<~ID (squiggly dedent), with
// bare/"…"/'…' markers (interpolating except the single-quoted form).
func TestHeredocs(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"basic", "x = <<HEREDOC\nhello\nworld\nHEREDOC\np x", "\"hello\\nworld\\n\"\n"},
		{"squiggly", "y = <<~SQL\n  SELECT 1\n  FROM t\nSQL\np y", "\"SELECT 1\\nFROM t\\n\"\n"},
		{"dash_indented_term", "z = <<-END\n  body\n    END\np z", "\"  body\\n\"\n"},
		{"single_quote_literal", "s = <<'EOS'\nno #{x} here\nback\\nslash\nEOS\np s", "\"no \\#{x} here\\nback\\\\nslash\\n\"\n"},
		{"double_quote_interp", "n = 3\nd = <<\"EOS\"\nval #{n}\ntab\\tend\nEOS\np d", "\"val 3\\ntab\\tend\\n\"\n"},
		{"bare_interp", "a = 2\nb = <<H\nsum #{a + a}\nH\np b", "\"sum 4\\n\"\n"},
		{"squiggly_interp_nested_string", "v = <<~T\n  #{1 + 1} #{\"x\".upcase}\nT\np v", "\"2 X\\n\"\n"},
		{"empty_body", "e = <<~X\nX\np e", "\"\"\n"},
		{"stacked", "arr = [<<A, <<B]\naaa\nA\nbbb\nB\np arr", "[\"aaa\\n\", \"bbb\\n\"]\n"},
		{"concat_on_line", "c = <<A + \"!\"\nhi\nA\np c", "\"hi\\n!\"\n"},
		{"puts_command_form", "puts <<~MSG\n  one\n    two\n  three\nMSG", "one\n  two\nthree\n"},
		{"squiggly_blank_shorter_line", "q = <<~T\n    a\n  \n    b\nT\np q", "\"a\\n\\nb\\n\"\n"},
		{"rest_of_line_continues", "h = <<E\nbody\nE\np h.upcase", "\"BODY\\n\"\n"},
		{"squiggly_single_quote", "q = <<~'X'\n  no #{x} t\nX\np q", "\"no \\#{x} t\\n\"\n"},
		{"hash_not_before_brace", "h = <<H\nval#end\nH\np h", "\"val#end\\n\"\n"},
		{"brace_inside_interp", "h = <<H\n#{ {1=>2}.size }\nH\np h", "\"1\\n\"\n"},
		{"literal_quote_in_body", "h = <<H\nsay \"hi\" now\nH\np h", "\"say \\\"hi\\\" now\\n\"\n"},
		{"shift_no_space", "a = [1]\nb = 2\na<<b\np a", "[1, 2]\n"},
		// `<<` remains the shift/append operator where a value is not expected.
		{"shift_array", "a = [1]\na << 2\np a", "[1, 2]\n"},
		{"append_string", "s = \"x\"\ns << \"y\"\np s", "\"xy\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// CRLF terminator lines and an unterminated heredoc (lenient: body runs to EOF)
// exercise the remaining lexer branches.
func TestHeredocEdgeCases(t *testing.T) {
	// CRLF line endings: the terminator is recognised despite the trailing \r;
	// the body keeps its \r, matching MRI.
	if got := eval(t, "x = <<E\r\nbody\r\nE\r\np x"); got != "\"body\r\\n\"\n" {
		t.Errorf("crlf heredoc = %q", got)
	}
	// An unterminated heredoc consumes the rest of the input as its body (so
	// there is nothing left to print); it must not crash or error.
	if got := eval(t, "y = <<MISSING\nline one\nline two\n"); got != "" {
		t.Errorf("unterminated heredoc = %q", got)
	}
	// `<<-` immediately at end of input is not a heredoc (no marker follows).
	if err := runErr(t, "x = 1 <<-"); err == nil {
		t.Error("expected error for dangling <<-")
	}
	// A bare `<<` at end of input is the shift operator, not a heredoc.
	if err := runErr(t, "x = 1 <<"); err == nil {
		t.Error("expected error for dangling <<")
	}
	// A heredoc whose `<<ID` is the last thing in the input (no newline after)
	// has an empty body and must not crash.
	if got := eval(t, "x = <<E"); got != "" {
		t.Errorf("eof heredoc = %q", got)
	}
	// An unterminated heredoc whose last body line has no trailing newline.
	if got := eval(t, "x = <<E\nbody"); got != "" {
		t.Errorf("no-trailing-newline heredoc = %q", got)
	}
}

// String#inspect escapes `#` only before an interpolation sigil; puts does not
// double a trailing newline. Both surfaced via heredocs.
func TestInspectAndPutsFixes(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"inspect_hash_brace", `p "a".gsub("a", "\#{x}")`, "\"\\#{x}\"\n"},
		{"inspect_hash_dollar", `p "a".gsub("a", "\#$x")`, "\"\\#$x\"\n"},
		{"inspect_hash_at", `p "a".gsub("a", "\#@x")`, "\"\\#@x\"\n"},
		{"inspect_hash_plain", `p "a#b"`, "\"a#b\"\n"},
		{"inspect_hash_end", `p "a#"`, "\"a#\"\n"},
		{"puts_no_double_nl", `puts "hi\n"`, "hi\n"},
		{"puts_adds_nl", `puts "hi"`, "hi\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
