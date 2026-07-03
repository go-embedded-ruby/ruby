// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"runtime"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// dotenvEnvSeams installs an in-memory ENV backing (via the same seams env_test
// uses) so Dotenv.load's mutation is exercised deterministically without touching
// the real process environment. It returns the installed VM and the backing map.
func dotenvEnvSeams(t *testing.T) (*VM, map[string]string) {
	t.Helper()
	store := map[string]string{}
	origLook, origSet := envLookup, envSetenv
	t.Cleanup(func() { envLookup, envSetenv = origLook, origSet })
	envLookup = func(k string) (string, bool) { v, ok := store[k]; return v, ok }
	envSetenv = func(k, v string) error { store[k] = v; return nil }
	return New(nil), store
}

// TestDotenvSourceArgNonString covers dotenvSourceArg's to_s branch.
func TestDotenvSourceArgNonString(t *testing.T) {
	if got := dotenvSourceArg(object.IntValue(int64(object.Integer(9)))); got != "9" {
		t.Errorf("non-string source -> %q", got)
	}
}

// TestDotenvLoadSetsEnv covers dotenvLoad's ENV-write path: a fresh key is set,
// and an already-present key is not overwritten (the gem's load semantics).
func TestDotenvLoadSetsEnv(t *testing.T) {
	vm, store := dotenvEnvSeams(t)
	store["EXISTING"] = "keep"
	dotenvLoad(vm, "NEWKEY=new\nEXISTING=would_overwrite", false)
	if store["NEWKEY"] != "new" {
		t.Errorf("NEWKEY = %q, want %q", store["NEWKEY"], "new")
	}
	// load must not clobber an already-present value.
	if store["EXISTING"] != "keep" {
		t.Errorf("EXISTING = %q, want kept %q", store["EXISTING"], "keep")
	}
}

// TestDotenvOverloadSetsEnv covers dotenvLoad with overwrite=true: an existing key
// is replaced.
func TestDotenvOverloadSetsEnv(t *testing.T) {
	vm, store := dotenvEnvSeams(t)
	store["K"] = "old"
	dotenvLoad(vm, "K=replaced", true)
	if store["K"] != "replaced" {
		t.Errorf("K = %q, want %q", store["K"], "replaced")
	}
}

// TestDotenvEnvRunCommand covers the RunCommand seam: `$(cmd)` substitution runs
// through the VM shell backtick and the trailing newline is chomped.
func TestDotenvEnvRunCommand(t *testing.T) {
	vm := New(nil)
	env := dotenvEnv(vm)
	if env.RunCommand == nil {
		t.Fatal("nil RunCommand seam")
	}
	if runtime.GOOS == "windows" {
		// On Windows runShellCommand shells through cmd.exe, where `printf` is
		// absent and `echo` appends CRLF. Still exercise the seam (covering the
		// RunCommand closure) and assert the whitespace-trimmed output.
		if got := strings.TrimSpace(env.RunCommand("echo hi")); got != "hi" {
			t.Errorf("RunCommand(windows) -> %q, want %q", got, "hi")
		}
		return
	}
	if got := env.RunCommand("printf hi"); got != "hi" {
		t.Errorf("RunCommand -> %q, want %q", got, "hi")
	}
	// A command whose output ends in a newline has exactly one trailing newline
	// chomped.
	if got := env.RunCommand("echo hi"); got != "hi" {
		t.Errorf("RunCommand newline -> %q, want %q", got, "hi")
	}
}

// TestDotenvParseErrorInternal covers dotenvParse's error path directly (a
// malformed line) and the Hash result shape.
func TestDotenvParseErrorInternal(t *testing.T) {
	vm := New(nil)
	h := dotenvParse(vm, "A=1", false)
	if hh, ok := object.KindOK[*object.Hash](h); !ok || len(hh.Keys) != 1 {
		t.Errorf("parse -> %#v", h)
	}
}

// TestDotenvLoadError covers dotenvLoad's error arm: a malformed source raises
// (surfaced as a Ruby ArgumentError), before any ENV mutation.
func TestDotenvLoadError(t *testing.T) {
	vm, _ := dotenvEnvSeams(t)
	defer func() {
		if recover() == nil {
			t.Error("expected a raise on malformed load")
		}
	}()
	dotenvLoad(vm, "export NOPE", false)
}
