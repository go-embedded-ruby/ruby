package vm_test

import "testing"

func TestBeginRescue(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"basic", "r = begin\nraise \"boom\"\nrescue\n\"caught\"\nend\np r", "\"caught\"\n"},
		{"no_exception_value", "r = begin\n10\nrescue\n20\nend\np r", "10\n"},
		{"bind_message", "begin\nraise ArgumentError, \"bad\"\nrescue ArgumentError => e\nputs e.message\nend", "bad\n"},
		{"bind_existing_local", "e = 0\nbegin\nraise \"z\"\nrescue => e\nputs e.message\nend", "z\n"},
		{"specific_class", "begin\nraise TypeError, \"t\"\nrescue ArgumentError\nputs \"no\"\nrescue TypeError => e\nputs e.message\nend", "t\n"},
		{"class_list", "begin\nraise KeyError\nrescue ArgumentError, IndexError => e\np e.class\nend", "KeyError\n"},
		{"subclass_match", "begin\nraise NoMethodError\nrescue NameError\nputs \"by super\"\nend", "by super\n"},
		{"else_runs", "begin\nx = 1\nrescue\nputs \"no\"\nelse\nputs \"yes\"\nend", "yes\n"},
		{"else_value", "r = begin\n1\nrescue\n2\nelse\n3\nend\np r", "3\n"},
		// internal errors are rescuable
		{"zero_div", "begin\n1 / 0\nrescue ZeroDivisionError => e\nputs e.message\nend", "divided by 0\n"},
		{"no_method", "begin\nnil.nope\nrescue => e\np e.class\nend", "NoMethodError\n"},
		{"domain_error", "begin\n(-5).digits\nrescue => e\np e.class\nend", "Math::DomainError\n"},
		// ensure
		{"ensure_success", "begin\nputs \"a\"\nensure\nputs \"b\"\nend", "a\nb\n"},
		{"ensure_on_rescue", "begin\nraise \"x\"\nrescue\nputs \"r\"\nensure\nputs \"e\"\nend", "r\ne\n"},
		{"ensure_value", "r = begin\n5\nensure\n99\nend\np r", "5\n"},
		// ensure runs while an exception propagates out
		{"ensure_propagate", "def f\nbegin\nraise \"deep\"\nensure\nputs \"cleanup\"\nend\nend\nbegin\nf\nrescue => e\nputs e.message\nend", "cleanup\ndeep\n"},
		// no clause matches → propagates to outer
		{"propagate_no_match", "begin\nbegin\nraise TypeError, \"t\"\nrescue ArgumentError\nputs \"no\"\nend\nrescue => e\np e.class\nend", "TypeError\n"},
		// bare raise in a rescue re-raises the current exception
		{"reraise", "begin\nbegin\nraise \"inner\"\nrescue\nraise\nend\nrescue => e\nputs e.message\nend", "inner\n"},
		// raising a user exception instance
		{"user_instance", "begin\nraise RuntimeError.new(\"custom\")\nrescue => e\nputs e.message\nend", "custom\n"},
		// break out of a block still works through a begin (not swallowed)
		{"break_through_begin", "p([1, 2, 3].each { |x| begin; break x * 10 if x == 2; rescue; end })", "20\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
