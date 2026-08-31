package vm

import (
	"math/big"
	"strings"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// trSet is a parsed tr/count/delete/squeeze character selector: a set of member
// characters (each a whole UTF-8 code point, or a single invalid byte) plus a
// negation flag for a leading '^'. It backs the intersection semantics shared by
// String#count, #delete(!) and #squeeze(!). Characters are keyed by their raw
// bytes, so multibyte code points match as a unit and never partially.
type trSet struct {
	in  map[string]bool
	neg bool
}

// charUnits splits s into its character units: each is a whole UTF-8 code point,
// or a single byte for an invalid sequence (so byte-oriented behaviour survives
// on ill-formed input).
func charUnits(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		out = append(out, s[i:i+size])
		i += size
	}
	return out
}

// parseTrList expands a tr-style selector into its ordered member characters and
// a negation flag. It honours "a-z" ranges (expanded by code point) and '\'
// escapes (so "\\^", "\\-" and "\\\\" are literals). When allowNeg is true a
// leading '^' negates membership, unless it is the selector's only character, in
// which case it is literal. A reversed range such as "z-a" raises ArgumentError,
// exactly as MRI does.
func parseTrList(sel string, allowNeg bool) ([]string, bool) {
	b := []byte(sel)
	neg := false
	i := 0
	if allowNeg && len(b) > 1 && b[0] == '^' {
		neg = true
		i = 1
	}
	// readUnit reads one selector character at p, consuming a '\' escape when
	// present; the returned unit is a whole code point (or one invalid byte).
	readUnit := func(p int) (string, int) {
		if b[p] == '\\' && p+1 < len(b) {
			p++
		}
		_, size := utf8.DecodeRune(b[p:])
		return string(b[p : p+size]), p + size
	}
	var out []string
	for i < len(b) {
		lo, next := readUnit(i)
		if next < len(b) && b[next] == '-' && next+1 < len(b) {
			hi, after := readUnit(next + 1)
			loR, _ := utf8.DecodeRuneInString(lo)
			hiR, _ := utf8.DecodeRuneInString(hi)
			if loR > hiR {
				raise("ArgumentError", "invalid range \"%s-%s\" in string transliteration", lo, hi)
			}
			for c := loR; c <= hiR; c++ {
				out = append(out, string(c))
			}
			i = after
			continue
		}
		out = append(out, lo)
		i = next
	}
	return out, neg
}

// newTrSet builds a membership set from a single selector argument.
func newTrSet(sel string) *trSet {
	chars, neg := parseTrList(sel, true)
	ts := &trSet{in: make(map[string]bool, len(chars)), neg: neg}
	for _, c := range chars {
		ts.in[c] = true
	}
	return ts
}

// has reports membership of character unit c, applying negation.
func (t *trSet) has(c string) bool {
	if t.neg {
		return !t.in[c]
	}
	return t.in[c]
}

// trMatchAll reports whether character unit c belongs to every selector — the
// intersection semantics of count/delete/squeeze with multiple arguments.
func trMatchAll(sets []*trSet, c string) bool {
	for _, s := range sets {
		if !s.has(c) {
			return false
		}
	}
	return true
}

// parseTrSets parses each selector argument into a trSet. It raises when no
// selector is given, matching String#count/#delete which require an argument.
func parseTrSets(args []object.Value) []*trSet {
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
	}
	sets := make([]*trSet, len(args))
	for i, a := range args {
		sets[i] = newTrSet(strArg(a))
	}
	return sets
}

// stringCount implements String#count over one or more selectors (intersection).
func stringCount(s string, args []object.Value) int {
	sets := parseTrSets(args)
	n := 0
	for _, c := range charUnits(s) {
		if trMatchAll(sets, c) {
			n++
		}
	}
	return n
}

// stringDelete implements String#delete over one or more selectors: a character
// is removed only when it belongs to every selector.
func stringDelete(s string, args []object.Value) string {
	sets := parseTrSets(args)
	var out strings.Builder
	for _, c := range charUnits(s) {
		if !trMatchAll(sets, c) {
			out.WriteString(c)
		}
	}
	return out.String()
}

// stringSqueeze implements String#squeeze: collapse each run of an identical
// character to one. With no selector every run collapses; with selectors only
// runs of a character in the intersection collapse.
func stringSqueeze(s string, args []object.Value) string {
	var sets []*trSet
	for _, a := range args {
		sets = append(sets, newTrSet(strArg(a)))
	}
	var out strings.Builder
	prev := ""
	first := true
	for _, c := range charUnits(s) {
		if !first && c == prev && (len(sets) == 0 || trMatchAll(sets, c)) {
			continue
		}
		out.WriteString(c)
		prev = c
		first = false
	}
	return out.String()
}

// trString implements String#tr and (when squeeze is true) String#tr_s. The
// from-selector may be negated with a leading '^', in which case every character
// not in the set maps to the last character of to (or is deleted when to is
// empty). For a plain from-selector each source character maps to the to-character
// at the same position, the last to-character repeating when to is shorter; an
// empty to deletes. When a source character appears more than once in from, the
// last occurrence wins. tr_s additionally collapses adjacent runs of an identical
// translated character. All matching is by whole code point, so a multibyte
// character never matches on part of its bytes.
func trString(s, from, to string, squeeze bool) string {
	fromChars, neg := parseTrList(from, true)
	toChars, _ := parseTrList(to, false)

	const del = "\x00del"       // sentinel: delete this character
	repl := map[string]string{} // source char -> replacement char, or del
	var negRepl string          // replacement for the negated case
	negDelete := false
	if neg {
		if len(toChars) > 0 {
			negRepl = toChars[len(toChars)-1]
		} else {
			negDelete = true
		}
	} else {
		for i, c := range fromChars {
			switch {
			case len(toChars) == 0:
				repl[c] = del
			case i < len(toChars):
				repl[c] = toChars[i]
			default:
				repl[c] = toChars[len(toChars)-1]
			}
		}
	}
	inFrom := make(map[string]bool, len(fromChars))
	for _, c := range fromChars {
		inFrom[c] = true
	}

	var out strings.Builder
	lastOut := ""
	lastTranslated := false
	for _, c := range charUnits(s) {
		var r string
		translate := false
		if neg {
			if !inFrom[c] {
				if negDelete {
					lastTranslated = false
					continue
				}
				r, translate = negRepl, true
			}
		} else if v, ok := repl[c]; ok {
			if v == del {
				lastTranslated = false
				continue
			}
			r, translate = v, true
		}
		if !translate {
			out.WriteString(c)
			lastTranslated = false
			continue
		}
		if squeeze && lastTranslated && lastOut == r {
			continue
		}
		out.WriteString(r)
		lastOut = r
		lastTranslated = true
	}
	return out.String()
}

// stringChr returns the first character (a whole UTF-8 code point, or the first
// byte of an invalid sequence) as a new String, or "" for an empty receiver.
func stringChr(s string) string {
	if s == "" {
		return ""
	}
	_, size := utf8.DecodeRuneInString(s)
	return s[:size]
}

// stringSum returns the sum of the receiver's bytes, truncated to the low `bits`
// bits when bits is positive (the default is 16); bits <= 0 returns the full sum.
func stringSum(s string, bits int) int64 {
	var total int64
	for i := 0; i < len(s); i++ {
		total += int64(s[i])
	}
	if bits > 0 {
		total &= (int64(1) << uint(bits)) - 1
	}
	return total
}

// allDigits reports whether s is non-empty and consists solely of ASCII digits;
// String#upto walks such string pairs numerically.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// leftPadZero pads s with leading '0' up to width runes (no-op when already wide
// enough), used to preserve the beginning string's width in numeric #upto.
func leftPadZero(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat("0", width-len(s)) + s
}

// stringUpto drives String#upto: it yields each successive string from beg to
// end inclusive (exclusive when excl is true). When both endpoints are all-digit
// it iterates numerically, preserving beg's width; otherwise it walks String#succ
// results, stopping when the current value passes end.
func stringUpto(beg, end string, excl bool, yield func(string)) {
	if allDigits(beg) && allDigits(end) {
		hi, _ := new(big.Int).SetString(end, 10)
		cur, _ := new(big.Int).SetString(beg, 10)
		width := len(beg)
		one := big.NewInt(1)
		for {
			if excl {
				if cur.Cmp(hi) >= 0 {
					break
				}
			} else if cur.Cmp(hi) > 0 {
				break
			}
			yield(leftPadZero(cur.String(), width))
			cur.Add(cur, one)
		}
		return
	}
	if beg > end {
		return
	}
	afterEnd := succString(end)
	current := beg
	for current != afterEnd {
		if excl && current == end {
			break
		}
		yield(current)
		if !excl && current == end {
			break
		}
		current = succString(current)
		if len(current) > len(end) || len(current) == 0 {
			break
		}
	}
}
