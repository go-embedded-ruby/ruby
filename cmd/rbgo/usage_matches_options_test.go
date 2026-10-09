package main

import (
	"io"
	"regexp"
	"testing"
)

// TestUsageNamesEveryOptionTheParserAccepts: `rbgo -h` is the only place a user
// can find out what rbgo takes, and it is a hand-written string, so it drifts
// silently the moment a switch is added. -v and --version shipped with the
// usage text still saying only -w/-W/-e/-h.
//
// The list is DERIVED, not copied: it asks the parser itself which switches it
// accepts, so a test cannot agree with the usage text by repeating it. A switch
// rbgo rejects -- whether unknown (-Z) or MRI-only (-n) -- is not expected to
// appear; this guards the ones that WORK and are undocumented, which is the
// direction that misleads.
func TestUsageNamesEveryOptionTheParserAccepts(t *testing.T) {
	// A program argument is supplied so that a switch is never judged by what
	// the parser does with an empty command line.
	accepts := func(args ...string) bool {
		_, err := parseOptions(append(args, "t.rb"), io.Discard)
		return err == nil
	}
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for _, c := range letters {
		opt := "-" + string(c)
		// -e consumes the next word as code; pass it one so it is judged on
		// acceptance rather than on a missing argument.
		args := []string{opt}
		if c == 'e' {
			args = []string{opt, "1"}
		}
		if !accepts(args...) {
			continue
		}
		if !namedInUsage(opt) {
			t.Errorf("parseOptions accepts %q but the usage text never names it:\n%s", opt, usageText)
		}
	}
	// Long options are not enumerable the way single letters are, so they come
	// from the one list the parser consults -- plus the ones it handles by name.
	long := []string{"help", "version"}
	for name := range mriOnlyLongOptions {
		long = append(long, name)
	}
	for _, name := range long {
		opt := "--" + name
		if !accepts(opt) {
			continue
		}
		if !namedInUsage(opt) {
			t.Errorf("parseOptions accepts %q but the usage text never names it:\n%s", opt, usageText)
		}
	}
}

// namedInUsage reports whether usageText names opt as a switch in its own
// right: not preceded by another dash (so "-v" is not found inside "--version")
// and not continued by a letter or digit (so "-W" is not found inside "-W0",
// which is a different switch to document).
func namedInUsage(opt string) bool {
	return regexp.MustCompile(`(^|[^-\w])` + regexp.QuoteMeta(opt) + `($|[^\w])`).MatchString(usageText)
}
