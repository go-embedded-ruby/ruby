// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"
	"os"
	"os/user"
	"testing"
)

// TestDirMkdir covers Dir.mkdir: a #to_path path argument, a #to_int permissions
// argument, the default and explicit-mode success paths, and the arity /
// TypeError / Errno guards (ruby/ruby v3_4_0 dir.c dir_s_mkdir).
func TestDirMkdir(t *testing.T) {
	dir := slash(t.TempDir())

	// Default mode, then a #to_int permissions argument, then a #to_path path
	// argument — each creates a real directory.
	ok := []struct{ src, want string }{
		{`Dir.mkdir("` + dir + `/a"); p File.directory?("` + dir + `/a")`, "true\n"},
		{`o = Object.new; def o.to_int; 0755; end; Dir.mkdir("` + dir + `/b", o); p File.directory?("` + dir + `/b")`, "true\n"},
		{`o = Object.new; def o.to_path; "` + dir + `/c"; end; Dir.mkdir(o); p File.directory?("` + dir + `/c")`, "true\n"},
		{`p Dir.mkdir("` + dir + `/d")`, "0\n"}, // returns 0
	}
	for _, c := range ok {
		if got := runFS(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}

	// Guards: wrong arity, non-#to_int permissions (TypeError), an already-existing
	// directory (Errno::EEXIST, via raiseMkdirErr), and a missing intermediate
	// directory (Errno::ENOENT).
	errCases := []struct{ src, want string }{
		{`Dir.mkdir`, "ArgumentError"},
		{`Dir.mkdir("a", "b", "c")`, "ArgumentError"},
		{`Dir.mkdir("` + dir + `/e", Object.new)`, "TypeError"},
		{`Dir.mkdir("` + dir + `/a"); Dir.mkdir("` + dir + `/a")`, "Errno::EEXIST"},
		{`Dir.mkdir("` + dir + `/nope/deeper")`, "Errno::ENOENT"},
	}
	for _, c := range errCases {
		if got := runFSErr(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestRaiseMkdirErr covers every branch of raiseMkdirErr directly with synthetic
// errors, so the EACCES branch (an unwritable parent) is exercised
// deterministically rather than depending on the runner's privileges.
func TestRaiseMkdirErr(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{os.ErrExist, "Errno::EEXIST"},
		{os.ErrPermission, "Errno::EACCES"},
		{os.ErrNotExist, "Errno::ENOENT"},
		{errors.New("some other failure"), "Errno::ENOENT"},
	}
	for _, c := range cases {
		got := func() (cls string) {
			defer func() {
				if re, ok := recover().(RubyError); ok {
					cls = re.Class
				}
			}()
			raiseMkdirErr(c.err, "/x")
			return ""
		}()
		if got != c.want {
			t.Errorf("raiseMkdirErr(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

// TestDirHome covers Dir.home: no argument / nil reads $HOME, a user-name
// argument reads the passwd database, and an unknown user raises ArgumentError
// (ruby/ruby v3_4_0 dir.c dir_s_home / rb_home_dir_of).
func TestDirHome(t *testing.T) {
	t.Setenv("HOME", "/rubyspec_home")
	if got := runFS(t, `p Dir.home`); got != "\"/rubyspec_home\"\n" {
		t.Errorf("Dir.home = %q, want /rubyspec_home", got)
	}
	if got := runFS(t, `p Dir.home(nil)`); got != "\"/rubyspec_home\"\n" {
		t.Errorf("Dir.home(nil) = %q, want /rubyspec_home", got)
	}
	// A real user name resolves to that user's home from the passwd database.
	if u, err := user.Current(); err == nil {
		src := `p Dir.home(` + strconvQuote(u.Username) + `).is_a?(String)`
		if got := runFS(t, src); got != "true\n" {
			t.Errorf("Dir.home(current user) = %q, want true", got)
		}
	}
	if got := runFSErr(t, `Dir.home("no_such_user_rbgo_xyz_123")`); got != "ArgumentError" {
		t.Errorf("Dir.home(bogus) = %q, want ArgumentError", got)
	}
}

// strconvQuote quotes s as a Ruby (and Go) double-quoted string literal for
// embedding a username into test source.
func strconvQuote(s string) string { return "\"" + s + "\"" }
