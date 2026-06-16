package vm_test

import (
	"strings"
	"testing"
)

func TestStringOrdPartitionSlice(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"ord", `p "hello".ord`, "104\n"},
		{"ord_upper", `p "A".ord`, "65\n"},
		{"ord_multibyte", `p "é".ord`, "233\n"},
		{"partition_found", `p "hello".partition("l")`, "[\"he\", \"l\", \"lo\"]\n"},
		{"partition_missing", `p "hello".partition("z")`, "[\"hello\", \"\", \"\"]\n"},
		{"partition_multi", `p "a=b=c".partition("=")`, "[\"a\", \"=\", \"b=c\"]\n"},
		{"rpartition_found", `p "hello".rpartition("l")`, "[\"hel\", \"l\", \"o\"]\n"},
		{"rpartition_missing", `p "hello".rpartition("z")`, "[\"\", \"\", \"hello\"]\n"},
		{"rpartition_multi", `p "a=b=c".rpartition("=")`, "[\"a=b\", \"=\", \"c\"]\n"},
		{"slice_start_len", `p "hello".slice(1, 2)`, "\"el\"\n"},
		{"slice_range", `p "hello".slice(1..3)`, "\"ell\"\n"},
		{"slice_index", `p "hello".slice(0)`, "\"h\"\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestStringOrdEmpty(t *testing.T) {
	if err := runErr(t, `"".ord`); err == nil || !strings.Contains(err.Error(), "empty string") {
		t.Fatalf("got %v want empty string", err)
	}
}
