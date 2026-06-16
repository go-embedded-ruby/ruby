package vm_test

import (
	"strings"
	"testing"
)

func TestFindPattern(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"basic", "case [1, 2, 3, 4, 5]\nin [*, 3, *]\np \"has3\"\nend", "\"has3\"\n"},
		{"named", "case [1, 2, 3, 4, 5]\nin [*pre, 3, *post]\np [pre, post]\nend", "[[1, 2], [4, 5]]\n"},
		{"match_at_zero", "case [3, 4, 5]\nin [*pre, 3, *post]\np [pre, post]\nend", "[[], [4, 5]]\n"},
		{"match_at_end", "case [1, 2, 3]\nin [*pre, 3, *post]\np [pre, post]\nend", "[[1, 2], []]\n"},
		{"no_match", "case [1, 2, 4]\nin [*, 3, *]\np \"y\"\nelse\np \"no3\"\nend", "\"no3\"\n"},
		{"bind_mid", "case [5, 1, 2, 3]\nin [*, x, 3, *]\np x\nend", "2\n"},
		{"multi_mid", "case [1, 2, 3, 4]\nin [*a, 2, 3, *b]\np [a, b]\nend", "[[1], [4]]\n"},
		{"second_mid_fails_window", "case [1, 3, 1, 2]\nin [*, 1, 2, *]\np \"found\"\nend", "\"found\"\n"},
		{"non_array", "case \"x\"\nin [*, 1, *]\np \"y\"\nelse\np \"notarr\"\nend", "\"notarr\"\n"},
		{"const_find", "C = Array\ncase [1, 2, 3]\nin C[*, 2, *]\np \"cf\"\nend", "\"cf\"\n"},
		{"const_find_wrong_type", "C = Array\ncase 42\nin C[*, 2, *]\np \"y\"\nelse\np \"wt\"\nend", "\"wt\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestMultipleSplatError(t *testing.T) {
	if err := runErr(t, "case [1]\nin [a, *b, *c]\np 1\nend"); err == nil || !strings.Contains(err.Error(), "multiple *") {
		t.Fatalf("got %v want multiple-splat error", err)
	}
}
