package vm

import (
	"math/big"
	"strings"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// trSet is a parsed tr/count/delete/squeeze character selector: a 256-entry
// byte-membership table plus a negation flag for a leading '^'. It backs the
// intersection semantics shared by String#count, #delete(!) and #squeeze(!).
type trSet struct {
	in  [256]bool
	neg bool
}

// parseTrList expands a tr-style selector into its ordered member bytes and a
// negation flag. It honours "a-z" ranges and '\' escapes (so "\\^", "\\-" and
// "\\\\" are literals). When allowNeg is true a leading '^' negates membership,
// unless it is the selector's only character, in which case it is literal. A
// reversed range such as "z-a" raises ArgumentError exactly as MRI does.
func parseTrList(sel string, allowNeg bool) ([]byte, bool) {
	b := []byte(sel)
	neg := false
	i := 0
	if allowNeg && len(b) > 1 && b[0] == '^' {
		neg = true
		i = 1
	}
	// readByte reads one selector byte at p, consuming a '\' escape when present.
	readByte := func(p int) (byte, int) {
		if b[p] == '\\' && p+1 < len(b) {
			return b[p+1], p + 2
		}
		return b[p], p + 1
	}
	var out []byte
	for i < len(b) {
		lo, next := readByte(i)
		if next < len(b) && b[next] == '-' && next+1 < len(b) {
			hi, after := readByte(next + 1)
			if lo > hi {
				raise("ArgumentError", "invalid range \"%c-%c\" in string transliteration", lo, hi)
			}
			for c := int(lo); c <= int(hi); c++ {
				out = append(out, byte(c))
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
	ts := &trSet{neg: neg}
	for _, c := range chars {
		ts.in[c] = true
	}
	return ts
}

// has reports membership, applying negation.
func (t *trSet) has(b byte) bool {
	if t.neg {
		return !t.in[b]
	}
	return t.in[b]
}

// trMatchAll reports whether b belongs to every selector — the intersection
// semantics of count/delete/squeeze with multiple arguments.
func trMatchAll(sets []*trSet, b byte) bool {
	for _, s := range sets {
		if !s.has(b) {
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
	for i := 0; i < len(s); i++ {
		if trMatchAll(sets, s[i]) {
			n++
		}
	}
	return n
}

// stringDelete implements String#delete over one or more selectors: a byte is
// removed only when it belongs to every selector.
func stringDelete(s string, args []object.Value) string {
	sets := parseTrSets(args)
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if !trMatchAll(sets, s[i]) {
			out = append(out, s[i])
		}
	}
	return string(out)
}

// stringSqueeze implements String#squeeze: collapse each run of an identical
// byte to one. With no selector every run collapses; with selectors only runs
// of a byte in the intersection collapse.
func stringSqueeze(s string, args []object.Value) string {
	var sets []*trSet
	for _, a := range args {
		sets = append(sets, newTrSet(strArg(a)))
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if i > 0 && s[i] == s[i-1] && (len(sets) == 0 || trMatchAll(sets, s[i])) {
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}

// trString implements String#tr and (when squeeze is true) String#tr_s. The
// from-selector may be negated with a leading '^', in which case every byte not
// in the set maps to the last byte of to (or is deleted when to is empty). For a
// plain from-selector each source byte maps to the to-byte at the same position,
// the last to-byte repeating when to is shorter; an empty to deletes. When a
// source byte appears more than once in from, the last occurrence wins. tr_s
// additionally collapses adjacent runs of an identical translated byte.
func trString(s, from, to string, squeeze bool) string {
	fromChars, neg := parseTrList(from, true)
	toChars, _ := parseTrList(to, false)

	const unchanged = -2
	const del = -1
	var repl [256]int
	for i := range repl {
		repl[i] = unchanged
	}
	if neg {
		last := del
		if len(toChars) > 0 {
			last = int(toChars[len(toChars)-1])
		}
		var inFrom [256]bool
		for _, c := range fromChars {
			inFrom[c] = true
		}
		for c := 0; c < 256; c++ {
			if !inFrom[c] {
				repl[c] = last
			}
		}
	} else {
		for i, c := range fromChars {
			switch {
			case len(toChars) == 0:
				repl[c] = del
			case i < len(toChars):
				repl[c] = int(toChars[i])
			default:
				repl[c] = int(toChars[len(toChars)-1])
			}
		}
	}

	out := make([]byte, 0, len(s))
	lastOut := -1
	lastTranslated := false
	for i := 0; i < len(s); i++ {
		r := repl[s[i]]
		if r == unchanged {
			out = append(out, s[i])
			lastTranslated = false
			continue
		}
		if r == del {
			lastTranslated = false
			continue
		}
		if squeeze && lastTranslated && lastOut == r {
			continue
		}
		out = append(out, byte(r))
		lastOut = r
		lastTranslated = true
	}
	return string(out)
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
