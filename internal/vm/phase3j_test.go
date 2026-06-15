package vm_test

import (
	"strings"
	"testing"
)

func TestMoreStringMethods(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"ljust", `p "hi".ljust(5)`, "\"hi   \"\n"},
		{"ljust_pad", `p "hi".ljust(5, ".")`, "\"hi...\"\n"},
		{"ljust_short", `p "abc".ljust(2)`, "\"abc\"\n"},
		{"rjust", `p "hi".rjust(5, "0")`, "\"000hi\"\n"},
		{"center", `p "hi".center(7)`, "\"  hi   \"\n"},
		{"center_pad", `p "hi".center(7, "*")`, "\"**hi***\"\n"},
		{"center_multichar_pad", `p "hi".center(8, "ab")`, "\"abahiaba\"\n"},
		{"center_short", `p "hello".center(3)`, "\"hello\"\n"},
		{"tr", `p "hello".tr("el", "ip")`, "\"hippo\"\n"},
		{"tr_range", `p "hello".tr("a-y", "b-z")`, "\"ifmmp\"\n"},
		{"tr_delete", `p "hello".tr("l", "")`, "\"heo\"\n"},
		{"tr_to_shorter", `p "hello".tr("el", "x")`, "\"hxxxo\"\n"},
		{"count", `p "hello".count("l")`, "2\n"},
		{"count_set", `p "hello world".count("lo")`, "5\n"},
		{"delete", `p "hello".delete("l")`, "\"heo\"\n"},
		{"squeeze", `p "aaabbbccc".squeeze`, "\"abc\"\n"},
		{"squeeze_mixed", `p "aabbaa".squeeze`, "\"aba\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStringPadEmptyPad(t *testing.T) {
	if err := runErr(t, `"x".ljust(5, "")`); err == nil || !strings.Contains(err.Error(), "ArgumentError") {
		t.Fatalf("got %v, want ArgumentError", err)
	}
}
