package vm

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
	onig "github.com/go-ruby-regexp/regexp"
)

// Regexp is a compiled Ruby regular expression. It wraps the pure-Go go-ruby-regexp
// engine so the interpreter stays CGO-free. flags holds the subset of the flag
// letters i, m, x that were present on the literal, in that canonical order.
//
// Byte-vs-character offsets: go-ruby-regexp reports BYTE offsets, but Ruby's
// MatchData#begin/#end and String#=~ report CHARACTER offsets. The conversion
// happens in this package (byteToChar); matched substrings are
// representation-independent and are returned verbatim.
type Regexp struct {
	re     *onig.Regexp
	source string
	flags  string
	// frozen is set on Regexps produced from a source literal (/…/, %r{…}),
	// which Ruby 3.0+ freezes; Object#frozen? reports it. Runtime constructions
	// (Regexp.new / Regexp.compile) leave it false, matching MRI.
	frozen bool
	// fixedEnc / noEnc record the FIXEDENCODING (Regexp::FIXEDENCODING, 16) and
	// NOENCODING (Regexp::NOENCODING, 32) option bits. They are reflected by
	// #options, #encoding and #fixed_encoding? but do not change matching (the
	// engine matches by UTF-8 code point or raw byte per its own encoding).
	fixedEnc bool
	noEnc    bool
	// timeout is the per-Regexp match time limit as a Float (seconds), or nil
	// when none was requested. It is reported by #timeout and, when set, applied
	// to the direct match paths via the engine's WithTimeout.
	timeout object.Value
	// srcEnc is the encoding of the String a runtime Regexp.new was built from,
	// which #encoding reports when the source has a non-ASCII byte (e.g. a Regexp
	// built from a EUC-JP or BINARY String keeps that encoding). Empty for a source
	// literal, whose non-ASCII source is UTF-8 by default.
	srcEnc string
	// nameMap is set only when the source has capture-group names the engine
	// cannot compile as written — duplicate names (Ruby allows several groups to
	// share a name) or names with non-ASCII characters. compileRegexp then rewrites
	// each named group to a unique ASCII synthetic name the engine accepts and
	// records, per original name, the synthetic names of its groups in source
	// order. Name resolution (MatchData#[], #begin, #named_captures, …) goes through
	// this map. It is nil for the common case, whose names the engine handles
	// directly, leaving that path unchanged.
	nameMap map[string][]string
}

// matcher returns the engine Regexp to match with: the receiver's compiled
// program, wrapped with the per-Regexp timeout when one was requested (the copy
// shares the heavy matcher state, so this is cheap).
func (r *Regexp) matcher() *onig.Regexp {
	if f, ok := r.timeout.(object.Float); ok {
		return r.re.WithTimeout(time.Duration(float64(f) * float64(time.Second)))
	}
	return r.re
}

// optionBits returns the Integer option mask MRI's Regexp#options exposes:
// IGNORECASE|EXTENDED|MULTILINE from the i/m/x flag letters, plus the
// FIXEDENCODING|NOENCODING bits from the encoding options.
func (r *Regexp) optionBits() int64 {
	var bits int64
	if strings.ContainsRune(r.flags, 'i') {
		bits |= reIgnoreCase
	}
	if strings.ContainsRune(r.flags, 'x') {
		bits |= reExtended
	}
	if strings.ContainsRune(r.flags, 'm') {
		bits |= reMultiline
	}
	if r.fixedEnc {
		bits |= reFixedEncoding
	}
	if r.noEnc {
		bits |= reNoEncoding
	}
	return bits
}

// escapeForwardSlashes backslash-escapes each unescaped '/' in a regexp source
// for #to_s / #inspect, the way MRI renders it, without double-escaping a '/'
// that is already preceded by a backslash (a backslash escapes the next byte).
func escapeForwardSlashes(src string) string {
	var b strings.Builder
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c == '\\' && i+1 < len(src) {
			b.WriteByte(c)
			b.WriteByte(src[i+1])
			i++
			continue
		}
		if c == '/' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}

// encodingName returns the canonical name of the encoding Regexp#encoding
// reports: an ASCII-only source is US-ASCII; a source with a non-ASCII byte is
// UTF-8, or ASCII-8BIT (BINARY) when the NOENCODING option is set.
func (r *Regexp) encodingName() string {
	if asciiOnly([]byte(r.source)) {
		return "US-ASCII"
	}
	if r.noEnc {
		return "ASCII-8BIT"
	}
	if r.srcEnc != "" {
		return r.srcEnc
	}
	return "UTF-8"
}

// isFixedEncoding backs Regexp#fixed_encoding?: true when the FIXEDENCODING
// option was requested or the source is tied to a concrete non-ASCII encoding (a
// non-ASCII source that is not the encoding-agnostic BINARY form).
func (r *Regexp) isFixedEncoding() bool {
	if r.fixedEnc {
		return true
	}
	return !asciiOnly([]byte(r.source)) && !r.noEnc
}

func (r *Regexp) ToS() string {
	// Ruby's Regexp#to_s renders the (?on-off:src) form, where the on-set is the
	// present flags and the off-set is the absent ones, always in m, i, x order.
	// When the whole pattern is a single option/non-capturing group spanning the
	// entire source, MRI hoists that group's options into the outer form and drops
	// the wrapping group, e.g. /(?i:.)/ => "(?i-mx:.)" and /(?:x)/ => "(?-mix:x)".
	base := r.flags
	src := r.source
	if gon, goff, body, ok := singleWholeGroup(src); ok {
		src = body
		base = mergeInlineFlags(r.flags, gon, goff)
	}
	on := orderFlags(base)
	off := ""
	for _, f := range "mix" {
		if !strings.ContainsRune(base, f) {
			off += string(f)
		}
	}
	if off != "" {
		off = "-" + off
	}
	return "(?" + on + off + ":" + escapeForwardSlashes(src) + ")"
}

// singleWholeGroup reports whether src is exactly one option or non-capturing
// group — `(?flags:…)`, `(?flags-flags:…)` or `(?:…)` — whose closing paren is
// the last character, returning the group's on/off inline flag letters and its
// body. Named groups, look-around and capturing groups do not qualify (MRI keeps
// those wrapped), and a group that closes before the end (so more of the pattern
// follows) does not span the whole source.
func singleWholeGroup(src string) (on, off, body string, ok bool) {
	if !strings.HasPrefix(src, "(?") {
		return "", "", "", false
	}
	i := 2
	for i < len(src) && (src[i] == 'm' || src[i] == 'i' || src[i] == 'x') {
		on += string(src[i])
		i++
	}
	if i < len(src) && src[i] == '-' {
		i++
		for i < len(src) && (src[i] == 'm' || src[i] == 'i' || src[i] == 'x') {
			off += string(src[i])
			i++
		}
	}
	if i >= len(src) || src[i] != ':' {
		return "", "", "", false
	}
	bodyStart := i + 1
	depth, inClass := 1, false
	for j := bodyStart; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++ // skip the escaped byte
		case '[':
			if !inClass {
				inClass = true
			}
		case ']':
			inClass = false
		case '(':
			if !inClass {
				depth++
			}
		case ')':
			if !inClass {
				depth--
				if depth == 0 {
					// Spans the whole source only if this is the final byte.
					if j == len(src)-1 {
						return on, off, src[bodyStart:j], true
					}
					return "", "", "", false
				}
			}
		}
	}
	return "", "", "", false
}

// mergeInlineFlags folds a hoisted group's inline on/off flag letters into the
// Regexp's own flags: a flag is on when the outer flags or the group's on-set
// enable it and the group's off-set does not disable it.
func mergeInlineFlags(outer, on, off string) string {
	out := ""
	for _, f := range "mix" {
		enabled := strings.ContainsRune(outer, f) || strings.ContainsRune(on, f)
		if enabled && !strings.ContainsRune(off, f) {
			out += string(f)
		}
	}
	return out
}

// Inspect renders /source/flags, escaping unescaped '/' in the source.
func (r *Regexp) Inspect() string {
	return "/" + escapeForwardSlashes(r.source) + "/" + orderFlags(r.flags)
}
func (r *Regexp) Truthy() bool { return true }

// orderFlags returns the present flags in Ruby's canonical m, i, x order.
func orderFlags(flags string) string {
	out := ""
	for _, f := range "mix" {
		if strings.ContainsRune(flags, f) {
			out += string(f)
		}
	}
	return out
}

// MatchData is the result of a successful match: it wraps the go-ruby-regexp
// MatchData and remembers the subject string and source Regexp (for named
// captures and offset conversion).
type MatchData struct {
	md      *onig.MatchData
	subject string
	re      *Regexp
	// byteOff is the byte position in subject where matching began (for
	// Regexp#match(str, pos) / String#match(re, pos)). The engine matched the
	// subject[byteOff:] tail, so every byte offset it reports is shifted by this
	// to land in the full subject. Zero for an ordinary whole-subject match.
	byteOff int
}

func (m *MatchData) ToS() string     { return m.md.Str(0) }
func (m *MatchData) Inspect() string { return "#<MatchData " + matchDataInspect(m) + ">" }
func (m *MatchData) Truthy() bool    { return true }

// matchDataInspect renders the body of MatchData#inspect: the whole match
// inspected, then each group as ` i:capture`. When the pattern has any named
// group, MRI shows ONLY the named groups (by name) and omits the unnamed
// numbered ones; otherwise it shows every numbered group.
func matchDataInspect(m *MatchData) string {
	var b strings.Builder
	b.WriteString(object.NewString(m.md.Str(0)).Inspect())
	idxToName := indexToName(m)
	hasNames := len(idxToName) > 0
	for i := 1; i <= m.md.NGroups(); i++ {
		nm, named := idxToName[i]
		if hasNames && !named {
			continue
		}
		b.WriteByte(' ')
		if named {
			b.WriteString(nm)
		} else {
			b.WriteString(strconv.Itoa(i))
		}
		b.WriteByte(':')
		b.WriteString(groupValue(m, i).Inspect())
	}
	return b.String()
}

// indexToName maps each named-capture group's index to its name, using the
// engine's own name→index resolution over the names parsed from the source.
func indexToName(m *MatchData) map[int]string {
	out := map[int]string{}
	for _, name := range namedGroups(m.re.source) {
		if i := m.indexOfName(name); i >= 0 {
			out[i] = name
		}
	}
	return out
}

// compileRegexp builds a Regexp value from a literal's source and flag letters,
// translating the Ruby flags to an inline (?imx) prefix that go-ruby-regexp accepts.
func (vm *VM) compileRegexp(source, flags string) object.Value {
	prefix := ""
	if imx := sortIMX(flags); imx != "" {
		// Translate the Ruby flags into an inline (?imx) prefix the engine accepts.
		prefix = "(?" + imx + ")"
	}
	// The engine does not accept Ruby's \uHHHH / \u{…} escapes, duplicate capture
	// names, or non-ASCII capture names. Translate the escapes to literal
	// characters and, when needed, rewrite named groups to synthetic ASCII names —
	// both no-ops for sources that do not use those features.
	engineSrc := translateUnicodeEscapes(prefix + source)
	engineSrc, nameMap := rewriteNamedGroups(engineSrc)
	re, err := onig.Compile(engineSrc)
	if err != nil {
		raise("RegexpError", "%s: /%s/", err.Error(), source)
	}
	return &Regexp{re: re, source: source, flags: flags, nameMap: nameMap}
}

// translateUnicodeEscapes rewrites Ruby's \uHHHH and \u{codepoint …} escapes into
// the literal characters they denote, which the engine matches directly (it has no
// \u escape of its own). A code point that is an ASCII metacharacter is emitted
// backslash-escaped so it keeps its literal meaning; other characters are emitted
// verbatim (raw UTF-8 for non-ASCII). Any \u that is not a well-formed escape, and
// every other escape sequence, is copied through untouched. Sources without \u are
// returned unchanged.
func translateUnicodeEscapes(src string) string {
	if !strings.Contains(src, `\u`) {
		return src
	}
	var b strings.Builder
	for i := 0; i < len(src); {
		if src[i] != '\\' || i+1 >= len(src) {
			b.WriteByte(src[i])
			i++
			continue
		}
		if src[i+1] != 'u' {
			// Some other escape (\\, \(, …): copy both bytes so the second is never
			// mistaken for the start of a fresh escape.
			b.WriteByte(src[i])
			b.WriteByte(src[i+1])
			i += 2
			continue
		}
		if i+2 < len(src) && src[i+2] == '{' {
			if end := strings.IndexByte(src[i+3:], '}'); end >= 0 {
				if runes, ok := parseUnicodeBraceBody(src[i+3 : i+3+end]); ok {
					for _, r := range runes {
						emitLiteralRune(&b, r)
					}
					i += 3 + end + 1
					continue
				}
			}
			// Unterminated or malformed \u{…}: copy the \u through and rescan.
			b.WriteByte(src[i])
			b.WriteByte(src[i+1])
			i += 2
			continue
		}
		if i+6 <= len(src) {
			if r, ok := parseHexRune(src[i+2 : i+6]); ok {
				emitLiteralRune(&b, r)
				i += 6
				continue
			}
		}
		// \u without four hex digits and without a brace: leave it for the engine.
		b.WriteByte(src[i])
		b.WriteByte(src[i+1])
		i += 2
	}
	return b.String()
}

// parseUnicodeBraceBody parses the body of a \u{…} escape: whitespace-separated
// runs of 1–6 hex digits, each a code point. It returns ok=false if any run is not
// valid hex or is out of the Unicode range.
func parseUnicodeBraceBody(body string) ([]rune, bool) {
	var runes []rune
	for _, tok := range strings.Fields(body) {
		r, ok := parseHexRune(tok)
		if !ok {
			return nil, false
		}
		runes = append(runes, r)
	}
	return runes, true
}

// parseHexRune parses 1–6 hex digits as a code point, rejecting an empty or
// over-long string, a non-hex digit, or a value past U+10FFFF.
func parseHexRune(s string) (rune, bool) {
	if len(s) == 0 || len(s) > 6 {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 16, 32)
	if err != nil || v > 0x10FFFF {
		return 0, false
	}
	return rune(v), true
}

// emitLiteralRune writes r to b as a regex fragment that matches exactly that
// character: ASCII letters and digits verbatim, other printable ASCII backslash-
// escaped (so a metacharacter like '.' stays literal), ASCII controls and space as
// a \xHH byte escape, and non-ASCII as raw UTF-8.
func emitLiteralRune(b *strings.Builder, r rune) {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		b.WriteByte(byte(r))
	case r >= 0x21 && r <= 0x7e:
		b.WriteByte('\\')
		b.WriteByte(byte(r))
	case r < 0x80:
		const hexDigits = "0123456789ABCDEF"
		b.WriteString(`\x`)
		b.WriteByte(hexDigits[r>>4])
		b.WriteByte(hexDigits[r&0xf])
	default:
		b.WriteRune(r)
	}
}

// rewriteNamedGroups makes a source the engine can compile when it has capture
// names the engine rejects: duplicate names or non-ASCII names. It renames every
// named group to a unique ASCII synthetic name and rewrites \k / \g name
// references to match, returning the rewritten source and a map from each original
// name to its groups' synthetic names in source order. When no such name is
// present it returns the source unchanged and a nil map, so ordinary patterns take
// the engine's own name handling. A gated pattern never compiled before, so any
// handling here is strictly an improvement over the previous hard failure.
func rewriteNamedGroups(src string) (string, map[string][]string) {
	if !needsNameRewrite(namedGroups(src)) {
		return src, nil
	}
	nameMap := map[string][]string{}
	var b strings.Builder
	counter := 0
	inClass := false
	classFirst := false
	for i := 0; i < len(src); {
		c := src[i]
		if c == '\\' && i+1 < len(src) {
			// A \k<name>/\k'name' backreference or \g<name>/\g'name' subroutine call
			// to a renamed group must be rewritten too; numbered references, and any
			// name not (yet) defined, are left alone. Escapes inside a class are never
			// references, so only rewrite outside a class.
			if !inClass && (src[i+1] == 'k' || src[i+1] == 'g') && i+2 < len(src) {
				var closeCh byte
				switch src[i+2] {
				case '<':
					closeCh = '>'
				case '\'':
					closeCh = '\''
				}
				if closeCh != 0 {
					j := i + 3
					for j < len(src) && src[j] != closeCh {
						j++
					}
					if j < len(src) {
						if syns, ok := nameMap[src[i+3:j]]; ok {
							b.WriteByte('\\')
							b.WriteByte(src[i+1])
							b.WriteByte(src[i+2])
							b.WriteString(syns[len(syns)-1])
							b.WriteByte(closeCh)
							i = j + 1
							continue
						}
					}
				}
			}
			b.WriteByte(c)
			b.WriteByte(src[i+1])
			classFirst = false
			i += 2
			continue
		}
		if inClass {
			b.WriteByte(c)
			if c == ']' && !classFirst {
				inClass = false
			}
			classFirst = false
			i++
			continue
		}
		if c == '[' {
			b.WriteByte(c)
			i++
			inClass = true
			classFirst = true
			if i < len(src) && src[i] == '^' {
				b.WriteByte('^')
				i++
			}
			continue
		}
		if c == '(' && i+3 < len(src) && src[i+1] == '?' && src[i+2] == '<' &&
			src[i+3] != '=' && src[i+3] != '!' {
			j := i + 3
			for j < len(src) && src[j] != '>' {
				j++
			}
			if j < len(src) {
				counter++
				syn := syntheticName(counter)
				name := src[i+3 : j]
				nameMap[name] = append(nameMap[name], syn)
				b.WriteString("(?<")
				b.WriteString(syn)
				b.WriteByte('>')
				i = j + 1
				continue
			}
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), nameMap
}

// needsNameRewrite reports whether the parsed capture names force a rewrite: the
// engine rejects a name that repeats or that carries a non-ASCII byte.
func needsNameRewrite(names []string) bool {
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			return true
		}
		seen[n] = true
		for i := 0; i < len(n); i++ {
			if n[i] >= 0x80 {
				return true
			}
		}
	}
	return false
}

// syntheticName is the nth synthetic capture name. Every original name is renamed,
// so these ASCII names cannot collide with a name the source still uses.
func syntheticName(n int) string {
	return "g" + strconv.Itoa(n)
}

// compileLiteralRegexp compiles a Regexp for a source-literal occurrence and
// marks it frozen (Ruby 3.0+ freezes regexp literals). Callers memoise the
// result per literal occurrence so repeated evaluation returns the same object.
func (vm *VM) compileLiteralRegexp(source, flags string) object.Value {
	r := vm.compileRegexp(source, flags)
	r.(*Regexp).frozen = true
	return r
}

// Regexp option bits, matching MRI's Regexp::IGNORECASE/EXTENDED/MULTILINE and
// the encoding options FIXEDENCODING/NOENCODING.
const (
	reIgnoreCase    = 1
	reExtended      = 2
	reMultiline     = 4
	reFixedEncoding = 16
	reNoEncoding    = 32
)

// regexpNew backs Regexp.new / Regexp.compile. The first argument is either a
// Regexp (copied, reusing its options) or a String (compiled). When the first
// argument is a String, the optional second argument selects options: an
// Integer is decoded bitwise (IGNORECASE/EXTENDED/MULTILINE), a String is read
// as option letters (i/m/x), nil/false select no options and any other truthy
// value selects IGNORECASE (the legacy form MRI still accepts).
func (vm *VM) regexpNew(args []object.Value) object.Value {
	// Peel a trailing keyword Hash (the only keyword MRI accepts here is
	// timeout:); the remaining positional arguments are the source and options.
	var timeout object.Value = object.NilV
	if h := regexpKwHash(args); h != nil {
		if t, ok := h.Get(object.Symbol("timeout")); ok {
			timeout = coerceTimeout(t)
		}
		args = args[:len(args)-1]
	}
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1..3)")
	}
	switch src := args[0].(type) {
	case *Regexp:
		// Copy the source Regexp; MRI warns when options are also given but still
		// reuses the original's options, so we ignore any extra arguments here.
		r := vm.compileRegexp(src.source, src.flags).(*Regexp)
		r.fixedEnc, r.noEnc = src.fixedEnc, src.noEnc
		if _, ok := timeout.(object.Float); ok {
			r.timeout = timeout
		} else {
			r.timeout = src.timeout
		}
		return r
	case *object.String:
		return vm.regexpFromString(src, args, timeout)
	default:
		// A non-String, non-Regexp source is coerced with #to_str, matching MRI.
		if vm.respondsToDynamic(args[0], "to_str") {
			conv := vm.send(args[0], "to_str", nil, nil)
			s, ok := conv.(*object.String)
			if !ok {
				raise("TypeError", "can't convert %s into String (%s#to_str gives %s)",
					classNameOf(args[0]), classNameOf(args[0]), classNameOf(conv))
			}
			return vm.regexpFromString(s, args, timeout)
		}
		raise("TypeError", "no implicit conversion of %s into String", classNameOf(args[0]))
		return object.NilVal()
	}
}

// regexpFromString builds a Regexp from a String source and the optional second
// (options) argument, tagging the FIXEDENCODING/NOENCODING bits, timeout and the
// source encoding. Shared by the direct String path and the #to_str-coerced path.
func (vm *VM) regexpFromString(src *object.String, args []object.Value, timeout object.Value) object.Value {
	flags, fixedEnc, noEnc := "", false, false
	if len(args) >= 2 {
		flags = regexpOptionFlags(args[1])
		fixedEnc, noEnc = regexpEncodingBits(args[1])
	}
	r := vm.compileRegexp(src.Str(), flags).(*Regexp)
	r.fixedEnc, r.noEnc, r.timeout = fixedEnc, noEnc, timeout
	r.srcEnc = src.EncName()
	return r
}

// regexpKwHash returns the trailing keyword Hash of a Regexp.new argument list,
// or nil when the last argument is not a Hash (positional options).
func regexpKwHash(args []object.Value) *object.Hash {
	if len(args) == 0 {
		return nil
	}
	h, _ := args[len(args)-1].(*object.Hash)
	return h
}

// coerceTimeout converts a timeout: keyword value into the Float (seconds) that
// #timeout reports: an Integer or Float becomes a Float, nil stays nil, and any
// other type raises TypeError as MRI does.
func coerceTimeout(v object.Value) object.Value {
	switch t := v.(type) {
	case object.Nil:
		return object.NilV
	case object.Integer:
		return object.Float(float64(t))
	case object.Float:
		return t
	default:
		raise("TypeError", "no implicit conversion to float from %s", classNameOf(v))
		return object.NilV
	}
}

// regexpEncodingBits reports whether the FIXEDENCODING / NOENCODING option bits
// are set in an Integer options argument (they are meaningless for the String
// option-letter form, which returns false, false).
func regexpEncodingBits(v object.Value) (fixed, no bool) {
	if bits, ok := v.(object.Integer); ok {
		return int(bits)&reFixedEncoding != 0, int(bits)&reNoEncoding != 0
	}
	return false, false
}

// regexpOptionFlags converts the second argument of Regexp.new into the engine's
// "imx" flag-letter string.
func regexpOptionFlags(v object.Value) string {
	switch opt := v.(type) {
	case object.Nil:
		return ""
	case object.Integer:
		return flagsFromBits(int(opt))
	case *object.String:
		return flagsFromLetters(opt.Str())
	default:
		// nil/false → none; any other truthy value → IGNORECASE (legacy form).
		if !v.Truthy() {
			return ""
		}
		return "i"
	}
}

// flagsFromBits decodes an Integer option mask into i/m/x flag letters. Bits
// other than IGNORECASE/EXTENDED/MULTILINE (e.g. encoding bits) are ignored.
func flagsFromBits(bits int) string {
	out := ""
	if bits&reIgnoreCase != 0 {
		out += "i"
	}
	if bits&reMultiline != 0 {
		out += "m"
	}
	if bits&reExtended != 0 {
		out += "x"
	}
	return out
}

// flagsFromLetters reads a String option argument as MRI does: each character is
// an option letter (i/m/x). An unrecognised letter raises ArgumentError.
func flagsFromLetters(s string) string {
	out := ""
	for _, c := range s {
		switch c {
		case 'i':
			if !strings.ContainsRune(out, 'i') {
				out += "i"
			}
		case 'm':
			if !strings.ContainsRune(out, 'm') {
				out += "m"
			}
		case 'x':
			if !strings.ContainsRune(out, 'x') {
				out += "x"
			}
		default:
			raise("ArgumentError", "unknown regexp option: %s", s)
		}
	}
	return out
}

// sortIMX returns the present flags as i, m, x (the order Ruby prints them when
// constructing the inline group; only presence matters to the engine).
func sortIMX(flags string) string {
	out := ""
	for _, f := range "imx" {
		if strings.ContainsRune(flags, f) {
			out += string(f)
		}
	}
	return out
}

// strMatchRegexp coerces the argument of String#match / String#match? into a
// Regexp: a Regexp passes through; a String is compiled (no flags); anything
// else raises TypeError.
func strMatchRegexp(v object.Value) *Regexp {
	switch x := v.(type) {
	case *Regexp:
		return x
	case *object.String:
		re, err := onig.Compile(x.Str())
		if err != nil {
			raise("RegexpError", "%s: /%s/", err.Error(), x.Str())
		}
		return &Regexp{re: re, source: x.Str()}
	default:
		raise("TypeError", "wrong argument type %s (expected Regexp)", classNameOf(v))
		return nil
	}
}

// runMatch matches re against subject, returning a MatchData value or nil. It
// also records the result as $~ (the last match).
func (vm *VM) runMatch(re *Regexp, subject string) object.Value {
	md := re.matcher().Match(subject)
	if md == nil {
		vm.lastMatch = object.NilV
		return object.NilV
	}
	m := &MatchData{md: md, subject: subject, re: re}
	vm.lastMatch = m
	return m
}

// runMatchFrom matches re against subject starting at character offset pos, the
// way Regexp#match(str, pos) / String#match(re, pos) do. The engine matches the
// byte tail at that offset and the MatchData records the byte offset so its
// positions report against the full subject. A negative pos counts from the end;
// an out-of-range pos yields no match.
func (vm *VM) runMatchFrom(re *Regexp, subject string, pos int64) object.Value {
	nChars := int64(utf8.RuneCountInString(subject))
	if pos < 0 {
		pos += nChars
	}
	if pos < 0 || pos > nChars {
		vm.lastMatch = object.NilV
		return object.NilV
	}
	byteOff := charToByte(subject, int(pos))
	md := re.matcher().Match(subject[byteOff:])
	if md == nil {
		vm.lastMatch = object.NilV
		return object.NilV
	}
	m := &MatchData{md: md, subject: subject, re: re, byteOff: byteOff}
	vm.lastMatch = m
	return m
}

// strIndexRegexp implements String#index with a Regexp: it finds the leftmost
// match at or after character offset off (matching the full subject so anchors
// and \G honour the offset), sets $~ (nil on no match) and returns the character
// index of the match start, or nil. off may equal the character count.
func (vm *VM) strIndexRegexp(re *Regexp, subject string, off int) object.Value {
	byteOff := charToByte(subject, off)
	md := re.matcher().Match(subject[byteOff:]) // leftmost match in the tail; \G pins to its start
	if md == nil {
		vm.lastMatch = object.NilV
		return object.NilV
	}
	vm.lastMatch = &MatchData{md: md, subject: subject, re: re, byteOff: byteOff}
	return object.IntValue(int64(byteToChar(subject, byteOff+md.Begin(0))))
}

// lastRegexpMatch returns the match of re that begins at the largest byte offset
// in subject (the rightmost match, as String#rpartition selects it), or nil when
// there is none. It probes anchored matches from the end toward the start, so the
// whole string stays visible to anchors and lookbehind.
func (vm *VM) lastRegexpMatch(re *Regexp, subject string) *MatchData {
	for p := len(subject); p >= 0; p-- {
		md := re.matcher().MatchAt(subject, p)
		if md != nil && md.Begin(0) == p {
			return &MatchData{md: md, subject: subject, re: re}
		}
	}
	return nil
}

// strRindexRegexp implements String#rindex with a Regexp: it returns the largest
// character index p (p <= limit) at which re matches starting exactly at p,
// setting $~ (nil when there is no such match). limit has been clamped to the
// character count by the caller.
func (vm *VM) strRindexRegexp(re *Regexp, subject string, limit int) object.Value {
	for p := limit; p >= 0; p-- {
		bytep := charToByte(subject, p)
		md := re.matcher().MatchAt(subject, bytep)
		if md != nil && md.Begin(0) == bytep {
			vm.lastMatch = &MatchData{md: md, subject: subject, re: re}
			return object.IntValue(int64(p))
		}
	}
	vm.lastMatch = object.NilV
	return object.NilV
}

// gvar reads a global variable. The match-data specials derive from $~ (the
// last match); any other name reads as nil (uninitialised global).
func (vm *VM) gvar(name string) object.Value {
	if v, handled := vm.specialGvar(name); handled {
		return v
	}
	// English match-data aliases ($MATCH -> $&, …) rewrite to the cryptic form so
	// the match-data resolution below applies; specialGvar reported them unhandled.
	if target, ok := englishAlias[name]; ok {
		name = target
	}
	last := vm.lastMatch
	if object.IsNil(last) {
		last = object.NilV
	}
	if name == "$~" {
		return last
	}
	md, ok := last.(*MatchData)
	switch name {
	case "$&":
		if ok {
			return object.NewString(md.md.Str(0))
		}
	case "$`":
		if ok {
			return object.NewString(md.md.Pre())
		}
	case "$'":
		if ok {
			return object.NewString(md.md.Post())
		}
	case "$+":
		// The last capture group that participated (highest-numbered match), or
		// nil when there were no groups or none participated.
		if ok {
			for i := md.md.NGroups(); i >= 1; i-- {
				if md.md.Begin(i) >= 0 {
					return object.NewString(md.md.Str(i))
				}
			}
		}
	default:
		if n, isGroup := gvarGroup(name); isGroup {
			if ok && n <= md.md.NGroups() {
				return groupValue(md, n)
			}
			return object.NilV
		}
		// Any other name is an ordinary user global: nil until assigned.
		if v, set := vm.globals[name]; set {
			return v
		}
	}
	return object.NilV
}

// gvarGroup parses "$N" (N a positive integer) into its group number.
func gvarGroup(name string) (int, bool) {
	if len(name) < 2 || name[1] < '1' || name[1] > '9' {
		return 0, false
	}
	n := 0
	for _, c := range name[1:] {
		n = n*10 + int(c-'0')
	}
	return n, true
}

// byteToChar converts a non-negative byte offset into the character offset Ruby
// reports. Callers guard against participating-group offsets before calling, so
// byteOff is always within s.
func byteToChar(s string, byteOff int) int {
	return utf8.RuneCountInString(s[:byteOff])
}

// charToByte converts a character offset into a byte offset in s (the inverse of
// byteToChar); an offset at or past the rune count returns len(s).
func charToByte(s string, charOff int) int {
	n := 0
	for i := range s {
		if n == charOff {
			return i
		}
		n++
	}
	return len(s)
}

// scanRegexp coerces the argument of String#scan into a Regexp: a Regexp passes
// through; a String is matched literally (its metacharacters are escaped, as
// Ruby does); anything else raises TypeError.
func scanRegexp(v object.Value) *Regexp {
	switch x := v.(type) {
	case *Regexp:
		return x
	case *object.String:
		// The escaped literal is always a well-formed pattern, so compilation
		// cannot fail here (the engine even accepts raw, non-UTF-8 bytes).
		src := regexpEscapeLiteral(x.Str())
		re, _ := onig.Compile(src)
		return &Regexp{re: re, source: src}
	default:
		raise("TypeError", "wrong argument type %s (expected Regexp)", classNameOf(v))
		return nil
	}
}

// regexpEscapeLiteral backslash-escapes the regexp metacharacters in s so it
// matches literally. Only the operators special at top level are escaped (the
// engine rejects superfluous escapes such as \-); control and other bytes are
// emitted verbatim, which the byte-oriented engine matches literally.
// regexpOperandStr converts a Regexp operand (for Regexp.quote/escape/union of a
// non-Regexp) to its string form the way MRI's reg_operand does: a String is
// taken directly, a Symbol yields its name, and anything else is coerced via
// #to_str (raising TypeError when it has none).
func (vm *VM) regexpOperandStr(v object.Value) string {
	switch x := v.(type) {
	case *object.String:
		return x.Str()
	case object.Symbol:
		return string(x)
	}
	if vm.respondsToDynamic(v, "to_str") {
		if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
			return s.Str()
		}
	}
	raise("TypeError", "no implicit conversion of %s into String", vm.classOf(v).name)
	return ""
}

// regexpUnion implements Regexp.union, following MRI's structure. With no
// arguments it matches nothing (/(?!)/). A single argument is special: a Regexp
// (or #to_regexp) is returned verbatim, an Array recurses as the pattern list,
// and a lone String/Symbol becomes a single quoted pattern. Two or more patterns
// are joined by '|', each Regexp contributing its #to_s and each other operand
// coerced via #to_str only (a Symbol raises TypeError here, unlike the lone case).
func (vm *VM) regexpUnion(args []object.Value) object.Value {
	if len(args) == 0 {
		return vm.regexpNew([]object.Value{object.NewString("(?!)")})
	}
	if len(args) == 1 {
		if re, ok := vm.toRegexpOperand(args[0]); ok {
			return re
		}
		if arr, ok := args[0].(*object.Array); ok {
			return vm.regexpUnion(arr.Elems)
		}
		// A lone String/Symbol: one quoted pattern, never an alternation.
		return vm.regexpNew([]object.Value{object.NewString(regexpEscapeLiteral(vm.regexpOperandStr(args[0])))})
	}
	sources := make([]string, len(args))
	for i, a := range args {
		if re, ok := vm.toRegexpOperand(a); ok {
			sources[i] = re.ToS()
		} else {
			sources[i] = regexpEscapeLiteral(vm.regexpUnionStr(a))
		}
	}
	return vm.regexpNew([]object.Value{object.NewString(strings.Join(sources, "|"))})
}

// regexpUnionStr coerces a non-Regexp operand of a multi-pattern Regexp.union to
// a String via #to_str only (MRI's rb_check_string_type): a String is taken
// directly, anything else must supply #to_str, and a Symbol — which has none —
// raises TypeError, unlike the single-argument union or Regexp.quote.
func (vm *VM) regexpUnionStr(v object.Value) string {
	if s, ok := v.(*object.String); ok {
		return s.Str()
	}
	if vm.respondsToDynamic(v, "to_str") {
		if s, ok := vm.send(v, "to_str", nil, nil).(*object.String); ok {
			return s.Str()
		}
	}
	raise("TypeError", "no implicit conversion of %s into String", vm.classOf(v).name)
	return ""
}

// toRegexpOperand reports whether v is a Regexp or converts to one via #to_regexp,
// returning that Regexp. It never raises: a value without #to_regexp is simply not
// a Regexp operand.
func (vm *VM) toRegexpOperand(v object.Value) (*Regexp, bool) {
	if re, ok := v.(*Regexp); ok {
		return re, true
	}
	if vm.respondsToDynamic(v, "to_regexp") {
		if re, ok := vm.send(v, "to_regexp", nil, nil).(*Regexp); ok {
			return re, true
		}
	}
	return nil, false
}

func regexpEscapeLiteral(s string) string {
	// The metacharacters MRI's rb_reg_quote backslash-escapes; note '-' and ' '
	// and '#' are included and '/' is NOT (it is not special outside a literal).
	const meta = `[]{}()|-*.\?+^$ #`
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\f':
			b.WriteString(`\f`)
		case '\t':
			b.WriteString(`\t`)
		case '\v':
			b.WriteString(`\v`)
		default:
			if strings.IndexByte(meta, c) >= 0 {
				b.WriteByte('\\')
			}
			b.WriteByte(c)
		}
	}
	return b.String()
}

// scan implements String#scan: it finds every non-overlapping match of re in
// subject left to right. With no capture groups each result element is the
// whole match; with one or more groups each element is the array of that
// match's captures (nil for a non-participating group). When blk is non-nil
// each element is yielded and the receiver string is returned; otherwise the
// elements are collected into an Array.
//
// After an empty match the scan advances by one character (Ruby semantics) so
// it terminates; a non-empty match advances past its end.
func (vm *VM) scan(re *Regexp, subject string, self object.Value, blk *Proc) object.Value {
	var results []object.Value
	enc := "" // result substrings inherit the receiver's encoding
	if s, ok := self.(*object.String); ok {
		enc = s.Enc
	}
	last := object.Value(object.NilV) // $~ after the call: last match, or nil when none
	pos := 0
	for pos <= len(subject) {
		md := re.matcher().Match(subject[pos:])
		if md == nil {
			break
		}
		elem := scanElement(md, enc)
		// Expose this match through $~ / $1.. (with the whole subject and an
		// absolute byteOff so MatchData#string, #begin and #offset are correct).
		// MRI leaves $~ set to the last match after scan returns, even if a block
		// reassigned it, so re-set it after the block runs.
		cur := &MatchData{md: md, subject: subject, re: re, byteOff: pos}
		vm.lastMatch = cur
		last = cur
		if blk != nil {
			vm.callBlock(blk, []object.Value{elem})
			vm.lastMatch = cur
		} else {
			results = append(results, elem)
		}
		matchEnd := md.End(0) // byte offset within subject[pos:]
		if matchEnd == md.Begin(0) {
			// Empty match: emit here, then step one character forward.
			pos += matchEnd
			if pos >= len(subject) {
				break
			}
			_, w := utf8.DecodeRuneInString(subject[pos:])
			pos += w
		} else {
			pos += matchEnd
		}
	}
	// $~ is the last match (or nil when there was none), as MRI leaves it.
	vm.lastMatch = last
	if blk != nil {
		return self
	}
	return object.NewArrayFromSlice(results)
}

// stringSplit backs String#split. With no pattern, a nil pattern, or the single
// space string " ", it splits on runs of whitespace (awk-style: leading
// whitespace is ignored and no empty fields are produced). Otherwise it splits
// on a Regexp (a String is matched literally), interpolating any captured
// groups between the pieces and dropping non-participating captures.
//
// An optional Integer limit caps the number of fields: a positive limit stops
// splitting once limit-1 fields have been taken (the last field is the unsplit
// remainder); a limit <= 0 keeps trailing empty fields, while the absent or
// zero limit strips them.
// stringSplit implements String#split; enc is the receiver's encoding, which
// every result substring inherits (MRI keeps split pieces in the same encoding
// as self).
func (vm *VM) stringSplit(subject, enc string, args []object.Value) object.Value {
	// A nil or absent pattern falls back to $; (the field separator): when $; is a
	// String it becomes the pattern; a nil $; keeps awk-style whitespace mode.
	if len(args) == 0 || object.IsNil(args[0]) {
		switch fs := vm.gvar("$;").(type) {
		case *object.String:
			args = replaceSplitPattern(args, fs)
		case *Regexp:
			args = replaceSplitPattern(args, fs)
		}
	}
	// A pattern that is neither a String, a Regexp nor nil is converted with
	// #to_str, and a non-Integer limit with #to_int, exactly as MRI does. Copy the
	// slice before rewriting the pattern so the caller's arguments are untouched.
	if len(args) >= 1 {
		switch args[0].(type) {
		case *object.String, *Regexp, object.Nil:
		default:
			if vm.respondsToDynamic(args[0], "to_str") {
				args = append([]object.Value(nil), args...)
				args[0] = vm.send(args[0], "to_str", nil, nil)
			}
		}
	}
	limit := 0
	if len(args) >= 2 {
		// A nil limit is not a default — MRI raises TypeError for split(p, nil) —
		// so coerceInt handles it (nil has no #to_int) alongside real conversions.
		limit = int(coerceInt(vm, args[1]))
	}
	if splitOnWhitespace(args) {
		return splitWhitespace(subject, limit, enc)
	}
	re := scanRegexp(args[0])
	return splitRegexp(re, subject, limit, enc)
}

// replaceSplitPattern returns args with the pattern (args[0]) set to pat,
// appending it when args was empty. The input slice is never mutated.
func replaceSplitPattern(args []object.Value, pat object.Value) []object.Value {
	if len(args) == 0 {
		return []object.Value{pat}
	}
	out := append([]object.Value(nil), args...)
	out[0] = pat
	return out
}

// splitOnWhitespace reports whether the split should use awk-style whitespace
// mode: no pattern, a nil pattern, or the literal single space " ".
func splitOnWhitespace(args []object.Value) bool {
	if len(args) == 0 {
		return true
	}
	switch p := args[0].(type) {
	case object.Nil:
		return true
	case *object.String:
		return p.Str() == " "
	default:
		return false
	}
}

// splitWhitespace implements awk-style whitespace splitting with an optional
// field limit.
func splitWhitespace(subject string, limit int, enc string) object.Value {
	var out []object.Value
	i := 0
	n := len(subject)
	for i < n {
		for i < n && isASCIISpace(subject[i]) { // skip leading whitespace
			i++
		}
		if i >= n {
			break
		}
		if limit > 0 && len(out)+1 == limit {
			out = append(out, object.NewStringViewEnc(subject[i:], enc))
			return object.NewArrayFromSlice(out)
		}
		start := i
		for i < n && !isASCIISpace(subject[i]) {
			i++
		}
		out = append(out, object.NewStringViewEnc(subject[start:i], enc))
	}
	// With an explicit non-zero limit, a run of trailing whitespace yields one
	// trailing empty field (awk mode collapses it to a single ""); the default
	// (limit 0) suppresses trailing empty fields.
	if limit != 0 && n > 0 && isASCIISpace(subject[n-1]) {
		out = append(out, object.NewStringViewEnc("", enc))
	}
	return object.NewArrayFromSlice(out)
}

func isASCIISpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

// splitRegexp splits subject on matches of re, interpolating captured groups
// and honouring the field limit (see stringSplit).
func splitRegexp(re *Regexp, subject string, limit int, enc string) object.Value {
	if subject == "" {
		return object.NewArray()
	}
	var out []object.Value
	last := 0   // byte offset of the start of the current field
	search := 0 // where the next match search begins
	pieces := 0 // count of delimiter-separated fields emitted (limit applies here)
	for search <= len(subject) {
		if limit > 0 && pieces+1 == limit {
			break
		}
		md := re.matcher().Match(subject[search:])
		if md == nil {
			break
		}
		mBegin := search + md.Begin(0)
		mEnd := search + md.End(0)
		if mEnd == mBegin {
			// Empty match: an empty match at the very start is skipped; otherwise
			// it ends the current character field. Advance one character.
			if mBegin >= len(subject) {
				// A zero-width match at end-of-string closes the final character
				// field (with its captures) and leaves a trailing empty field, which
				// the post-loop tail append produces — kept unless limit 0 strips it.
				if mBegin != last {
					out = append(out, object.NewStringViewEnc(subject[last:mBegin], enc))
					out = append(out, captureFields(md, enc)...)
					pieces++
					last = mBegin
				}
				break
			}
			if mBegin == last {
				_, w := utf8.DecodeRuneInString(subject[mBegin:])
				search = mBegin + w
				continue
			}
			out = append(out, object.NewStringViewEnc(subject[last:mBegin], enc))
			out = append(out, captureFields(md, enc)...)
			pieces++
			last = mBegin
			_, w := utf8.DecodeRuneInString(subject[mBegin:])
			search = mBegin + w
			continue
		}
		out = append(out, object.NewStringViewEnc(subject[last:mBegin], enc))
		out = append(out, captureFields(md, enc)...)
		pieces++
		last = mEnd
		search = mEnd
	}
	out = append(out, object.NewStringViewEnc(subject[last:], enc))
	if limit == 0 {
		// Strip trailing empty fields (the default behaviour).
		for len(out) > 0 {
			if s, ok := out[len(out)-1].(*object.String); ok && len(s.Bytes()) == 0 {
				out = out[:len(out)-1]
				continue
			}
			break
		}
	}
	return object.NewArrayFromSlice(out)
}

// captureFields returns the participating capture groups of a split delimiter
// match, in order, each in the receiver's encoding enc. Non-participating groups
// are dropped (Ruby omits them).
func captureFields(md *onig.MatchData, enc string) []object.Value {
	var out []object.Value
	for i := 1; i <= md.NGroups(); i++ {
		if md.Begin(i) >= 0 {
			out = append(out, object.NewStringViewEnc(md.Str(i), enc))
		}
	}
	return out
}

// stringSub backs String#sub (global=false) and String#gsub (global=true). The
// first argument is the pattern (a Regexp, or a String matched literally). A
// replacement is given as a second String argument (with backref templates), a
// second Hash argument (each match is replaced by hash[match], "" when absent),
// or a block (yielded each match). With neither a replacement nor a block,
// gsub returns an Enumerator over the matches; sub raises ArgumentError, as MRI
// does.
func (vm *VM) stringSub(subject string, args []object.Value, blk *Proc, global bool) object.Value {
	re := vm.subRegexp(args[0])
	// A replacement argument takes precedence over a block: MRI ignores the block
	// when a String or Hash replacement is also supplied.
	if len(args) >= 2 {
		if h, ok := args[1].(*object.Hash); ok {
			return vm.gsubHash(re, subject, h, global)
		}
		repl, _ := vm.strCoerceArg(args[1]) // a non-String replacement converts via #to_str
		return vm.gsub(re, subject, repl, nil, global)
	}
	if blk != nil {
		return vm.gsub(re, subject, "", blk, global)
	}
	if !global {
		raise("ArgumentError", "wrong number of arguments (given 1, expected 2)")
	}
	// gsub(pattern) with no replacement and no block → an Enumerator yielding
	// the matched substrings; supports #with_index, #to_a, etc. via the
	// receiver+method form, replaying gsub with the enumerator's block. MRI reports
	// this enumerator's #size as nil (the match count is not known ahead of time).
	return enumForSized(object.NewString(subject), "gsub",
		func(*VM) object.Value { return object.NilV }, args[0])
}

// subRegexp coerces the pattern argument of String#sub/#gsub into a Regexp: a
// Regexp passes through; anything else is coerced to a String (via #to_str,
// unwrapping a String subclass) and matched literally, exactly as MRI does.
func (vm *VM) subRegexp(v object.Value) *Regexp {
	if u, ok := v.(object.KeyUnwrapper); ok {
		if w, wrapped := u.HashUnwrap(); wrapped {
			v = w
		}
	}
	if r, ok := v.(*Regexp); ok {
		return r
	}
	s, _ := vm.strCoerceArg(v)
	src := regexpEscapeLiteral(s)
	re, _ := onig.Compile(src)
	return &Regexp{re: re, source: src}
}

// gsub implements String#sub (global=false) and String#gsub (global=true) over
// a Regexp. Each match is replaced either by expanding a replacement template
// (with \0/\&, \1..\9, \k<name>, \`, \' backrefs) or by the to_s of a block's
// result (the block is yielded the matched substring). Returns the new string.
//
// Empty matches advance one character (Ruby semantics); a non-empty match
// advances past its end. With global=false only the first match is replaced.
func (vm *VM) gsub(re *Regexp, subject, repl string, blk *Proc, global bool) object.Value {
	var b strings.Builder
	pos := 0                          // byte cursor into subject (start of the not-yet-emitted tail)
	search := 0                       // byte cursor where the next search begins
	last := object.Value(object.NilV) // $~ after the call: last match, or nil when there is none
	for search <= len(subject) {
		md := re.matcher().Match(subject[search:])
		if md == nil {
			break
		}
		mBegin := search + md.Begin(0)
		mEnd := search + md.End(0)
		b.WriteString(subject[pos:mBegin]) // literal text before the match
		// Expose this match through $~ / $1.. so a replacement block sees the
		// captures. md's offsets are relative to the searched slice, so carry the
		// FULL subject with byteOff=search — then MatchData#string is the whole
		// receiver and #offset/#begin are absolute, as MRI reports inside the block.
		cur := &MatchData{md: md, subject: subject, re: re, byteOff: search}
		vm.lastMatch = cur
		last = cur
		if blk != nil {
			res := vm.callBlock(blk, []object.Value{object.NewString(md.Str(0))})
			b.WriteString(vm.send(res, "to_s", nil, nil).ToS())
		} else {
			// Prematch/postmatch are taken from the whole subject so \` and \'
			// span text already consumed by earlier matches (Ruby semantics).
			b.WriteString(expandReplacement(repl, md, subject[:mBegin], subject[mEnd:]))
		}
		pos = mEnd
		if mEnd == mBegin { // empty match: emit one char, step forward
			if mEnd >= len(subject) {
				search = mEnd
				break
			}
			_, w := utf8.DecodeRuneInString(subject[mEnd:])
			b.WriteString(subject[mEnd : mEnd+w])
			pos = mEnd + w
			search = mEnd + w
		} else {
			search = mEnd
		}
		if !global {
			break
		}
	}
	// MRI leaves $~ as the last match (or nil when there was none) after the call,
	// even if a block reassigned $~ via its own match.
	vm.lastMatch = last
	b.WriteString(subject[pos:]) // remaining tail
	return object.NewString(b.String())
}

// gsubHash implements the Hash-replacement form of String#sub/#gsub: each
// matched substring m is replaced by hash[m], or the empty string when the hash
// has no such key. $~ / $1.. are updated per match, as in the block form.
func (vm *VM) gsubHash(re *Regexp, subject string, h *object.Hash, global bool) object.Value {
	var b strings.Builder
	pos := 0                          // byte cursor into subject (start of the not-yet-emitted tail)
	search := 0                       // byte cursor where the next search begins
	last := object.Value(object.NilV) // $~ after the call: last match, or nil when there is none
	for search <= len(subject) {
		md := re.matcher().Match(subject[search:])
		if md == nil {
			break
		}
		mBegin := search + md.Begin(0)
		mEnd := search + md.End(0)
		b.WriteString(subject[pos:mBegin]) // literal text before the match
		// Carry the FULL subject with byteOff=search so $~ reports absolute
		// offsets and the whole receiver (see gsub).
		cur := &MatchData{md: md, subject: subject, re: re, byteOff: search}
		vm.lastMatch = cur
		last = cur
		// Look the match up with Hash#[] (not a bare Get) so a missing key runs the
		// hash's default value / default_proc, exactly as MRI does. A nil result
		// (no matching key and no default) contributes nothing; any other value is
		// coerced with #to_s.
		v := vm.send(h, "[]", []object.Value{object.NewString(md.Str(0))}, nil)
		if _, isNil := v.(object.Nil); !isNil {
			b.WriteString(vm.send(v, "to_s", nil, nil).ToS())
		}
		pos = mEnd
		if mEnd == mBegin { // empty match: emit one char, step forward
			if mEnd >= len(subject) {
				search = mEnd
				break
			}
			_, w := utf8.DecodeRuneInString(subject[mEnd:])
			b.WriteString(subject[mEnd : mEnd+w])
			pos = mEnd + w
			search = mEnd + w
		} else {
			search = mEnd
		}
		if !global {
			break
		}
	}
	// $~ is the last match (or nil when none), even after a default_proc reset it.
	vm.lastMatch = last
	b.WriteString(subject[pos:]) // remaining tail
	return object.NewString(b.String())
}

// expandReplacement expands a sub/gsub replacement template against a match:
// \0 and \& insert the whole match; \1..\9 a numbered group (empty when the
// group did not participate or is out of range); \k<name> a named group
// (IndexError for an unknown name); \` the pre-match and \' the post-match; \\
// a literal backslash. A backslash before any other character (or at the end)
// is kept literally with that character.
func expandReplacement(tmpl string, md *onig.MatchData, pre, post string) string {
	var b strings.Builder
	for i := 0; i < len(tmpl); i++ {
		c := tmpl[i]
		if c != '\\' || i+1 >= len(tmpl) {
			b.WriteByte(c)
			continue
		}
		n := tmpl[i+1]
		switch {
		case n >= '0' && n <= '9':
			idx := int(n - '0')
			if idx <= md.NGroups() && md.Begin(idx) >= 0 {
				b.WriteString(md.Str(idx))
			}
			i++
		case n == '&':
			b.WriteString(md.Str(0))
			i++
		case n == '+':
			// \+ inserts the last capture group that participated (highest-numbered),
			// or nothing when there were no captures or none participated.
			for gi := md.NGroups(); gi >= 1; gi-- {
				if md.Begin(gi) >= 0 {
					b.WriteString(md.Str(gi))
					break
				}
			}
			i++
		case n == '`':
			b.WriteString(pre)
			i++
		case n == '\'':
			b.WriteString(post)
			i++
		case n == '\\':
			b.WriteByte('\\')
			i++
		case n == 'k' && i+2 < len(tmpl) && tmpl[i+2] == '<':
			j := i + 3
			for j < len(tmpl) && tmpl[j] != '>' {
				j++
			}
			if j >= len(tmpl) { // \k< without a closing '>'
				raise("RuntimeError", "invalid group name reference format")
			}
			name := tmpl[i+3 : j]
			gi := md.IndexOfName(name)
			if gi < 0 {
				raise("IndexError", "undefined group name reference: %s", name)
			}
			if md.Begin(gi) >= 0 {
				b.WriteString(md.Str(gi))
			}
			i = j
		default: // \ followed by an ordinary char: keep the backslash and the char
			b.WriteByte(c)
		}
	}
	return b.String()
}

// scanElement builds one String#scan result element from a match: the whole
// match (no groups) or the array of captures (one or more groups). Every result
// substring inherits enc, the receiver's encoding (MRI keeps scan pieces in the
// same encoding as self).
func scanElement(md *onig.MatchData, enc string) object.Value {
	n := md.NGroups()
	if n == 0 {
		return object.NewStringViewEnc(md.Str(0), enc)
	}
	caps := make([]object.Value, n)
	for i := 1; i <= n; i++ {
		if md.Begin(i) < 0 {
			caps[i-1] = object.NilV
		} else {
			caps[i-1] = object.NewStringViewEnc(md.Str(i), enc)
		}
	}
	return object.NewArrayFromSlice(caps)
}

// namedGroups returns the names of (?<name>…) capture groups in source, in
// order of appearance (duplicates kept; resolution to indices is delegated to
// the engine). Escaped parens and the (?<=…)/(?<!…) look-behind forms are
// skipped, since neither introduces a named capture.
func namedGroups(source string) []string {
	var names []string
	for i := 0; i+3 < len(source); i++ {
		if source[i] == '\\' { // skip the escaped character
			i++
			continue
		}
		if source[i] != '(' || source[i+1] != '?' || source[i+2] != '<' {
			continue
		}
		// Look-behind groups (?<= and (?<! are not named captures.
		if source[i+3] == '=' || source[i+3] == '!' {
			continue
		}
		j := i + 3
		for j < len(source) && source[j] != '>' {
			j++
		}
		if j < len(source) {
			names = append(names, source[i+3:j])
		}
	}
	return names
}

// dedupNames returns names with duplicates removed, keeping first-seen order.
func dedupNames(names []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// regexpNamesArray is the Array of a source's capture names (deduplicated,
// first-appearance order), shared by Regexp#names and MatchData#names.
func regexpNamesArray(source string) object.Value {
	names := dedupNames(namedGroups(source))
	out := make([]object.Value, len(names))
	for i, n := range names {
		out[i] = object.NewString(n)
	}
	return object.NewArrayFromSlice(out)
}

// groupValue returns group i of a match as a Ruby value: nil for a
// non-participating group, else the captured substring.
func groupValue(m *MatchData, i int) object.Value {
	if m.md.Begin(i) < 0 {
		return object.NilV
	}
	return object.NewString(m.md.Str(i))
}

// installRegexp registers the Regexp and MatchData method tables. It runs at the
// end of bootstrap so the classes already exist as constants.
func (vm *VM) installRegexp() {
	// reArg unwraps the Regexp receiver, raising TypeError for an uninitialized
	// one (Regexp.allocate without a call to #initialize), as MRI does.
	reArg := func(v object.Value) *Regexp {
		r, ok := v.(*Regexp)
		if !ok {
			raise("TypeError", "uninitialized Regexp")
		}
		return r
	}

	// Regexp option constants (MRI values): IGNORECASE=1, EXTENDED=2, MULTILINE=4.
	vm.cRegexp.consts["IGNORECASE"] = object.IntValue(reIgnoreCase)
	vm.cRegexp.consts["EXTENDED"] = object.IntValue(reExtended)
	vm.cRegexp.consts["MULTILINE"] = object.IntValue(reMultiline)
	vm.cRegexp.consts["FIXEDENCODING"] = object.IntValue(reFixedEncoding)
	vm.cRegexp.consts["NOENCODING"] = object.IntValue(reNoEncoding)

	// Regexp.new(str_or_regexp[, options]) / Regexp.compile(...) build a Regexp at
	// runtime. A Regexp argument is copied (its options are reused); a String is
	// compiled with the options decoded from the second argument.
	reNew := &Method{name: "new", owner: vm.cRegexp,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.regexpNew(args)
		}}
	vm.cRegexp.smethods["new"] = reNew
	vm.cRegexp.smethods["compile"] = &Method{name: "compile", owner: vm.cRegexp, native: reNew.native}

	// Regexp#initialize is a private method that a user can never usefully call: a
	// Regexp is fully built by Regexp.new / a literal, so re-running #initialize on
	// a frozen literal raises FrozenError and on an already-initialized non-literal
	// raises TypeError ("already initialized regexp"), matching MRI (< 4.1).
	vm.cRegexp.define("initialize", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if isFrozen(self) {
			vm.raiseFrozen(self)
		}
		raise("TypeError", "already initialized regexp")
		return object.NilV
	})
	vm.setInstanceVisibility(vm.cRegexp, "initialize", visPrivate)

	// Regexp.escape(str) / Regexp.quote(str): the string with regex metacharacters
	// escaped so it matches literally. A Symbol is accepted (its name is quoted),
	// matching MRI's reg_operand handling.
	reEscape := &Method{name: "escape", owner: vm.cRegexp,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return object.NewString(regexpEscapeLiteral(vm.regexpOperandStr(args[0])))
		}}
	vm.cRegexp.smethods["escape"] = reEscape
	// quote shares escape's *Method so `Regexp.method(:escape) == Regexp.method(:quote)`.
	vm.cRegexp.smethods["quote"] = reEscape

	// Regexp.last_match returns the MatchData of the most recent match ($~), or
	// with an Integer / name argument the corresponding capture (Regexp.last_match(1)
	// == $1). nil when there has been no match, matching MRI.
	vm.cRegexp.smethods["last_match"] = &Method{name: "last_match", owner: vm.cRegexp,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			md, ok := vm.lastMatch.(*MatchData)
			if !ok {
				return object.NilV
			}
			if len(args) == 0 {
				return md
			}
			// An index argument that is not already an Integer/String/Symbol is
			// coerced to an Integer via #to_int, matching MRI's rb_reg_nth_match path.
			key := args[0]
			switch key.(type) {
			case object.Integer, *object.String, object.Symbol:
			default:
				if vm.respondsToDynamic(key, "to_int") {
					if iv, ok := vm.send(key, "to_int", nil, nil).(object.Integer); ok {
						key = iv
					}
				}
			}
			return vm.send(md, "[]", []object.Value{key}, nil)
		}}

	// Regexp.union(pat, ...) / Regexp.union([pat, ...]) builds one Regexp matching
	// any of the patterns. A Regexp argument contributes its #to_s (so its options
	// ride along); a String/Symbol is escaped to match literally. With no arguments
	// it matches nothing, as MRI.
	vm.cRegexp.smethods["union"] = &Method{name: "union", owner: vm.cRegexp,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			return vm.regexpUnion(args)
		}}

	vm.cRegexp.define("source", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(reArg(self).source)
	})
	vm.cRegexp.define("options", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(reArg(self).optionBits())
	})
	vm.cRegexp.define("casefold?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(strings.ContainsRune(reArg(self).flags, 'i'))
	})
	vm.cRegexp.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(reArg(self).ToS())
	})
	vm.cRegexp.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(reArg(self).Inspect())
	})
	vm.cRegexp.define("match?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if _, isNil := args[0].(object.Nil); isNil {
			return object.False
		}
		re := reArg(self)
		subject := strArg(args[0])
		// match?(str, pos): probe from character offset pos, without touching $~
		// (the predicate form has no match-data side effect).
		if len(args) >= 2 {
			nChars := int64(utf8.RuneCountInString(subject))
			pos := intArg(args[1])
			if pos < 0 {
				pos += nChars
			}
			if pos < 0 || pos > nChars {
				return object.False
			}
			return object.Bool(re.matcher().MatchString(subject[charToByte(subject, int(pos)):]))
		}
		return object.Bool(re.matcher().MatchString(subject))
	})
	vm.cRegexp.define("match", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		re := reArg(self)
		// A nil subject never matches and resets $~ to nil (so a later Regexp.last_match
		// sees no match), returning nil without yielding to any block.
		if _, isNil := args[0].(object.Nil); isNil {
			vm.lastMatch = object.NilV
			return object.NilV
		}
		// The subject is coerced like any Regexp operand: a Symbol yields its name,
		// anything else is taken via #to_str (Integer/Exception raise TypeError).
		subject := vm.regexpOperandStr(args[0])
		var md object.Value
		if len(args) >= 2 {
			// match(str, pos): start scanning at character offset pos.
			md = vm.runMatchFrom(re, subject, intArg(args[1]))
		} else {
			md = vm.runMatch(re, subject)
		}
		// With a block, a successful match yields the MatchData and the block's value
		// is returned; a failed match returns nil and never yields.
		if blk != nil {
			if _, isNil := md.(object.Nil); isNil {
				return object.NilV
			}
			return vm.callBlock(blk, []object.Value{md})
		}
		return md
	})
	vm.cRegexp.define("=~", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.regexpMatchIndex(reArg(self), args[0])
	})
	vm.cRegexp.define("===", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s, ok := stringLike(args[0])
		if !ok {
			// A string-like object is accepted via #to_str (MRI's rb_check_string_type).
			if vm.respondsToDynamic(args[0], "to_str") {
				if str, isStr := vm.send(args[0], "to_str", nil, nil).(*object.String); isStr {
					s, ok = str.Str(), true
				}
			}
		}
		if !ok {
			// A non-string operand never matches and clears $~, as MRI does (so a
			// case/when over a non-string subject leaves no stale last match).
			vm.lastMatch = object.NilV
			return object.False
		}
		re := reArg(self)
		md := re.matcher().Match(s)
		if md == nil {
			vm.lastMatch = object.NilV
			return object.False
		}
		// Like =~, a successful === records $~ so Regexp.last_match / $1 work in the
		// taken case/when branch (Trollop derives an option's :long this way).
		vm.lastMatch = &MatchData{md: md, subject: s, re: re}
		return object.True
	})

	// Regexp#names lists the names of the (?<name>…) capture groups in order of
	// first appearance, without duplicates (MRI collapses repeated names).
	vm.cRegexp.define("names", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return regexpNamesArray(reArg(self).source)
	})
	// Regexp#named_captures maps each capture name to the Array of its group
	// indices. Because Ruby forbids mixing named and numbered captures, the named
	// groups are the only capturing groups, so the k-th named group (skipping
	// non-capturing groups, which namedGroups already ignores) has index k.
	vm.cRegexp.define("named_captures", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		h := object.NewHash()
		for i, name := range namedGroups(reArg(self).source) {
			h.Set(object.NewString(name), object.NewArrayFromSlice([]object.Value{object.IntValue(int64(i + 1))}))
		}
		return h
	})
	// Regexp#== / #eql? compare the source and the full option mask (i/m/x plus
	// the encoding options); a non-Regexp operand is never equal.
	reEqual := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := args[0].(*Regexp)
		if !ok {
			return object.False
		}
		a := reArg(self)
		return object.Bool(a.source == other.source && a.optionBits() == other.optionBits())
	}
	vm.cRegexp.define("==", reEqual)
	// #eql? is a genuine alias of #== (shared record).
	aliasBuiltin(vm.cRegexp, "eql?", "==")
	// Regexp#hash is consistent with #== / #eql?: equal Regexps hash equal.
	vm.cRegexp.define("hash", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		r := reArg(self)
		return object.IntValue(fnvHash(r.source) ^ r.optionBits())
	})
	// Regexp#encoding returns the Encoding the pattern is associated with.
	vm.cRegexp.define("encoding", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.internEncoding(reArg(self).encodingName())
	})
	// Regexp#fixed_encoding? reports whether the Regexp is tied to a specific
	// (non-ASCII) encoding rather than matching any ASCII-compatible string.
	vm.cRegexp.define("fixed_encoding?", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(reArg(self).isFixedEncoding())
	})
	// Regexp#~ matches the Regexp against $_ (the last line read by Kernel#gets),
	// returning the character offset of the match or nil. It records $~.
	vm.cRegexp.define("~", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		line, ok := vm.globals["$_"]
		if !ok {
			line = object.NilV
		}
		return vm.regexpMatchIndex(reArg(self), line)
	})
	// Regexp#timeout returns this Regexp's own match-time limit as a Float
	// (seconds), or nil when none was set at construction. It does not fall back
	// to the Regexp.timeout default.
	vm.cRegexp.define("timeout", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if t := reArg(self).timeout; t != nil {
			return t
		}
		return object.NilV
	})

	// Regexp.timeout / Regexp.timeout= read and write the process-wide default
	// match-time limit (a Float in seconds, or nil for no limit).
	vm.cRegexp.smethods["timeout"] = &Method{name: "timeout", owner: vm.cRegexp,
		native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			if vm.regexpTimeout == nil {
				return object.NilV
			}
			return vm.regexpTimeout
		}}
	vm.cRegexp.smethods["timeout="] = &Method{name: "timeout=", owner: vm.cRegexp,
		native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
			vm.regexpTimeout = coerceTimeout(args[0])
			return args[0]
		}}

	mdArg := func(v object.Value) *MatchData { return v.(*MatchData) }

	// MatchData.allocate is undefined (a MatchData can only arise from a match), so
	// it raises NoMethodError rather than returning an uninitialized object — see
	// https://bugs.ruby-lang.org/issues/16294.
	vm.cMatchData.smethods["allocate"] = &Method{name: "allocate", owner: vm.cMatchData,
		native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
			raise("NoMethodError", "undefined method 'allocate' for class MatchData")
			return object.NilV
		}}
	vm.cMatchData.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(mdArg(self).md.Str(0))
	})
	vm.cMatchData.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(mdArg(self).Inspect())
	})
	vm.cMatchData.define("pre_match", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		if m.byteOff == 0 {
			return object.NewString(m.md.Pre())
		}
		// Everything in the full subject before the match (the engine's Pre is
		// relative to the matched tail, so prepend the skipped prefix).
		return object.NewString(m.subject[:m.byteOff+m.md.Begin(0)])
	})
	vm.cMatchData.define("post_match", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		if m.byteOff == 0 {
			return object.NewString(m.md.Post())
		}
		return object.NewString(m.subject[m.byteOff+m.md.End(0):])
	})
	vm.cMatchData.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(mdArg(self).md.NGroups() + 1))
	})
	// MatchData#length is a genuine alias of #size (shared record, so
	// MatchData.instance_method(:length) == MatchData.instance_method(:size)).
	aliasBuiltin(vm.cMatchData, "length", "size")
	vm.cMatchData.define("to_a", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		out := make([]object.Value, 0, m.md.NGroups()+1)
		for i := 0; i <= m.md.NGroups(); i++ {
			out = append(out, groupValue(m, i))
		}
		return object.NewArrayFromSlice(out)
	})
	vm.cMatchData.define("captures", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		out := make([]object.Value, 0, m.md.NGroups())
		for i := 1; i <= m.md.NGroups(); i++ {
			out = append(out, groupValue(m, i))
		}
		return object.NewArrayFromSlice(out)
	})
	vm.cMatchData.define("begin", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		return m.offset(int64(m.indexForKey(vm, args[0])), false)
	})
	vm.cMatchData.define("end", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		return m.offset(int64(m.indexForKey(vm, args[0])), true)
	})
	// MatchData#offset(n_or_name) is the [begin, end] character-offset pair of the
	// group; [nil, nil] for a group that did not participate.
	vm.cMatchData.define("offset", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		idx := int64(m.indexForKey(vm, args[0]))
		return object.NewArrayFromSlice([]object.Value{m.offset(idx, false), m.offset(idx, true)})
	})
	// bytebegin / byteend / byteoffset are the byte-index counterparts of begin /
	// end / offset (Ruby 3.2+): they skip the byte-to-character conversion.
	vm.cMatchData.define("bytebegin", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		return m.byteOffset(int64(m.indexForKey(vm, args[0])), false)
	})
	vm.cMatchData.define("byteend", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		return m.byteOffset(int64(m.indexForKey(vm, args[0])), true)
	})
	vm.cMatchData.define("byteoffset", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		idx := int64(m.indexForKey(vm, args[0]))
		return object.NewArrayFromSlice([]object.Value{m.byteOffset(idx, false), m.byteOffset(idx, true)})
	})
	// MatchData#named_captures maps each capture name to its captured substring
	// (nil when the group did not participate); symbolize_names: true keys by
	// Symbol instead of String.
	vm.cMatchData.define("named_captures", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		symbolize := false
		if len(args) > 0 {
			if kw, ok := args[len(args)-1].(*object.Hash); ok {
				if sv, ok := kw.Get(object.Symbol("symbolize_names")); ok {
					symbolize = !object.IsNil(sv) && sv.Truthy()
				}
			}
		}
		h := object.NewHash()
		for _, name := range dedupNames(namedGroups(m.re.source)) {
			h.Set(namedKey(name, symbolize), groupValue(m, m.indexOfName(name)))
		}
		return h
	})
	vm.cMatchData.define("names", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return regexpNamesArray(mdArg(self).re.source)
	})
	vm.cMatchData.define("regexp", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return mdArg(self).re
	})
	vm.cMatchData.define("string", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		// MRI returns a frozen copy of the original subject.
		return object.NewFrozenStringView(mdArg(self).subject)
	})
	// MatchData#deconstruct is the Array of captures (groups 1..n), for array
	// pattern matching (`in [a, b]`); it is a genuine alias of #captures (shared
	// record, so MatchData.instance_method(:deconstruct) == …(:captures)).
	aliasBuiltin(vm.cMatchData, "deconstruct", "captures")
	// MatchData#deconstruct_keys(keys) is the symbol-keyed named captures, for
	// hash pattern matching (`in {name:}`). keys nil selects them all; otherwise
	// only the requested Symbol keys are returned, and the walk stops at the first
	// key that is not a capture name (matching MRI).
	vm.cMatchData.define("deconstruct_keys", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		return mdArg(self).deconstructKeys(args[0])
	})
	// MatchData#values_at(*args) selects groups by Integer index, name, or Range,
	// like #[] applied to each argument.
	vm.cMatchData.define("values_at", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		var out []object.Value
		for _, a := range args {
			if rng, ok := a.(*object.Range); ok {
				out = append(out, m.rangeValuesAt(rng)...)
				continue
			}
			out = append(out, m.at(a))
		}
		return object.NewArrayFromSlice(out)
	})
	// MatchData#== / #eql? compare the source Regexp, the subject, and the matched
	// span; a non-MatchData operand is never equal.
	mdEqual := func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		other, ok := args[0].(*MatchData)
		if !ok {
			return object.False
		}
		a := mdArg(self)
		return object.Bool(a.equalTo(other))
	}
	vm.cMatchData.define("==", mdEqual)
	// #eql? is a genuine alias of #== (shared record).
	aliasBuiltin(vm.cMatchData, "eql?", "==")
	vm.cMatchData.define("hash", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		return object.IntValue(fnvHash(m.re.source+"\x00"+m.md.Str(0)) ^ int64(m.byteOff+m.md.Begin(0)))
	})
	vm.cMatchData.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		// The Range and (start, length) forms slice the array of all groups
		// (whole match at 0, then each capture) — md[1..] is how callers grab just
		// the captures, as Puppet's parse_title does.
		if rng, ok := args[0].(*object.Range); ok {
			all := m.allGroups()
			start, length, ok := sliceRange(len(all), rng)
			if !ok {
				return object.NilV
			}
			out := make([]object.Value, length)
			copy(out, all[start:start+length])
			return object.NewArrayFromSlice(out)
		}
		if len(args) == 2 { // md[start, length]
			all := m.allGroups()
			start := normIndex(intArg(args[0]), len(all))
			length := int(intArg(args[1]))
			if start < 0 || start > len(all) || length < 0 {
				return object.NilV
			}
			end := start + length
			if end > len(all) {
				end = len(all)
			}
			out := make([]object.Value, end-start)
			copy(out, all[start:end])
			return object.NewArrayFromSlice(out)
		}
		return m.at(args[0])
	})
	// MatchData#match(n) (Ruby 3.4) returns the single group by index or name — the
	// scalar subset of #[] (no Range or (start, length) form), nil for a group that
	// did not participate.
	vm.cMatchData.define("match", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return mdArg(self).at(args[0])
	})
	// MatchData#match_length(n) (Ruby 3.4) is the character length of that group's
	// match, or nil when the group did not participate.
	vm.cMatchData.define("match_length", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		v := mdArg(self).at(args[0])
		s, ok := v.(*object.String)
		if !ok {
			return object.NilV
		}
		return object.IntValue(int64(utf8.RuneCountInString(s.Str())))
	})
}

// regexpMatchIndex implements Regexp#=~ and (via a String receiver) String#=~:
// the character offset of the match, or nil. nil coerces to a non-match; a
// String or Symbol is matched; any other subject raises TypeError (matching
// MRI, which only converts those types).
func (vm *VM) regexpMatchIndex(re *Regexp, subject object.Value) object.Value {
	if _, isNil := subject.(object.Nil); isNil {
		return object.NilV
	}
	s, ok := stringLike(subject)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into String", classNameOf(subject))
	}
	md := re.matcher().Match(s)
	if md == nil {
		vm.lastMatch = object.NilV
		return object.NilV
	}
	vm.lastMatch = &MatchData{md: md, subject: s, re: re}
	return object.IntValue(int64(byteToChar(s, md.Begin(0))))
}

// stringRegexpIndex implements String#[] / #slice with a Regexp argument: the
// whole match (no extra arg) or the numbered/named capture group, and nil when
// the pattern does not match. $~ is updated, as in MRI.
func (vm *VM) stringRegexpIndex(s string, re *Regexp, rest []object.Value) object.Value {
	md := re.matcher().Match(s)
	if md == nil {
		vm.lastMatch = object.NilV
		return object.NilV
	}
	m := &MatchData{md: md, subject: s, re: re}
	vm.lastMatch = m
	if len(rest) == 0 {
		return object.NewString(md.Str(0))
	}
	return m.at(rest[0])
}

// stringLike returns the Go string for a String or Symbol receiver (the two
// types Ruby's Regexp matching coerces), and whether it was one.
func stringLike(v object.Value) (string, bool) {
	switch x := v.(type) {
	case *object.String:
		return x.Str(), true
	case object.Symbol:
		return string(x), true
	default:
		return "", false
	}
}

// offset returns the character offset of group i's begin (end=false) or end
// (end=true), nil for a non-participating group. Callers resolve and validate
// the group index (via indexForKey) before calling.
func (m *MatchData) offset(i int64, end bool) object.Value {
	var b int
	if end {
		b = m.md.End(int(i))
	} else {
		b = m.md.Begin(int(i))
	}
	if b < 0 {
		return object.NilV
	}
	return object.IntValue(int64(byteToChar(m.subject, b+m.byteOff)))
}

// byteOffset is offset without the byte-to-character conversion: it returns the
// raw byte index of the group's start (or end), or nil for a group that did not
// participate. It backs #bytebegin / #byteend / #byteoffset.
func (m *MatchData) byteOffset(i int64, end bool) object.Value {
	var b int
	if end {
		b = m.md.End(int(i))
	} else {
		b = m.md.Begin(int(i))
	}
	if b < 0 {
		return object.NilV
	}
	return object.IntValue(int64(b + m.byteOff))
}

// at implements MatchData#[]: an Integer selects a group by index; a String or
// Symbol selects a named group (raising IndexError for an unknown name).
func (m *MatchData) at(key object.Value) object.Value {
	switch k := key.(type) {
	case object.Integer:
		return m.intGroup(int(k))
	case *object.String:
		return m.byName(k.Str())
	case object.Symbol:
		return m.byName(string(k))
	default:
		raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(key))
		return object.NilV
	}
}

// intGroup returns group i as MatchData#[] does for an Integer: a positive index
// selects that group (nil past the last); a negative index counts back from the
// group count, and — matching MRI — a negative index that lands on 0 is out of
// range (the whole match is reachable only by the literal 0).
func (m *MatchData) intGroup(i int) object.Value {
	n := m.md.NGroups()
	if i < 0 {
		i += n + 1
		if i <= 0 {
			return object.NilV
		}
	}
	if i > n {
		return object.NilV
	}
	return groupValue(m, i)
}

// indexOfName resolves a capture name to a group index. For an ordinary pattern it
// defers to the engine. For a pattern whose names were rewritten (duplicate or
// non-ASCII names) it applies Ruby's rule for a name shared by several groups:
// return the highest-indexed group with that name that participated in the match,
// or — when none participated — the highest-indexed group (so its value reads as
// nil). It returns -1 when no group has the name.
func (m *MatchData) indexOfName(name string) int {
	if m.re == nil || m.re.nameMap == nil {
		return m.md.IndexOfName(name)
	}
	syns, ok := m.re.nameMap[name]
	if !ok {
		return -1
	}
	best, bestMatched := -1, -1
	for _, syn := range syns {
		idx := m.md.IndexOfName(syn)
		if idx < 0 {
			continue
		}
		if idx > best {
			best = idx
		}
		if idx > bestMatched && m.md.Begin(idx) >= 0 {
			bestMatched = idx
		}
	}
	if bestMatched >= 0 {
		return bestMatched
	}
	return best
}

// byName resolves a named-group capture, raising IndexError if no group has the
// name.
func (m *MatchData) byName(name string) object.Value {
	i := m.indexOfName(name)
	if i < 0 {
		raise("IndexError", "undefined group name reference: %s", name)
	}
	return groupValue(m, i)
}

// indexForKey resolves a #begin/#end/#offset key to a group index: an Integer
// out of range raises IndexError; a String/Symbol name is resolved (IndexError
// when unknown); any other type is coerced through #to_int (raising TypeError
// when it has none), matching MRI.
func (m *MatchData) indexForKey(vm *VM, key object.Value) int {
	switch k := key.(type) {
	case object.Integer:
		i := int(k)
		if i < 0 || i > m.md.NGroups() {
			raise("IndexError", "index %d out of matches", i)
		}
		return i
	case *object.String:
		return m.nameIndex(k.Str())
	case object.Symbol:
		return m.nameIndex(string(k))
	default:
		if vm.respondsToDynamic(key, "to_int") {
			if iv, ok := vm.send(key, "to_int", nil, nil).(object.Integer); ok {
				return m.indexForKey(vm, iv)
			}
		}
		raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(key))
		return 0
	}
}

// nameIndex resolves a capture name to its group index, raising IndexError when
// no group has the name.
func (m *MatchData) nameIndex(name string) int {
	i := m.indexOfName(name)
	if i < 0 {
		raise("IndexError", "undefined group name reference: %s", name)
	}
	return i
}

// allGroups returns every group (whole match at 0, then each capture) as a Ruby
// value slice.
func (m *MatchData) allGroups() []object.Value {
	out := make([]object.Value, 0, m.md.NGroups()+1)
	for i := 0; i <= m.md.NGroups(); i++ {
		out = append(out, groupValue(m, i))
	}
	return out
}

// rangeValuesAt implements the Range form of MatchData#values_at: the range
// endpoints are resolved against the group count (num_regs = NGroups+1), then the
// concrete index sequence is walked, yielding the group (or nil past the last
// group) at each — so an over-long range pads with nil, as MRI does. A begin that
// is still negative after resolution raises RangeError.
func (m *MatchData) rangeValuesAt(r *object.Range) []object.Value {
	n := m.md.NGroups() + 1
	lo := 0
	if !object.IsNil(r.Lo) {
		lo = int(intArg(r.Lo))
		if lo < 0 {
			lo += n
		}
	}
	hi := n - 1
	if !object.IsNil(r.Hi) {
		hi = int(intArg(r.Hi))
		if hi < 0 {
			hi += n
		}
		if r.Exclusive {
			hi--
		}
	}
	if lo < 0 {
		raise("RangeError", "%s out of range", r.Inspect())
	}
	var out []object.Value
	for j := lo; j <= hi; j++ {
		if j >= 0 && j <= m.md.NGroups() {
			out = append(out, groupValue(m, j))
		} else {
			out = append(out, object.NilV)
		}
	}
	return out
}

// deconstructKeys implements MatchData#deconstruct_keys(keys): a nil keys
// argument returns all named captures (Symbol-keyed); an Array of Symbols
// returns just those captures, stopping at the first key that is not a capture
// name and short-circuiting to an empty Hash when more keys are requested than
// there are named captures. A non-Array, non-nil keys argument, or a non-Symbol
// element, raises TypeError.
func (m *MatchData) deconstructKeys(keys object.Value) object.Value {
	h := object.NewHash()
	if object.IsNil(keys) {
		for _, name := range dedupNames(namedGroups(m.re.source)) {
			h.Set(object.Symbol(name), groupValue(m, m.indexOfName(name)))
		}
		return h
	}
	arr, ok := keys.(*object.Array)
	if !ok {
		raise("TypeError", "wrong argument type %s (expected Array)", classNameOf(keys))
	}
	if len(arr.Elems) > len(dedupNames(namedGroups(m.re.source))) {
		return h
	}
	for _, k := range arr.Elems {
		sym, ok := k.(object.Symbol)
		if !ok {
			raise("TypeError", "wrong argument type %s (expected Symbol)", classNameOf(k))
		}
		i := m.indexOfName(string(sym))
		if i < 0 {
			break
		}
		h.Set(sym, groupValue(m, i))
	}
	return h
}

// equalTo backs MatchData#== / #eql?: two matches are equal when they come from
// an equal Regexp (same source and options), over the same subject, and cover
// the same byte span.
func (m *MatchData) equalTo(other *MatchData) bool {
	return m.re.source == other.re.source &&
		m.re.optionBits() == other.re.optionBits() &&
		m.subject == other.subject &&
		m.byteOff+m.md.Begin(0) == other.byteOff+other.md.Begin(0) &&
		m.byteOff+m.md.End(0) == other.byteOff+other.md.End(0)
}

// namedKey returns a capture-name Hash key as a Symbol (symbolize) or String.
func namedKey(name string, symbolize bool) object.Value {
	if symbolize {
		return object.Symbol(name)
	}
	return object.NewString(name)
}
