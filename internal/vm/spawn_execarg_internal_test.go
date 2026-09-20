// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !windows && !wasm

package vm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSpawn installs a runSpawnProc that records each prepared child and writes
// fixed text to its two streams, so the argument-peeling and redirection paths
// can be asserted without running anything.
func fakeSpawn(t *testing.T, out, errOut string, code int) *[]*spawnReq {
	t.Helper()
	var got []*spawnReq
	orig := runSpawnProc
	runSpawnProc = func(r *spawnReq) int {
		got = append(got, r)
		_, _ = r.stdout.Write([]byte(out))
		_, _ = r.stderr.Write([]byte(errOut))
		return code
	}
	t.Cleanup(func() { runSpawnProc = orig })
	return &got
}

// TestExecArgCommandForms covers rb_exec_getargs + rb_check_argv: the shell
// form, an explicit argv, the [prog, argv0] command array, and the #to_ary /
// #to_str conversions each of them accepts.
func TestExecArgCommandForms(t *testing.T) {
	for _, tc := range []struct {
		src        string
		wantShell  string
		wantArgv   []string
		wantProgIn string // substring the resolved path must contain
	}{
		{src: `Process.spawn("echo hi there")`, wantShell: "echo hi there"},
		{src: `Process.spawn("echo", "hi")`, wantArgv: []string{"echo", "hi"}, wantProgIn: "echo"},
		{src: `Process.spawn(["/bin/echo", "argv_zero"], "hi")`,
			wantArgv: []string{"argv_zero", "hi"}, wantProgIn: "/bin/echo"},
		// #to_ary on the first argument, #to_str on each element.
		{src: `o = Object.new; def o.to_ary; ["/bin/echo", "zero"]; end; Process.spawn(o, "hi")`,
			wantArgv: []string{"zero", "hi"}, wantProgIn: "/bin/echo"},
		{src: `o = Object.new; def o.to_str; "hi"; end; Process.spawn("echo", o)`,
			wantArgv: []string{"echo", "hi"}, wantProgIn: "echo"},
		// A command array is never handed to the shell even with one argument.
		{src: `Process.spawn(["/bin/echo", "a b"])`, wantArgv: []string{"a b"}, wantProgIn: "/bin/echo"},
	} {
		got := fakeSpawn(t, "", "", 0)
		eval(t, tc.src)
		if len(*got) != 1 {
			t.Fatalf("%s: ran %d commands", tc.src, len(*got))
		}
		r := (*got)[0]
		if r.shell != tc.wantShell {
			t.Errorf("%s: shell = %q want %q", tc.src, r.shell, tc.wantShell)
		}
		if tc.wantShell == "" && strings.Join(r.argv, "\x00") != strings.Join(tc.wantArgv, "\x00") {
			t.Errorf("%s: argv = %v want %v", tc.src, r.argv, tc.wantArgv)
		}
		if tc.wantProgIn != "" && !strings.Contains(r.path, tc.wantProgIn) {
			t.Errorf("%s: path = %q want one containing %q", tc.src, r.path, tc.wantProgIn)
		}
	}
}

// TestExecArgErrors pins every argument error rb_check_argv, check_exec_env_i
// and check_exec_options_i raise, byte for byte against MRI 4.0.5.
func TestExecArgErrors(t *testing.T) {
	fakeSpawn(t, "", "", 0)
	for _, tc := range []struct{ src, class, msg string }{
		{`Process.spawn`, "ArgumentError", "wrong number of arguments (given 0, expected 1+)"},
		{`Process.spawn({})`, "ArgumentError", "wrong number of arguments (given 0, expected 1+)"},
		{`Process.spawn({}, {})`, "ArgumentError", "wrong number of arguments (given 0, expected 1+)"},
		{`Process.spawn(:echo)`, "TypeError", "no implicit conversion of Symbol into String"},
		{`Process.spawn("echo", :foo)`, "TypeError", "no implicit conversion of Symbol into String"},
		{`o = Object.new; def o.to_str; 1; end; Process.spawn(o)`, "TypeError", "can't convert Object to String"},
		{"Process.spawn(\"\\000\")", "ArgumentError", "string contains null byte"},
		{"Process.spawn(\"echo\", \"\\000\")", "ArgumentError", "string contains null byte"},
		{`Process.spawn([])`, "ArgumentError", "wrong first argument"},
		{`Process.spawn([:a])`, "ArgumentError", "wrong first argument"},
		{`Process.spawn([:a, :b, :c])`, "ArgumentError", "wrong first argument"},
		{`Process.spawn([:echo, "echo"])`, "TypeError", "no implicit conversion of Symbol into String"},
		{`Process.spawn({"FOO=" => "BAR"}, "echo")`, "ArgumentError", "environment name contains a equal : FOO="},
		{"Process.spawn({\"\\000\" => \"BAR\"}, \"echo\")", "ArgumentError", "string contains null byte"},
		{"Process.spawn({\"FOO\" => \"\\000\"}, \"echo\")", "ArgumentError", "string contains null byte"},
		{`Process.spawn("echo", pgroup: -1)`, "ArgumentError", "negative process group ID : -1"},
		{`Process.spawn("echo", pgroup: :true)`, "TypeError", "no implicit conversion of Symbol into Integer"},
		{`Process.spawn("echo", "chdir" => Dir.pwd)`, "ArgumentError", "wrong exec option"},
		{`Process.spawn("echo", nonesuch: :foo)`, "ArgumentError", "wrong exec option symbol: nonesuch"},
		{`Process.spawn("echo", out: Object.new)`, "ArgumentError", "wrong exec redirect action"},
		{`Process.spawn("echo", out: [])`, "ArgumentError", "wrong exec redirect action"},
		{`Process.spawn("echo", chdir: "no-such-directory-xyz")`,
			"Errno::ENOENT", "No such file or directory - no-such-directory-xyz"},
	} {
		class, msg := evalErr(t, tc.src)
		if class != tc.class || msg != tc.msg {
			t.Errorf("%s: got %s/%q want %s/%q", tc.src, class, msg, tc.class, tc.msg)
		}
	}
}

// TestExecArgOptions covers the option keys that ARE honoured (:chdir, :pgroup's
// accepted shapes, the accepted-and-ignored set) and the environment the child
// ends up with under :unsetenv_others.
func TestExecArgOptions(t *testing.T) {
	dir := t.TempDir()
	got := fakeSpawn(t, "", "", 0)

	eval(t, `Process.spawn("echo hi", chdir: `+quoteRuby(dir)+`)`)
	if (*got)[0].dir != dir {
		t.Errorf("chdir: got %q want %q", (*got)[0].dir, dir)
	}
	// :pgroup false / nil / true / a non-negative pgid are all accepted, and so
	// are the options this VM records but cannot apply to a synchronous child.
	*got = nil
	eval(t, `Process.spawn("echo hi", pgroup: false)
Process.spawn("echo hi", pgroup: nil)
Process.spawn("echo hi", pgroup: true)
Process.spawn("echo hi", pgroup: 0)
Process.spawn("echo hi", umask: 146, close_others: true, exception: false, in: 0)`)
	if len(*got) != 5 {
		t.Fatalf("accepted options: ran %d commands", len(*got))
	}

	// No environment Hash and no :unsetenv_others ⇒ inherit (a nil environment).
	*got = nil
	eval(t, `Process.spawn("echo hi")`)
	if (*got)[0].env != nil {
		t.Errorf("no env Hash: got %v want nil", (*got)[0].env)
	}
	// An environment Hash overrides, adds and (with a nil value) removes.
	*got = nil
	eval(t, `ENV["RBGO_SPAWN_BASE"] = "base"
Process.spawn({"RBGO_SPAWN_BASE" => nil, "RBGO_SPAWN_NEW" => "new"}, "echo hi")`)
	env := envOf((*got)[0].env)
	if _, still := env["RBGO_SPAWN_BASE"]; still {
		t.Error("a nil environment value must unset the variable")
	}
	if env["RBGO_SPAWN_NEW"] != "new" {
		t.Errorf("added variable: got %q", env["RBGO_SPAWN_NEW"])
	}
	if _, ok := env["PATH"]; !ok {
		t.Error("without :unsetenv_others the child still inherits PATH")
	}
	// :unsetenv_others keeps ONLY the Hash's own names.
	*got = nil
	eval(t, `Process.spawn({"ONLY" => "1"}, "echo hi", unsetenv_others: true)`)
	if env := envOf((*got)[0].env); len(env) != 1 || env["ONLY"] != "1" {
		t.Errorf("unsetenv_others: got %v want only ONLY=1", env)
	}
	// :unsetenv_others false leaves the inherited environment in place.
	*got = nil
	eval(t, `Process.spawn({"ONLY" => "1"}, "echo hi", unsetenv_others: false)`)
	if env := envOf((*got)[0].env); len(env) < 2 {
		t.Errorf("unsetenv_others false: got %v", env)
	}
	// :unsetenv_others with no environment Hash at all still narrows the child's
	// environment to nothing.
	*got = nil
	eval(t, `Process.spawn("echo hi", unsetenv_others: true)`)
	if env := envOf((*got)[0].env); len(env) != 0 {
		t.Errorf("unsetenv_others without an env Hash: got %v", env)
	}
	// #to_hash makes an object the environment; #to_str converts its keys/values.
	*got = nil
	eval(t, `o = Object.new; def o.to_hash; {"VIA_TO_HASH" => "1"}; end
Process.spawn(o, "echo hi", unsetenv_others: true)
k = Object.new; def k.to_str; "K"; end
v = Object.new; def v.to_str; "V"; end
Process.spawn({k => v}, "echo hi", unsetenv_others: true)`)
	if env := envOf((*got)[0].env); env["VIA_TO_HASH"] != "1" {
		t.Errorf("#to_hash environment: got %v", env)
	}
	if env := envOf((*got)[1].env); env["K"] != "V" {
		t.Errorf("#to_str environment key/value: got %v", env)
	}
	// A String or an Array in the leading slot is a command, never the
	// environment, and an object with no #to_hash likewise.
	*got = nil
	eval(t, `o = Object.new; def o.to_str; "echo hi"; end; Process.spawn(o)`)
	if (*got)[0].shell != "echo hi" {
		t.Errorf("a #to_str object is the command: got %+v", (*got)[0])
	}
}

// TestExecArgRedirection covers :out / :err delivery — to a file, to an IO, to
// the interpreter's own streams, and the folds this VM treats as already
// merged.
func TestExecArgRedirection(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "out.txt")
	fakeSpawn(t, "to-stdout", "to-stderr", 0)

	// out: String truncates; ["name", "a"] appends; ["name", "w"] truncates.
	eval(t, `Process.spawn("echo hi", out: `+quoteRuby(name)+`)`)
	mustRead(t, name, "to-stdout")
	eval(t, `Process.spawn("echo hi", out: [`+quoteRuby(name)+`, "a"])`)
	mustRead(t, name, "to-stdoutto-stdout")
	eval(t, `Process.spawn("echo hi", out: [`+quoteRuby(name)+`, "w"])`)
	mustRead(t, name, "to-stdout")
	// A bare [name] Array opens for writing too.
	eval(t, `Process.spawn("echo hi", out: [`+quoteRuby(name)+`])`)
	mustRead(t, name, "to-stdout")
	// err: String.
	eval(t, `Process.spawn("echo hi", err: `+quoteRuby(name)+`)`)
	mustRead(t, name, "to-stderr")

	// out: IO, and an object that answers #to_io.
	if out := eval(t, `require "stringio"
io = StringIO.new(+"")
Process.spawn("echo hi", out: io)
print io.string`); out != "to-stderrto-stdout" {
		t.Errorf("out: IO got %q", out)
	}
	if out := eval(t, `require "stringio"
io = StringIO.new(+"")
o = Object.new
o.define_singleton_method(:to_io) { io }
Process.spawn("echo hi", out: o)
print io.string`); out != "to-stderrto-stdout" {
		t.Errorf("out: #to_io got %q", out)
	}
	// With no redirection the child's streams are the interpreter's own (which
	// this harness gives one buffer, so both land there in order).
	if out := eval(t, `Process.spawn("echo hi")`); out != "to-stdoutto-stderr" {
		t.Errorf("inherited stdout: got %q", out)
	}
	// The folds this VM cannot express are accepted and merged: a descriptor
	// number, :close, and [:child, :out].
	eval(t, `Process.spawn("echo hi", out: 5, err: :close)
Process.spawn("echo hi", err: [:child, :out])`)

	// A redirection target that cannot be opened raises the open's errno.
	class, msg := evalErr(t, `Process.spawn("echo hi", out: `+quoteRuby(filepath.Join(dir, "no", "such", "f"))+`)`)
	if class != "Errno::ENOENT" || !strings.HasPrefix(msg, "No such file or directory - ") {
		t.Errorf("unopenable :out: got %s/%q", class, msg)
	}
}

// TestSpawnResolve covers the program lookup that decides which errno a command
// that cannot be run reports — the one MRI learns from its child's failed
// execve. It exercises the real resolver, not a stub.
func TestSpawnResolve(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "runnable")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("not executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		prog, env          string
		wantOK             bool
		wantClass, wantMsg string
	}{
		{prog: exe, wantOK: true},
		{prog: filepath.Join(dir, "missing"), wantClass: "Errno::ENOENT",
			wantMsg: "No such file or directory - " + filepath.Join(dir, "missing")},
		{prog: dir, wantClass: "Errno::EACCES", wantMsg: "Permission denied - " + dir},
		{prog: plain, wantClass: "Errno::EACCES", wantMsg: "Permission denied - " + plain},
		// A bare name is looked up in the CHILD's PATH.
		{prog: "runnable", env: dir, wantOK: true},
		{prog: "plain", env: dir, wantClass: "Errno::ENOENT",
			wantMsg: "No such file or directory - plain"},
		{prog: "runnable", env: "", wantClass: "Errno::ENOENT",
			wantMsg: "No such file or directory - runnable"},
	} {
		var env []string
		if !strings.ContainsRune(tc.prog, os.PathSeparator) {
			env = []string{"PATH=" + tc.env}
		}
		path, class, msg, ok := spawnResolve(tc.prog, env)
		if ok != tc.wantOK {
			t.Errorf("spawnResolve(%q, PATH=%q): ok = %v want %v (%s/%s)", tc.prog, tc.env, ok, tc.wantOK, class, msg)
			continue
		}
		if !ok && (class != tc.wantClass || msg != tc.wantMsg) {
			t.Errorf("spawnResolve(%q): got %s/%q want %s/%q", tc.prog, class, msg, tc.wantClass, tc.wantMsg)
		}
		if ok && path == "" {
			t.Errorf("spawnResolve(%q): resolved to an empty path", tc.prog)
		}
	}
	// An empty PATH element means the current directory, which is how a child
	// with PATH=":/bin" searches.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := spawnResolve("runnable", []string{"PATH=:" + cwd}); !ok {
		t.Error("an empty PATH element must search the current directory")
	}
	// With no environment override the resolver reads the interpreter's own PATH.
	if _, _, _, ok := spawnResolve("no-such-program-xyz", nil); ok {
		t.Error("a missing program must not resolve against the inherited PATH")
	}
	if got := spawnEnvGet(nil, "PATH"); got != os.Getenv("PATH") {
		t.Errorf("spawnEnvGet(nil): got %q", got)
	}
	if got := spawnEnvGet([]string{"A=1"}, "MISSING"); got != "" {
		t.Errorf("spawnEnvGet(missing): got %q", got)
	}
}

// TestSpawnFailureStatus: a command that cannot be started raises its errno and
// leaves $? reporting the 127 the child MRI forked would have exited with — and
// leaves nothing for Process.wait to reap, because that child is already reaped.
func TestSpawnFailureStatus(t *testing.T) {
	fakeSpawn(t, "", "", 0)
	if got := eval(t, `begin
  Process.spawn("no-such-program-xyz")
rescue Errno::ENOENT => e
  p [e.message, $?.exitstatus, Process.waitall]
end`); got != `["No such file or directory - no-such-program-xyz", 127, []]`+"\n" {
		t.Errorf("failed spawn: got %q", got)
	}
	if got := eval(t, `begin
  Process.spawn("echo", chdir: "no-such-directory-xyz")
rescue Errno::ENOENT
  p [$?.exitstatus, Process.waitall]
end`); got != "[127, []]\n" {
		t.Errorf("failed chdir: got %q", got)
	}
}

// TestProcessWaitFamily covers wait / wait2 / waitpid / waitpid2 / waitall /
// last_status and Process::Status.wait over the recorded-child model.
func TestProcessWaitFamily(t *testing.T) {
	fakeSpawn(t, "", "", 7)
	for _, tc := range []struct{ src, want string }{
		// wait with no argument takes the oldest child and sets $?.
		{`pid = Process.spawn("echo hi"); p Process.wait == pid, $?.exitstatus`, "true\n7\n"},
		{`pid = Process.spawn("echo hi"); p Process.wait(pid) == pid`, "true\n"},
		{`pid = Process.spawn("echo hi"); p Process.wait(pid, 0) == pid`, "true\n"},
		{`pid = Process.spawn("echo hi"); p Process.wait(nil, nil) == pid`, "true\n"},
		{`pid = Process.spawn("echo hi"); p Process.wait2(pid).map { |v| v.class }`, "[Integer, Process::Status]\n"},
		// waitpid / waitpid2 are the very same methods, as MRI's aliases are.
		{`p Process.method(:waitpid) == Process.method(:wait)`, "true\n"},
		{`p Process.method(:waitpid2) == Process.method(:wait2)`, "true\n"},
		{`pid = Process.spawn("echo hi"); p Process.waitpid(pid) == pid`, "true\n"},
		{`pid = Process.spawn("echo hi"); p Process.waitpid2(pid)[1].exitstatus`, "7\n"},
		// waitall reaps everything, in order, and empties the table.
		{`a = Process.spawn("echo hi"); b = Process.spawn("echo hi")
p Process.waitall.map(&:first) == [a, b], Process.waitall`, "true\n[]\n"},
		// WNOHANG on a child that is not there reports "nothing ready".
		{`p Process.wait(999999, Process::WNOHANG)`, "nil\n"},
		{`p Process.wait2(999999, Process::WNOHANG)`, "nil\n"},
		// last_status is $?, and nil before any child has run.
		{`p Process.last_status`, "nil\n"},
		{`Process.wait Process.spawn("echo hi"); p Process.last_status.exitstatus`, "7\n"},
		// Process::Status.wait answers with a Status, and with pid -1 when there
		// is no child rather than raising.
		{`p Process::Status.wait.pid`, "-1\n"},
		{`pid = Process.spawn("echo hi"); p Process::Status.wait(pid).exitstatus`, "7\n"},
		{`pid = Process.spawn("echo hi"); p Process::Status.wait.pid == pid`, "true\n"},
	} {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("%s\n got %q want %q", tc.src, got, tc.want)
		}
	}
	for _, tc := range []struct{ src, class, msg string }{
		{`Process.wait`, "Errno::ECHILD", "No child processes"},
		{`Process.wait2`, "Errno::ECHILD", "No child processes"},
		{`Process.waitall(0)`, "ArgumentError", "wrong number of arguments (given 1, expected 0)"},
		{`Process.last_status(1)`, "ArgumentError", "wrong number of arguments (given 1, expected 0)"},
	} {
		class, msg := evalErr(t, tc.src)
		if class != tc.class || msg != tc.msg {
			t.Errorf("%s: got %s/%q want %s/%q", tc.src, class, msg, tc.class, tc.msg)
		}
	}
}

// TestProcessExecValidates: Process.exec is reachable with an explicit receiver
// (Kernel#exec alone is private) and rejects a command it cannot run before
// anything is executed.
func TestProcessExecValidates(t *testing.T) {
	fakeSpawn(t, "execed", "", 0)
	for _, tc := range []struct{ src, class, msg string }{
		{`Process.exec([])`, "ArgumentError", "wrong first argument"},
		{`Process.exec("")`, "Errno::ENOENT", "No such file or directory - "},
		{`Process.exec("no-such-program-xyz")`, "Errno::ENOENT", "No such file or directory - no-such-program-xyz"},
		{"Process.exec(\"\\000\")", "ArgumentError", "string contains null byte"},
	} {
		class, msg := evalErr(t, tc.src)
		if class != tc.class || msg != tc.msg {
			t.Errorf("%s: got %s/%q want %s/%q", tc.src, class, msg, tc.class, tc.msg)
		}
	}
	// A command that CAN run unwinds the enclosing fork block with its status.
	if got := eval(t, `pid = Kernel.fork { Process.exec("echo hi") }
p Process.waitpid2(pid)[1].exitstatus`); got != "execed0\n" {
		t.Errorf("Process.exec inside fork: got %q", got)
	}
}

// mustRead asserts a file's exact contents.
func mustRead(t *testing.T, name, want string) {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if string(b) != want {
		t.Errorf("%s: got %q want %q", name, b, want)
	}
}

// envOf turns an environment slice into a map for assertions.
func envOf(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

// quoteRuby renders a path as a Ruby string literal.
func quoteRuby(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }
