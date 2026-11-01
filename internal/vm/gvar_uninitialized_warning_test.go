// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"sort"
	"strings"
	"testing"
)

// Reading a global nothing ever assigned warns, and the three things that are
// NOT that read stay silent. variable.c at v4.0.9 gives the mechanism: an id
// nobody has assigned gets rb_gvar_undef_getter installed for it
// (variable.c:649), whose whole body is
//
//	rb_warning("global variable '%"PRIsVALUE"' not initialized", QUOTE_ID(id));
//	return Qnil;
//
// so the subject is "never assigned", not "reads as nil" — and it is rb_warning,
// not rb_warn, so only a TRUE $VERBOSE prints it. language/variables_spec.rb
// asks for it through `complain(verbose: true)` for exactly that reason.
//
// Every row was run through /usr/bin/ruby on the same source before it was
// written here.
func TestUninitializedGlobalWarning(t *testing.T) {
	const probe = warnProbeFile
	cases := []struct {
		name, src, want string
	}{
		{
			name: "never assigned warns and names the variable",
			src:  "$VERBOSE = true\n$specs_uninitialized_global_variable\n",
			want: probe + ":2: warning: global variable '$specs_uninitialized_global_variable' not initialized\n",
		}, {
			// ⛔ The distinction the whole fix turns on. Assigning replaces the
			// undef getter, so the variable is initialised FOREVER after, even
			// though every read of it still answers nil.
			name: "assigned nil is not uninitialized",
			src:  "$VERBOSE = true\n$q = nil\n$q\n",
			want: "",
		}, {
			// The getter is still rb_gvar_undef_getter on the second read, so MRI
			// warns again rather than deduplicating. Measured: two lines.
			name: "each read warns",
			src:  "$VERBOSE = true\n$twice\n$twice\n",
			want: probe + ":2: warning: global variable '$twice' not initialized\n" +
				probe + ":3: warning: global variable '$twice' not initialized\n",
		}, {
			// rb_warning's gate is RTEST(ruby_verbose): a FALSE $VERBOSE is the
			// default and is already silent. This is the row that would catch the
			// warning being wired to rb_warn by mistake.
			name: "silent when $VERBOSE is false",
			src:  "$VERBOSE = false\n$quiet_one\n",
			want: "",
		}, {
			name: "silent when $VERBOSE was never set",
			src:  "$quiet_two\n",
			want: "",
		}, {
			name: "silent when $VERBOSE is nil",
			src:  "$VERBOSE = nil\n$quiet_three\n",
			want: "",
		}, {
			// defined? compiles to its own instruction (OpDefinedGVar), so asking
			// whether a global exists must not report that it does not.
			name: "defined? does not warn",
			src:  "$VERBOSE = true\np defined?($not_there)\n",
			want: "nil\n",
		}, {
			// ⛔ bytecode.ErrinfoCell ("#$!") is the cell begin/rescue reads the
			// pending exception through, not a variable any program can name. The
			// interpreter intercepts it before the read; the two AOT tiers do not,
			// so without the '#' guard in gvarGet every compiled rescue would
			// report an uninitialised global here.
			name: "a rescue body is silent",
			src:  "$VERBOSE = true\nbegin\n  raise \"x\"\nrescue => e\n  e.message\nend\n",
			want: "",
		}, {
			// The match-data family resolves through getters MRI defines, so no
			// match means a nil result, not an uninitialised variable.
			name: "match-data globals with no match are silent",
			src:  "$VERBOSE = true\np [$~, $&, $1, $`, $', $+]\n",
			want: "[nil, nil, nil, nil, nil, nil]\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := runWarn(t, c.src); got != c.want {
				t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
			}
		})
	}
}

// ⛔ THE NINE NAMES AN EARLIER DRAFT OF THIS WARNING JABBERED AT.
//
// Measured, not remembered: the first version of the fix warned for `$;`, `$\`,
// `$,`, `$.`, `$_`, `$?`, `$FILENAME`, `$-F` and `$-i`, because this VM leaves
// them on the ordinary user-global path and the warning asked THIS VM whether a
// name was known. MRI defines all nine in the interpreter, so each has a real
// getter and none of them can be uninitialised; running the same probe through
// /usr/bin/ruby printed nothing for any of them.
//
// "rbgo falls through to the user-global path" and "MRI has no getter" are
// different statements, and only the second licenses a warning. That those nine
// read as nil here is a separate and older divergence, and a warning is the
// wrong channel to report it through: it would turn a diagnostic about the
// PROGRAM's variable into a report about our own coverage.
//
// This walks the whole table rather than the nine, so a name added to it is
// covered the day it is added.
func TestPredefinedGlobalsNeverReportUninitialized(t *testing.T) {
	names := make([]string, 0, len(mriPredefinedGvars))
	for name := range mriPredefinedGvars {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) < 40 {
		t.Fatalf("the predefined table holds %d names, which is too few to be MRI's set", len(names))
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			got := runWarn(t, "$VERBOSE = true\n"+name+"\n")
			if strings.Contains(got, "not initialized") {
				t.Errorf("reading %s reported it uninitialised: %q", name, got)
			}
		})
	}
}

// The control for the test above: it must be able to SEE the thing it looks for.
// A predicate that answered "no warning" for every input would pass that test
// with the fix removed, so this plants a positive in the same harness, with the
// same assertion, and fails if the warning cannot be observed at all.
func TestUninitializedGlobalWarningIsObservable(t *testing.T) {
	got := runWarn(t, "$VERBOSE = true\n$a_name_in_no_table\n")
	if !strings.Contains(got, "not initialized") {
		t.Fatalf("the planted positive produced no warning: %q — the other test's "+
			"silence proves nothing", got)
	}
}
