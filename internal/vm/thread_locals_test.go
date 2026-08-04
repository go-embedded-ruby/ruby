// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestThreadLocals covers Thread#fetch, Thread#keys and the shared fiber-local
// key coercion of Thread#[] / #[]= / #key? / #fetch, asserted against MRI 3.4.
func TestThreadLocals(t *testing.T) {
	cases := []struct{ src, want string }{
		// fetch: value when present; default / block when absent; block precedence.
		{`Thread.current[:a] = 1; p Thread.current.fetch(:a)`, "1\n"},
		{`p Thread.current.fetch(:absent_zz, 9)`, "9\n"},
		{`p Thread.current.fetch(:absent_zy) { 7 }`, "7\n"},
		{`Thread.current[:b] = 2; p Thread.current.fetch(:b) { 7 }`, "2\n"},
		{`Thread.current[:c] = 3; var = :no; Thread.current.fetch(:c) { var = :yes }; p var`, ":no\n"},
		{`p Thread.current.fetch(:absent_zx, 1) { 5 }`, "5\n"}, // block supersedes default

		// Key coercion: String and Symbol address the same slot; #to_str is honoured.
		{`Thread.current[:x] = 1; p Thread.current.key?("x")`, "true\n"},
		{`class K1; def to_str; "kk"; end; end; Thread.current[K1.new] = 5; p Thread.current["kk"]`, "5\n"},
		{`class K2; def to_str; "q"; end; end; Thread.current["q"] = 8; p Thread.current.key?(K2.new)`, "true\n"},

		// keys: fiber-locals of a thread, as sorted Symbols; empty when none set.
		{`t = Thread.new { Thread.current[:m] = 1; Thread.current[:a] = 2 }; t.join; p t.keys`, "[:a, :m]\n"},
		{`t = Thread.new {}; t.join; p t.keys`, "[]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}

	errs := []struct{ src, want string }{
		// fetch arity and the missing-key KeyError.
		{`Thread.current.fetch`, "wrong number of arguments"},
		{`Thread.current.fetch(1, 2, 3)`, "wrong number of arguments"},
		{`Thread.current.fetch(:definitely_missing_key)`, "key not found"},
		// Wrong key types raise TypeError, including a #to_str that returns a non-String.
		{`Thread.current[nil] = 1`, "is not a symbol nor a string"},
		{`Thread.current[5]`, "is not a symbol nor a string"},
		{`class Bad1; def to_str; 5; end; end; Thread.current[Bad1.new]`, "is not a symbol nor a string"},
	}
	for _, c := range errs {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want error containing %q", c.src, err, c.want)
		}
	}
}
