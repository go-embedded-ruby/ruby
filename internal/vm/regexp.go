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

// runMatch matches re against subject, returning a MatchData value or nil.
func (vm *VM) runMatch(re *Regexp, subject string) object.Value {
	md := re.re.Match(subject)
	if md == nil {
		return object.NilV
	}
	return &MatchData{md: md, subject: subject, re: re}
}

// byteToChar converts a non-negative byte offset into the character offset Ruby
// reports. Callers guard against participating-group offsets before calling, so
// byteOff is always within s.
func byteToChar(s string, byteOff int) int {
	return utf8.RuneCountInString(s[:byteOff])
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
		return object.NilV
	}
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
