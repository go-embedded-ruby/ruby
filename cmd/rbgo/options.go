// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_closed

package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// This file is the front-end's command line, ported from ruby.c.
//
// MRI's contract is `ruby [options] [--] [filepath] [arguments]`, and the rule
// that splits it lives in exactly two places:
//
//   - proc_options (ruby.c:1558 in ruby_4_0) scans argv[1..] and STOPS at the
//     first entry that is not of the form "-X..." — `if (!arg || arg[0] != '-'
//     || !arg[1]) break;`. So a bare word, and "-" by itself, both end option
//     processing and stay in place. "--" is the one option that also stops the
//     scan, and it is consumed (case '-': `if (!s[1]) { argc--, argv++; goto
//     switch_end; }`).
//
//   - process_options (ruby.c:2361) then takes ONE more entry as the script —
//     `if (!opt->e_script) { ... opt->script = argv[0]; argc--; argv++; }` — and
//     hands everything that is left to ruby_set_argv, which fills ARGV.
//
// Three consequences are easy to get wrong and were each measured against the
// oracle (ruby 4.0.5) rather than reasoned about:
//
//   - `ruby s.rb -- -w` puts BOTH "--" and "-w" in ARGV. Options stopped at
//     "s.rb"; the "--" after it is an ordinary argument, not a separator.
//   - `ruby -e 'p ARGV' -w` prints []. -e does not end option processing — the
//     scan resumes at the next entry — so "-w" is still a flag.
//   - with -e there is NO script entry, so everything left after the scan is ARGV.
type options struct {
	eScript   string   // the -e chunks, each terminated by "\n" as proc_e_option does
	haveE     bool     // -e was given: no script entry is taken from argv
	script    string   // the script path; "" (nothing left) and "-" both mean stdin
	argv      []string // what ruby_set_argv would receive
	warnLevel int      // -W<level>: 0 silent, 1 default, >=2 verbose
	warnSet   bool     // a -w or -W actually chose a level
	help      bool     // -h / --help
}

// optError is an option-parsing failure whose Error() is the COMPLETE stderr
// line, because MRI's wording is part of the contract: an unknown switch is an
// uncaught RuntimeError, so the process prints
// "ruby: invalid option -Z  (-h will show valid options) (RuntimeError)" (two
// spaces before the parenthesis) and exits 1 — not a usage summary, and not
// exit 2. Issue #708 was found because rbgo printed its usage text here.
type optError struct{ text string }

func (e *optError) Error() string { return e.text }

// rubyRaise renders what an uncaught RuntimeError from proc_options looks like on
// stderr, with this program's name in place of rb_progname.
func rubyRaise(format string, args ...any) *optError {
	return &optError{text: "rbgo: " + fmt.Sprintf(format, args...) + " (RuntimeError)"}
}

// mriOnlyOptions are switches ruby accepts and rbgo does not implement. They are
// kept apart from the unknown ones deliberately: telling a user that -n is an
// "invalid option" would be a false statement about Ruby, so rbgo says it is
// unimplemented instead and still fails rather than ignoring it.
var mriOnlyOptions = map[byte]string{
	'a': "split each input line into $F", 'p': "loop and print $_",
	'n': "loop over input lines", 'd': "set $DEBUG", 'y': "parser debug output",
	'v': "print the version and set $VERBOSE", 'c': "syntax check only",
	's': "parse switches after the script name", 'l': "line-ending processing",
	'S': "search $PATH for the script", 'r': "require a library first",
	'i': "in-place edit", 'x': "skip to #!ruby", 'C': "chdir first",
	'X': "chdir first", 'F': "set the input field separator",
	'E': "set the external/internal encoding", 'U': "internal encoding UTF-8",
	'K': "1.8 kcode", 'I': "prepend to $LOAD_PATH", '0': "set $/",
}

// mriOnlyLongOptions is the same list for ruby's long switches
// (proc_long_options in ruby_4_0).
var mriOnlyLongOptions = map[string]bool{
	"copyright": true, "crash-report": true, "debug": true, "disable": true,
	"dump": true, "enable": true, "encoding": true, "external-encoding": true,
	"internal-encoding": true, "source-encoding": true, "jit": true,
	"parser": true, "prism": true, "verbose": true, "version": true,
	"yjit": true, "yydebug": true, "zjit": true, "backtrace-limit": true,
}

// warnCategories are proc_W_option's four `-W:name` categories. rbgo accepts the
// spelling and does not yet route the per-category bits, which is why they are
// listed rather than acted on; an unknown name gets MRI's rb_warn, gated on
// $VERBOSE exactly as rb_warn is (so `-W0 -W:bogus` is silent, as measured).
var warnCategories = map[string]bool{
	"deprecated": true, "experimental": true, "performance": true,
	"strict_unused_block": true,
}

// parseOptions is proc_options followed by the script/ARGV split in
// process_options. args is os.Args[1:], i.e. MRI's argv from index 1, which is
// where proc_options' loop begins (`for (argc--, argv++; ...)`).
//
// stderr is where an accepted-but-meaningless `-W:name` warning goes; it is a
// parameter so a test can read it without touching the process's descriptors.
func parseOptions(args []string, stderr io.Writer) (*options, error) {
	o := &options{warnLevel: defaultWarnLevel}
	i := 0
scan:
	for ; i < len(args); i++ {
		arg := args[i]
		// proc_options: the scan ends at the first entry that is not "-X...".
		// len(arg) < 2 covers both "" and "-" (MRI's `!arg[1]`), and "-" must stay
		// in place because it is a script name meaning stdin.
		if len(arg) < 2 || arg[0] != '-' {
			break
		}
		s := arg[1:]
		// MRI's `goto reswitch` chews one letter at a time off a cluster like
		// "-wW0"; a `break` out of its switch abandons the rest of the word and
		// moves to the next entry, which is `break reswitch` here.
	reswitch:
		for len(s) > 0 {
			switch s[0] {
			case 'w':
				// case 'w': ruby_verbose = Qtrue, i.e. -W2's level.
				o.setWarn(2)
				s = s[1:]
			case 'W':
				rest, more, err := o.procW(s, stderr)
				if err != nil {
					return nil, err
				}
				if !more {
					break reswitch
				}
				s = rest
			case 'e':
				// proc_e_option: the code is the rest of the word, or the next
				// entry when the word ends at "-e".
				code := s[1:]
				if code == "" {
					if i+1 >= len(args) {
						return nil, rubyRaise("no code specified for -e")
					}
					i++
					code = args[i]
				}
				o.haveE = true
				o.eScript += code + "\n" // proc_e_option appends a newline per chunk
				break reswitch
			case 'h':
				// case 'h' jumps straight to switch_end: the rest of the command
				// line is never looked at.
				o.help = true
				break scan
			case '-':
				if s == "-" { // the argument was exactly "--"
					i++ // MRI consumes it (argc--, argv++) before switch_end
					break scan
				}
				name := s[1:]
				if eq := strings.IndexByte(name, '='); eq >= 0 {
					name = name[:eq]
				}
				switch {
				case name == "help":
					o.help = true
					break scan
				case mriOnlyLongOptions[name]:
					return nil, unimplemented("--" + name)
				default:
					return nil, rubyRaise("invalid option %s  (-h will show valid options)", arg)
				}
			default:
				c := s[0]
				if _, ok := mriOnlyOptions[c]; ok {
					return nil, unimplemented("-" + string(rune(c)))
				}
				// The default arm prints ONE character, not the rest of the word:
				// rb_enc_precise_mbclen gives the first character's length and the
				// raise uses "-%.*s" with it. `ruby -Zabc` says "invalid option -Z".
				return nil, rubyRaise("invalid option -%s  (-h will show valid options)", firstChar(s))
			}
		}
	}

	if o.help {
		// process_options tests the usage/help dump bit BEFORE it does `argc -= i`,
		// and returns straight away — no script entry is taken and ARGV is never
		// filled. Mirror that: -h answers a question about rbgo, it does not run a
		// program.
		o.argv = nil
		return o, nil
	}

	rest := args[i:]
	// process_options: one entry becomes the script, but only when -e did not
	// already supply the program.
	if !o.haveE && len(rest) > 0 {
		o.script = rest[0]
		rest = rest[1:]
	}
	o.argv = rest
	return o, nil
}

// defaultWarnLevel is $VERBOSE == false, MRI's starting state and what -W1 asks
// for explicitly.
const defaultWarnLevel = 1

// unimplemented reports an option ruby has and rbgo does not. It is a failure,
// not a warning: silently ignoring -n would run the program under semantics the
// user did not ask for.
func unimplemented(opt string) *optError {
	return &optError{text: "rbgo: " + opt + " is a ruby option that rbgo does not implement"}
}

// firstChar returns s's first UTF-8 character (or its first byte when that byte
// does not begin a valid sequence), which is the unit MRI names in the
// invalid-option message.
func firstChar(s string) string {
	for i := 1; i <= len(s); i++ {
		if i == len(s) || (s[i]&0xC0) != 0x80 {
			return s[:i]
		}
	}
	return s
}

// setWarn records a level. proc_options reads opt->warning, which it only commits
// at switch_end, so every -w/-W on one command line takes effect and the LAST one
// wins: `ruby -W0 -w` is verbose and `ruby -w -W0` is silent (both measured).
func (o *options) setWarn(level int) {
	o.warnLevel = level
	o.warnSet = true
}

// procW is proc_W_option. s starts at the 'W'. It returns the unconsumed tail of
// the cluster and whether the scan should continue chewing it (MRI's `goto
// reswitch` versus its `break`).
func (o *options) procW(s string, stderr io.Writer) (rest string, more bool, err error) {
	if len(s) > 1 && s[1] == ':' {
		// -W:name / -W:no-name. proc_W_option returns 0 here, so the rest of the
		// word is never re-scanned.
		name := strings.TrimPrefix(s[2:], "no-")
		if !warnCategories[name] {
			// rb_warn, so it is silent when $VERBOSE is already nil.
			if o.warnLevel > 0 {
				fmt.Fprintf(stderr, "rbgo: warning: unknown warning category: '%s'\n", name)
			}
		}
		return "", false, nil
	}
	// The numeric form. scan_oct(s, 1, &numlen) reads AT MOST ONE octal digit;
	// numlen == 0 leaves v at 2 and leaves the character in place, so "-W9"
	// selects level 2 and then fails on "-9" the way MRI does.
	v := 2
	tail := s[1:]
	if tail != "" && tail[0] >= '0' && tail[0] <= '7' {
		v = int(tail[0] - '0')
		tail = tail[1:]
	}
	o.setWarn(v)
	return tail, true, nil
}

// load resolves the program text and the name it runs under ($0, __FILE__ and the
// label in a backtrace). fromFile says whether the name is a real path, which is
// what decides between SetScriptPath (require_relative resolves against the
// script's directory) and SetScriptName.
func (o *options) load() (src, name string, fromFile bool, err error) {
	if o.haveE {
		// proc_e_option: `if (opt->script == 0) opt->script = "-e";`
		return o.eScript, "-e", false, nil
	}
	if o.script == "" || o.script == "-" {
		// process_options: `if (argc <= 0) opt->script = "-";` — with no script
		// entry left, and with an explicit "-", the program comes from stdin and
		// $0 is "-".
		b, rerr := io.ReadAll(os.Stdin)
		if rerr != nil {
			return "", "", false, rerr
		}
		return string(b), "-", false, nil
	}
	b, rerr := os.ReadFile(o.script)
	if rerr != nil {
		return "", "", false, rerr
	}
	return string(b), o.script, true, nil
}
