package vm

import (
	"strconv"
	"strings"
	"unicode/utf8"

	onig "github.com/go-onigmo/regexp"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Regexp is a compiled Ruby regular expression. It wraps the pure-Go go-onigmo
// engine so the interpreter stays CGO-free. flags holds the subset of the flag
// letters i, m, x that were present on the literal, in that canonical order.
//
// Byte-vs-character offsets: go-onigmo reports BYTE offsets, but Ruby's
// MatchData#begin/#end and String#=~ report CHARACTER offsets. The conversion
// happens in this package (byteToChar); matched substrings are
// representation-independent and are returned verbatim.
type Regexp struct {
	re     *onig.Regexp
	source string
	flags  string
}

func (r *Regexp) ToS() string {
	// Ruby's Regexp#to_s renders the (?on-off:src) form, where the on-set is the
	// present flags and the off-set is the absent ones, always in m, i, x order.
	on := orderFlags(r.flags)
	off := ""
	for _, f := range "mix" {
		if !strings.ContainsRune(r.flags, f) {
			off += string(f)
		}
	}
	if off != "" {
		off = "-" + off
	}
	return "(?" + on + off + ":" + r.source + ")"
}

func (r *Regexp) Inspect() string { return "/" + r.source + "/" + orderFlags(r.flags) }
func (r *Regexp) Truthy() bool    { return true }

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

// MatchData is the result of a successful match: it wraps the go-onigmo
// MatchData and remembers the subject string and source Regexp (for named
// captures and offset conversion).
type MatchData struct {
	md      *onig.MatchData
	subject string
	re      *Regexp
}

func (m *MatchData) ToS() string     { return m.md.Str(0) }
func (m *MatchData) Inspect() string { return "#<MatchData " + matchDataInspect(m) + ">" }
func (m *MatchData) Truthy() bool    { return true }

// matchDataInspect renders the body of MatchData#inspect: the whole match
// inspected, then each group as ` i:capture` (or ` name:capture` for named
// groups).
func matchDataInspect(m *MatchData) string {
	var b strings.Builder
	b.WriteString(object.String(m.md.Str(0)).Inspect())
	idxToName := indexToName(m)
	for i := 1; i <= m.md.NGroups(); i++ {
		b.WriteByte(' ')
		if nm, ok := idxToName[i]; ok {
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
		if i := m.md.IndexOfName(name); i >= 0 {
			out[i] = name
		}
	}
	return out
}

// compileRegexp builds a Regexp value from a literal's source and flag letters,
// translating the Ruby flags to an inline (?imx) prefix that go-onigmo accepts.
func (vm *VM) compileRegexp(source, flags string) object.Value {
	prefix := ""
	if imx := sortIMX(flags); imx != "" {
		// Translate the Ruby flags into an inline (?imx) prefix the engine accepts.
		prefix = "(?" + imx + ")"
	}
	re, err := onig.Compile(prefix + source)
	if err != nil {
		raise("RegexpError", "%s: /%s/", err.Error(), source)
	}
	return &Regexp{re: re, source: source, flags: flags}
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
	case object.String:
		re, err := onig.Compile(string(x))
		if err != nil {
			raise("RegexpError", "%s: /%s/", err.Error(), string(x))
		}
		return &Regexp{re: re, source: string(x)}
	default:
		raise("TypeError", "wrong argument type %s (expected Regexp)", classNameOf(v))
		return nil
	}
}

// runMatch matches re against subject, returning a MatchData value or nil. It
// also records the result as $~ (the last match).
func (vm *VM) runMatch(re *Regexp, subject string) object.Value {
	md := re.re.Match(subject)
	if md == nil {
		vm.lastMatch = object.NilV
		return object.NilV
	}
	m := &MatchData{md: md, subject: subject, re: re}
	vm.lastMatch = m
	return m
}

// gvar reads a global variable. The match-data specials derive from $~ (the
// last match); any other name reads as nil (uninitialised global).
func (vm *VM) gvar(name string) object.Value {
	last := vm.lastMatch
	if last == nil {
		last = object.NilV
	}
	if name == "$~" {
		return last
	}
	md, ok := last.(*MatchData)
	switch name {
	case "$&":
		if ok {
			return object.String(md.md.Str(0))
		}
	case "$`":
		if ok {
			return object.String(md.md.Pre())
		}
	case "$'":
		if ok {
			return object.String(md.md.Post())
		}
	default:
		if n, isGroup := gvarGroup(name); isGroup {
			if ok && n <= md.md.NGroups() {
				return groupValue(md, n)
			}
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

// scanRegexp coerces the argument of String#scan into a Regexp: a Regexp passes
// through; a String is matched literally (its metacharacters are escaped, as
// Ruby does); anything else raises TypeError.
func scanRegexp(v object.Value) *Regexp {
	switch x := v.(type) {
	case *Regexp:
		return x
	case object.String:
		// The escaped literal is always a well-formed pattern, so compilation
		// cannot fail here (the engine even accepts raw, non-UTF-8 bytes).
		src := regexpEscapeLiteral(string(x))
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
func regexpEscapeLiteral(s string) string {
	const meta = `.*+?()[]{}|^$\`
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if strings.IndexByte(meta, c) >= 0 {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
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
	pos := 0
	for pos <= len(subject) {
		md := re.re.Match(subject[pos:])
		if md == nil {
			break
		}
		elem := scanElement(md)
		if blk != nil {
			vm.callBlock(blk, []object.Value{elem})
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
	if blk != nil {
		return self
	}
	return &object.Array{Elems: results}
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
func (vm *VM) stringSplit(subject string, args []object.Value) object.Value {
	limit := 0
	if len(args) >= 2 {
		limit = int(intArg(args[1]))
	}
	if splitOnWhitespace(args) {
		return splitWhitespace(subject, limit)
	}
	re := scanRegexp(args[0])
	return splitRegexp(re, subject, limit)
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
	case object.String:
		return string(p) == " "
	default:
		return false
	}
}

// splitWhitespace implements awk-style whitespace splitting with an optional
// field limit.
func splitWhitespace(subject string, limit int) object.Value {
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
			out = append(out, object.String(subject[i:]))
			return &object.Array{Elems: out}
		}
		start := i
		for i < n && !isASCIISpace(subject[i]) {
			i++
		}
		out = append(out, object.String(subject[start:i]))
	}
	return &object.Array{Elems: out}
}

func isASCIISpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

// splitRegexp splits subject on matches of re, interpolating captured groups
// and honouring the field limit (see stringSplit).
func splitRegexp(re *Regexp, subject string, limit int) object.Value {
	if subject == "" {
		return &object.Array{Elems: []object.Value{}}
	}
	var out []object.Value
	last := 0    // byte offset of the start of the current field
	search := 0  // where the next match search begins
	pieces := 0  // count of delimiter-separated fields emitted (limit applies here)
	for search <= len(subject) {
		if limit > 0 && pieces+1 == limit {
			break
		}
		md := re.re.Match(subject[search:])
		if md == nil {
			break
		}
		mBegin := search + md.Begin(0)
		mEnd := search + md.End(0)
		if mEnd == mBegin {
			// Empty match: an empty match at the very start is skipped; otherwise
			// it ends the current character field. Advance one character.
			if mBegin >= len(subject) {
				break
			}
			if mBegin == last {
				_, w := utf8.DecodeRuneInString(subject[mBegin:])
				search = mBegin + w
				continue
			}
			out = append(out, object.String(subject[last:mBegin]))
			out = append(out, captureFields(md)...)
			pieces++
			last = mBegin
			_, w := utf8.DecodeRuneInString(subject[mBegin:])
			search = mBegin + w
			continue
		}
		out = append(out, object.String(subject[last:mBegin]))
		out = append(out, captureFields(md)...)
		pieces++
		last = mEnd
		search = mEnd
	}
	out = append(out, object.String(subject[last:]))
	if limit == 0 {
		// Strip trailing empty fields (the default behaviour).
		for len(out) > 0 {
			if s, ok := out[len(out)-1].(object.String); ok && s == "" {
				out = out[:len(out)-1]
				continue
			}
			break
		}
	}
	return &object.Array{Elems: out}
}

// captureFields returns the participating capture groups of a split delimiter
// match, in order. Non-participating groups are dropped (Ruby omits them).
func captureFields(md *onig.MatchData) []object.Value {
	var out []object.Value
	for i := 1; i <= md.NGroups(); i++ {
		if md.Begin(i) >= 0 {
			out = append(out, object.String(md.Str(i)))
		}
	}
	return out
}

// stringSub backs String#sub (global=false) and String#gsub (global=true). The
// first argument is the pattern (a Regexp, or a String matched literally). A
// replacement is given either as a second String argument (with backref
// templates) or as a block (yielded each match). The enumerator and Hash
// replacement forms are not yet supported.
func (vm *VM) stringSub(subject string, args []object.Value, blk *Proc, global bool) object.Value {
	re := scanRegexp(args[0])
	if blk != nil {
		return vm.gsub(re, subject, "", blk, global)
	}
	if len(args) < 2 {
		raise("ArgumentError", "wrong number of arguments (given 1, expected 2)")
	}
	return vm.gsub(re, subject, strArg(args[1]), nil, global)
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
	pos := 0    // byte cursor into subject (start of the not-yet-emitted tail)
	search := 0 // byte cursor where the next search begins
	for search <= len(subject) {
		md := re.re.Match(subject[search:])
		if md == nil {
			break
		}
		mBegin := search + md.Begin(0)
		mEnd := search + md.End(0)
		b.WriteString(subject[pos:mBegin]) // literal text before the match
		if blk != nil {
			res := vm.callBlock(blk, []object.Value{object.String(md.Str(0))})
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
	b.WriteString(subject[pos:]) // remaining tail
	return object.String(b.String())
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
// match (no groups) or the array of captures (one or more groups).
func scanElement(md *onig.MatchData) object.Value {
	n := md.NGroups()
	if n == 0 {
		return object.String(md.Str(0))
	}
	caps := make([]object.Value, n)
	for i := 1; i <= n; i++ {
		if md.Begin(i) < 0 {
			caps[i-1] = object.NilV
		} else {
			caps[i-1] = object.String(md.Str(i))
		}
	}
	return &object.Array{Elems: caps}
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

// groupValue returns group i of a match as a Ruby value: nil for a
// non-participating group, else the captured substring.
func groupValue(m *MatchData, i int) object.Value {
	if m.md.Begin(i) < 0 {
		return object.NilV
	}
	return object.String(m.md.Str(i))
}

// installRegexp registers the Regexp and MatchData method tables. It runs at the
// end of bootstrap so the classes already exist as constants.
func (vm *VM) installRegexp() {
	reArg := func(v object.Value) *Regexp { return v.(*Regexp) }

	vm.cRegexp.define("source", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(reArg(self).source)
	})
	vm.cRegexp.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(reArg(self).ToS())
	})
	vm.cRegexp.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(reArg(self).Inspect())
	})
	vm.cRegexp.define("match?", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if _, isNil := args[0].(object.Nil); isNil {
			return object.False
		}
		return object.Bool(reArg(self).re.MatchString(strArg(args[0])))
	})
	vm.cRegexp.define("match", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if _, isNil := args[0].(object.Nil); isNil {
			return object.NilV
		}
		return vm.runMatch(reArg(self), strArg(args[0]))
	})
	vm.cRegexp.define("=~", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.regexpMatchIndex(reArg(self), args[0])
	})
	vm.cRegexp.define("===", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		s, ok := stringLike(args[0])
		if !ok {
			return object.False
		}
		return object.Bool(reArg(self).re.MatchString(s))
	})

	mdArg := func(v object.Value) *MatchData { return v.(*MatchData) }

	vm.cMatchData.define("to_s", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(mdArg(self).md.Str(0))
	})
	vm.cMatchData.define("inspect", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(mdArg(self).Inspect())
	})
	vm.cMatchData.define("pre_match", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(mdArg(self).md.Pre())
	})
	vm.cMatchData.define("post_match", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.String(mdArg(self).md.Post())
	})
	vm.cMatchData.define("size", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(mdArg(self).md.NGroups() + 1)
	})
	vm.cMatchData.define("length", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Integer(mdArg(self).md.NGroups() + 1)
	})
	vm.cMatchData.define("to_a", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		out := make([]object.Value, 0, m.md.NGroups()+1)
		for i := 0; i <= m.md.NGroups(); i++ {
			out = append(out, groupValue(m, i))
		}
		return &object.Array{Elems: out}
	})
	vm.cMatchData.define("captures", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		out := make([]object.Value, 0, m.md.NGroups())
		for i := 1; i <= m.md.NGroups(); i++ {
			out = append(out, groupValue(m, i))
		}
		return &object.Array{Elems: out}
	})
	vm.cMatchData.define("begin", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return mdArg(self).offset(intArg(args[0]), false)
	})
	vm.cMatchData.define("end", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return mdArg(self).offset(intArg(args[0]), true)
	})
	vm.cMatchData.define("named_captures", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		m := mdArg(self)
		h := object.NewHash()
		for _, name := range namedGroups(m.re.source) {
			h.Set(object.String(name), groupValue(m, m.md.IndexOfName(name)))
		}
		return h
	})
	vm.cMatchData.define("[]", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return mdArg(self).at(args[0])
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
	md := re.re.Match(s)
	if md == nil {
		vm.lastMatch = object.NilV
		return object.NilV
	}
	vm.lastMatch = &MatchData{md: md, subject: s, re: re}
	return object.Integer(byteToChar(s, md.Begin(0)))
}

// stringLike returns the Go string for a String or Symbol receiver (the two
// types Ruby's Regexp matching coerces), and whether it was one.
func stringLike(v object.Value) (string, bool) {
	switch x := v.(type) {
	case object.String:
		return string(x), true
	case object.Symbol:
		return string(x), true
	default:
		return "", false
	}
}

// offset returns the character offset of group i's begin (end=false) or end
// (end=true), nil for a non-participating group, raising IndexError when i is
// out of range.
func (m *MatchData) offset(i int64, end bool) object.Value {
	if i < 0 || int(i) > m.md.NGroups() {
		raise("IndexError", "index %d out of matches", i)
	}
	var b int
	if end {
		b = m.md.End(int(i))
	} else {
		b = m.md.Begin(int(i))
	}
	if b < 0 {
		return object.NilV
	}
	return object.Integer(byteToChar(m.subject, b))
}

// at implements MatchData#[]: an Integer selects a group by index; a String or
// Symbol selects a named group (raising IndexError for an unknown name).
func (m *MatchData) at(key object.Value) object.Value {
	switch k := key.(type) {
	case object.Integer:
		idx := int(k)
		if idx < 0 || idx > m.md.NGroups() {
			return object.NilV
		}
		return groupValue(m, idx)
	case object.String:
		return m.byName(string(k))
	case object.Symbol:
		return m.byName(string(k))
	default:
		raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(key))
		return object.NilV
	}
}

// byName resolves a named-group capture, raising IndexError if no group has the
// name.
func (m *MatchData) byName(name string) object.Value {
	i := m.md.IndexOfName(name)
	if i < 0 {
		raise("IndexError", "undefined group name reference: %s", name)
	}
	return groupValue(m, i)
}
