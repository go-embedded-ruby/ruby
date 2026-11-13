// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Command mriref runs a reference MRI with every bundled gem on the load path,
// so it can serve as the conformance oracle.
//
// ⛔ WHY IT EXISTS, AND WHAT IT COST TO FIND. A pkgx-installed MRI 4.0.6 on a
// developer machine starts with RubyGems absent from its load path:
//
//	`RubyGems' were not loaded.
//	cannot load such file -- rubygems (LoadError)
//
// `net/ftp`, `net/http`, `stringio`, `bigdecimal` and most of the rest have
// been BUNDLED GEMS since Ruby 3.1, so on such an interpreter every ruby/spec
// file that needs one raises LoadError before a single example runs.
//
// Used as an oracle that way it does not error out: it scores a clean ZERO on
// every file. A sweep then reports `control 0 passing, treatment 331 passing,
// +331`, which reads exactly like the implementation under test beating the
// reference. It is the silence of a broken instrument wearing the shape of a
// measurement. The whole of library/net-ftp was compared that way once before
// anyone looked at the interpreter's stderr -- a per-file score cannot tell
// "failed every example" from "never started".
//
// With the gem lib directories on the path the same interpreter scores 459 on
// those 56 files, which is the figure the repository's own dated sweep
// recorded, so the oracle is checkable against something.
//
// Rather than resolve net/ftp's dependency closure by hand (net-protocol,
// timeout, resolv, io-wait, stringio, …) it adds EVERY gem's lib directory,
// which is what RubyGems would have resolved anyway.
//
// Usage, as an interpreter that takes one script -- the shape cmd/specdelta
// and run.sh expect:
//
//	MRIREF_RUBY=/path/to/bin/ruby \
//	MRIREF_GEMS=/path/to/lib/ruby/gems/<abi>/gems \
//	mriref library/net-ftp/acct_spec.rb
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

func main() {
	args, err := buildArgs(os.Getenv("MRIREF_RUBY"), os.Getenv("MRIREF_GEMS"), os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "mriref: %v\n", err)
		os.Exit(2)
	}
	cmd := exec.Command(os.Getenv("MRIREF_RUBY"), args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "mriref: %v\n", err)
		os.Exit(2)
	}
}

// errNoGems is returned when the gem root holds no lib directory at all.
var errNoGems = errors.New("no gem lib directory")

// buildArgs turns a gem root into the -I list, then appends the caller's own
// arguments.
//
// ⛔ IT REFUSES AN EMPTY RESULT. A gem root that yields nothing is not a
// gem-free Ruby, it is the wrong path -- and running anyway would reproduce
// precisely the failure this command exists to prevent: an interpreter that
// cannot load its bundled gems, scoring zero on every file while looking like
// it ran.
func buildArgs(ruby, gems string, rest []string) ([]string, error) {
	if ruby == "" || gems == "" {
		return nil, errors.New("MRIREF_RUBY and MRIREF_GEMS must both be set")
	}
	libs, err := gemLibs(gems)
	if err != nil {
		return nil, err
	}
	if len(libs) == 0 {
		return nil, fmt.Errorf("%w under %s -- refusing to run as if there were none", errNoGems, gems)
	}
	out := make([]string, 0, 2*len(libs)+len(rest))
	for _, l := range libs {
		out = append(out, "-I", l)
	}
	return append(out, rest...), nil
}

// gemLibs lists the lib directory of every gem under root, sorted so a run is
// reproducible.
func gemLibs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var libs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		lib := filepath.Join(root, e.Name(), "lib")
		if st, err := os.Stat(lib); err == nil && st.IsDir() {
			libs = append(libs, lib)
		}
	}
	sort.Strings(libs)
	return libs, nil
}
