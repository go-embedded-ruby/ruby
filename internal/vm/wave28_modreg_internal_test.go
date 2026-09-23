// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"os"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// writeTestFile writes a fixture file for the autoload tests below.
func writeTestFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}

// rescueRuby runs fn and reports the Ruby exception class and message it raised,
// or ("", "") when it returned normally. It lets the whitebox tests below reach
// the raise-only branches of the wave-28 helpers without going through a script.
func rescueRuby(fn func()) (class, msg string) {
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(RubyError); ok {
				class, msg = re.Class, re.Message
				return
			}
			panic(r)
		}
	}()
	fn()
	return "", ""
}

func TestPatternNeedsLeftContext(t *testing.T) {
	tests := []struct {
		src  string
		want bool
	}{
		{"", false},
		{"abc", false},
		{"a$", false},
		{`a\z`, false},
		{`\d+`, false},
		{"^a", true},
		{"a^b", true},      // conservative: not the class-negation slot
		{"[^a]", false},    // the negation slot is not an anchor
		{`\^`, false},      // an escaped caret is a literal
		{`\A`, true},       // beginning-of-string
		{`\b`, true},       // word boundary
		{`\B`, true},       // non-boundary
		{`\K`, true},       // keep operator
		{`\G`, false},      // deliberately excluded (see searchFrom)
		{`\\A`, false},     // an escaped backslash, then a literal A
		{"(?<=a)", true},   // look-behind
		{"(?<!a)", true},   // negative look-behind
		{"(?<n>a)", false}, // a NAMED group is not look-behind
		{"(?:a)", false},   // a plain group is not look-behind
		{`a\`, false},      // a trailing backslash consumes nothing
		{"[a^b]", true},    // conservative over-report inside a class
		{"(?<", false},     // truncated look-behind prefix
	}
	for _, tc := range tests {
		if got := patternNeedsLeftContext(tc.src); got != tc.want {
			t.Errorf("patternNeedsLeftContext(%q) = %v, want %v", tc.src, got, tc.want)
		}
	}
}

func TestSearchFromBoundsAndPaths(t *testing.T) {
	vm := New(&strings.Builder{})

	// Out-of-range cursors yield no match and echo the cursor back as the base.
	plain := vm.compileRegexp("a", "").(*Regexp)
	if md, base := plain.searchFrom("abc", -1); md != nil || base != -1 {
		t.Fatalf("searchFrom(-1) = %v, %d; want nil, -1", md, base)
	}
	if md, base := plain.searchFrom("abc", 4); md != nil || base != 4 {
		t.Fatalf("searchFrom(4) = %v, %d; want nil, 4", md, base)
	}

	// The fast path slices the tail, so offsets come back relative to the cursor.
	md, base := plain.searchFrom("xxa", 1)
	if md == nil || base != 1 || base+md.Begin(0) != 2 {
		t.Fatalf("fast path: base=%d begin=%v", base, md)
	}
	if md, _ := plain.searchFrom("xxx", 0); md != nil {
		t.Fatalf("fast path should not match")
	}

	// A left-context pattern takes the anchored probe, which reports absolute
	// offsets (base 0) and sees the real prefix.
	anchored := vm.compileRegexp(`\A`, "").(*Regexp)
	if md, base := anchored.searchFrom("abc", 1); md != nil {
		t.Fatalf("\\A must not match from 1, got %v (base %d)", md, base)
	}
	if md, base := anchored.searchFrom("abc", 0); md == nil || base != 0 {
		t.Fatalf("\\A must match at 0, got %v (base %d)", md, base)
	}
	// The probe must advance by whole characters and reach the very end.
	bound := vm.compileRegexp(`\b`, "").(*Regexp)
	if md, base := bound.searchFrom("éa", 0); md == nil || base != 0 || md.Begin(0) != 0 {
		t.Fatalf("\\b over a multi-byte prefix: %v base=%d", md, base)
	}
	if md, _ := bound.searchFrom("...", 0); md != nil {
		t.Fatalf("\\b must not match inside punctuation only")
	}
	// The probe must walk PAST a failing start and stop at the end of the string.
	head := vm.compileRegexp(`\Ab`, "").(*Regexp)
	if md, _ := head.searchFrom("ab", 0); md != nil {
		t.Fatalf("\\Ab must not match \"ab\" from any start")
	}
}

func TestRewriteBeginLineAnchor(t *testing.T) {
	tests := []struct{ src, want string }{
		{"abc", "abc"},                                       // no caret at all
		{"^a", beginLineEquivalent + "a"},                    // the anchor
		{`\^a`, `\^a`},                                       // escaped: left alone
		{"[^a]", "[^a]"},                                     // class negation
		{"[[:alpha:]^]", "[[:alpha:]^]"},                     // nested class keeps its caret
		{"[a-z&&[^b]]", "[a-z&&[^b]]"},                       // nested negation
		{"(?#a^b)^c", "(?#a^b)" + beginLineEquivalent + "c"}, // a comment is copied whole
		{"(?#unterminated^", "(?#unterminated^"},             // no closing paren: copy the rest
		{"]^a", "]" + beginLineEquivalent + "a"},             // a stray ] must not go negative
		{`a\`, `a\`},                                         // trailing backslash, no caret
	}
	for _, tc := range tests {
		if got := rewriteBeginLineAnchor(tc.src); got != tc.want {
			t.Errorf("rewriteBeginLineAnchor(%q) = %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestBeginLineAnchorSemantics(t *testing.T) {
	// The whole point of the rewrite: "^" does not fire at the end of a string
	// that ends in a newline (regexec.c OP_BEGIN_LINE), but does on the empty
	// string, whose start is its end.
	tests := []struct{ src, want string }{
		{`p "Text\n".gsub(/^/, " ")`, "\" Text\\n\"\n"},
		{`p "a\nb\n".gsub(/^/, "-")`, "\"-a\\n-b\\n\"\n"},
		{`p "".gsub(/^/, "-")`, "\"-\"\n"},
		{`p "a\nb".gsub(/^/, "-")`, "\"-a\\n-b\"\n"},
		{`p("a\nb" =~ /^b/)`, "2\n"},
		{`p("abc" =~ /^b/)`, "nil\n"},
		{`p(/^a[b^c]d$/.to_s)`, "\"(?-mix:^a[b^c]d$)\"\n"},
	}
	for _, tc := range tests {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestSearchFromScriptSemantics(t *testing.T) {
	// Each of these was wrong while the engine was handed subject[pos:].
	tests := []struct{ src, want string }{
		{`p "ab cd".scan(/\b\w/)`, "[\"a\", \"c\"]\n"},
		{`p "a\nb\nc".split(/^/)`, "[\"a\\n\", \"b\\n\", \"c\"]\n"},
		{`p "hello".index(/\A/, 2)`, "nil\n"},
		{`p "hello world".match(/\Ao/, 4)`, "nil\n"},
		{`p "xay".byteindex(/\A/, 1)`, "nil\n"},
		{`p "aaa".gsub(/\A/, "-")`, "\"-aaa\"\n"},
		{`p "a1b2".gsub(/\b/, "|")`, "\"|a1b2|\"\n"},
		{`p "ab".gsub(/(?<=a)/, "-")`, "\"a-b\"\n"},
		{`p "abc".scan(/\A./)`, "[\"a\"]\n"},
		// The Hash form of gsub goes through the same loop.
		{`p "ab cd".gsub(/\b/, "" => "|")`, "\"|ab| |cd|\"\n"},
		// And the ordinary, context-free patterns must be untouched.
		{`p "a,b,,c".split(/,/)`, "[\"a\", \"b\", \"\", \"c\"]\n"},
		{`p "hello".scan(/l/)`, "[\"l\", \"l\"]\n"},
		{`p "one two".sub(/(\w+) (\w+)/, "\\2 \\1")`, "\"two one\"\n"},
	}
	for _, tc := range tests {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestControlEscapeHelpers(t *testing.T) {
	if _, ok := controlEscapeValue(0x80); ok {
		t.Error("a byte at 0x80 is not a control escape payload")
	}
	if v, ok := controlEscapeValue('?'); !ok || v != 0x7F {
		t.Errorf("\\c? = %#x, %v; want 0x7f, true", v, ok)
	}
	if v, ok := controlEscapeValue('A'); !ok || v != 0x01 {
		t.Errorf("\\cA = %#x, %v; want 0x01, true", v, ok)
	}
	if v, ok := controlEscapeValue('#'); !ok || v != 0x03 {
		t.Errorf("\\c# = %#x, %v; want 0x03, true", v, ok)
	}

	if _, _, ok := controlEscapePayload(""); ok {
		t.Error("an empty payload is not readable")
	}
	if _, _, ok := controlEscapePayload(`\`); ok {
		t.Error("a lone backslash payload is not readable")
	}
	if v, w, ok := controlEscapePayload(`\\`); !ok || w != 2 || v != 0x1C {
		t.Errorf("\\c\\ = %#x, %d, %v; want 0x1c, 2, true", v, w, ok)
	}
	if v, w, ok := controlEscapePayload("A"); !ok || w != 1 || v != 0x01 {
		t.Errorf("\\cA = %#x, %d, %v; want 0x01, 1, true", v, w, ok)
	}
	if _, _, ok := controlEscapePayload("\u00e9"); ok {
		t.Error("a multi-byte payload is not a control escape")
	}

	if got := hexEscape(0x0a); got != `\x0a` {
		t.Errorf("hexEscape(0x0a) = %q", got)
	}
	if !isASCIILetter('a') || !isASCIILetter('Z') || isASCIILetter('1') {
		t.Error("isASCIILetter misclassified")
	}
}

func TestTranslateCharEscapes(t *testing.T) {
	tests := []struct{ src, want string }{
		{"abc", "abc"},             // no backslash
		{`\d+`, `\d+`},             // a meaningful letter is left alone
		{`\k<n>`, `\k<n>`},         // group reference
		{`\cA`, `\x01`},            // control
		{`\C-J`, `\x0a`},           // control, \C- spelling
		{`\c\\`, `\x1c`},           // the payload is itself escaped
		{`\C-\\`, `\x1c`},          //
		{`\0`, `\x00`},             // octal
		{`\101`, `\101`},           // outside a class: a back-reference, untouched
		{`[\1]`, `[\x01]`},         // inside a class: octal
		{`[\000-\b]`, `[\x00-\b]`}, //
		{`(a)\1`, `(a)\1`},         // a real back-reference survives
		{`\y`, `y`},                // a meaningless letter loses its backslash
		{`\Q`, `Q`},                //
		{`\400`, `\400`},           // above 0x7f: left as written
		{`a\`, `a\`},               // trailing backslash
		{`\c`, `\c`},               // truncated \c
		{`\C`, `\C`},               // truncated \C-
		{`\C-`, `\C-`},             //
		{`\-`, `\-`},               // a punctuation escape is not a letter
		{`[a]\0`, `[a]\x00`},       // the class depth returns to zero
		{`]\1`, `]\1`},             // a stray ] must not make the depth negative
		{"\\c\u00e9", "\\c\u00e9"}, // an unusable control payload is copied
	}
	for _, tc := range tests {
		if got := translateCharEscapes(tc.src); got != tc.want {
			t.Errorf("translateCharEscapes(%q) = %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestCharEscapeSemantics(t *testing.T) {
	tests := []struct{ src, want string }{
		{`p(/\c#\cc\cC/ =~ "\x03\x03\x03")`, "0\n"},
		{`p(/\C-*\C-J\C-j/ =~ "\n\n\n")`, "0\n"},
		{`p(/[\000-\b]/ =~ "\x00")`, "0\n"},
		{`p(/\y/ =~ "y")`, "0\n"},
		{`p(/(a)\1/ =~ "aa")`, "0\n"},
		{`p(/\d\w\s/ =~ "1a ")`, "0\n"},
	}
	for _, tc := range tests {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestUnpackOffsetKeyword(t *testing.T) {
	tests := []struct{ src, want string }{
		{`p "abc".unpack("C*", offset: 1)`, "[98, 99]\n"},
		{`p "a".unpack("C", offset: 1)`, "[nil]\n"},
		{`p "a".unpack1("C", offset: 1)`, "nil\n"},
		{`p "abc".unpack("C*")`, "[97, 98, 99]\n"},
		{`p "abc".unpack1("C")`, "97\n"},
		// The offset counts bytes, not characters.
		{`p "\u0608".unpack1("C", offset: 1)`, "136\n"},
		// nil is "no offset given".
		{`p "abc".unpack("C", offset: nil)`, "[97]\n"},
		// An object answering #to_int is accepted, as NUM2LONG does.
		{"class O; def to_int; 1; end; end\np \"abc\".unpack(\"C\", offset: O.new)", "[98]\n"},
		{`begin; "a".unpack("C", offset: 2); rescue ArgumentError => e; p e.message; end`,
			"\"offset outside of string\"\n"},
		{`begin; "a".unpack1("C", offset: -1); rescue ArgumentError => e; p e.message; end`,
			"\"offset can't be negative\"\n"},
		{`begin; "a".unpack("C", bogus: 1); rescue ArgumentError => e; p e.message; end`,
			"\"unknown keyword: :bogus\"\n"},
	}
	for _, tc := range tests {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestUnpackOffsetDirect(t *testing.T) {
	vm := New(&strings.Builder{})
	// No trailing Hash, and a lone Hash argument (the format itself) are both 0.
	if got := vm.unpackOffset([]object.Value{object.NewString("C")}, 3); got != 0 {
		t.Errorf("no keyword = %d, want 0", got)
	}
	h := object.NewHash()
	h.Set(object.Symbol("offset"), object.IntValue(2))
	if got := vm.unpackOffset([]object.Value{object.NewString("C"), h}, 3); got != 2 {
		t.Errorf("offset keyword = %d, want 2", got)
	}
	// A non-Symbol key is an unknown keyword too.
	bad := object.NewHash()
	bad.Set(object.NewString("offset"), object.IntValue(1))
	class, _ := rescueRuby(func() {
		vm.unpackOffset([]object.Value{object.NewString("C"), bad}, 3)
	})
	if class != "ArgumentError" {
		t.Errorf("a String key raised %q, want ArgumentError", class)
	}
}

func TestRefinementImportMethods(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"imports", `
			su = Module.new do
			  def indent(level); " " * level + self; end
			end
			R = Module.new do
			  refine String do
			    import_methods su
			  end
			end
			module S; using R; p "foo".indent(3); end`, "\"   foo\"\n"},
		{"last_module_wins", `
			a = Module.new { def tag; "a"; end }
			b = Module.new { def tag; "b"; end }
			R = Module.new { refine(String) { import_methods a, b } }
			module S; using R; p "x".tag; end`, "\"b\"\n"},
		{"methods_see_each_other", `
			su = Module.new do
			  def indent(level); " " * level + self; end
			  def dotted(level); indent(level) + "."; end
			end
			R = Module.new { refine(String) { import_methods su } }
			module S; using R; p "foo".dotted(2); end`, "\"  foo.\"\n"},
		{"owner_is_the_refinement", `
			su = Module.new { def tag; "t"; end }
			R = Module.new { refine(String) { import_methods su } }
			module S
			  using R
			  p String.instance_method(:tag).owner == R.refinements.first
			end`, "true\n"},
		{"no_class_methods", `
			su = Module.new { def self.tag; "t"; end }
			R = Module.new { refine(String) { import_methods su } }
			module S
			  using R
			  p String.instance_methods.include?(:tag)
			end`, "false\n"},
		{"visibility_carried", `
			su = Module.new do
			  def pub; "p"; end
			  private
			  def priv; "q"; end
			end
			R = Module.new { refine(String) { import_methods su } }
			module S
			  using R
			  p "x".pub
			  begin; "x".priv; rescue NoMethodError; p :private; end
			end`, "\"p\"\n:private\n"},
		{"type_error", `
			R = Module.new do
			  refine String do
			    begin; import_methods Integer; rescue TypeError => e; p e.message; end
			  end
			end`, "\"wrong argument type Class (expected Module)\"\n"},
		{"arity", `
			R = Module.new do
			  refine String do
			    begin; import_methods; rescue ArgumentError => e; p e.message; end
			  end
			end`, "\"wrong number of arguments (given 0, expected 1+)\"\n"},
		{"not_ruby_code", `
			R = Module.new do
			  refine String do
			    begin; import_methods Kernel; rescue ArgumentError => e; p e.class; end
			  end
			end`, "ArgumentError\n"},
		{"nothing_imported_when_one_arg_is_bad", `
			su = Module.new { def tag; "t"; end }
			R = Module.new do
			  refine String do
			    begin; import_methods su, Integer; rescue TypeError; end
			  end
			end
			module S
			  using R
			  begin; "x".tag; rescue NoMethodError; p :none; end
			end`, ":none\n"},
		{"warns_about_ancestors", `
			$VERBOSE = true
			M1 = Module.new
			M2 = Module.new { include M1 }
			R = Module.new { refine(String) { import_methods M2 } }
			p :done`, "warning: M2 has ancestors, but Refinement#import_methods doesn't import their methods\n:done\n"},
		{"undefined_entries_are_skipped", `
			su = Module.new do
			  def gone; end
			  undef_method :gone
			  def kept; "k"; end
			end
			R = Module.new { refine(String) { import_methods su } }
			module S; using R; p "x".kept; end`, "\"k\"\n"},
		{"returns_the_refinement", `
			su = Module.new { def tag; "t"; end }
			R = Module.new do
			  refine String do
			    p import_methods(su).equal?(self)
			  end
			end`, "true\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestImportMethodsRejectsNonRefinementSelf(t *testing.T) {
	src := `
	  begin
	    Module.new.send(:import_methods, Module.new)
	  rescue NoMethodError, TypeError => e
	    p e.class
	  end`
	if got := eval(t, src); got != "NoMethodError\n" && got != "TypeError\n" {
		t.Errorf("got %q, want NoMethodError or TypeError", got)
	}
}

func TestImportMethodsOnNonRefinementSelf(t *testing.T) {
	// import_methods is only ever reached with a Refinement as self, so this
	// guard is defensive: call the body directly to exercise it.
	vm := New(&strings.Builder{})
	class, msg := rescueRuby(func() {
		refinementImportMethods(vm, object.IntValue(1), []object.Value{vm.cString}, nil)
	})
	if class != "TypeError" || !strings.Contains(msg, "expected Refinement") {
		t.Errorf("non-refinement self raised %q / %q", class, msg)
	}
	// A plain RClass that is not a refinement takes the same branch.
	class, _ = rescueRuby(func() {
		refinementImportMethods(vm, vm.cString, []object.Value{vm.cString}, nil)
	})
	if class != "TypeError" {
		t.Errorf("a non-refinement module raised %q, want TypeError", class)
	}
}

func TestImportMethodsPinsLexicalScope(t *testing.T) {
	// A method written in a NAMED module body carries no explicit lexScope, so
	// the import must pin it to the source module for constant lookup. CONST is
	// resolvable only from SrcMod, never from the refinement.
	src := `
	  module SrcMod
	    CONST = "from-src"
	    def tagged; CONST; end
	  end
	  R = Module.new { refine(String) { import_methods SrcMod } }
	  module S
	    using R
	    p "x".tagged
	  end`
	if got := eval(t, src); got != "\"from-src\"\n" {
		t.Errorf("imported method constant lookup = %q", got)
	}
}

func TestDefinedWithRubyCodeAndSortedNames(t *testing.T) {
	if definedWithRubyCode(&Method{}) {
		t.Error("a method with no iseq is not Ruby code")
	}
	if definedWithRubyCode(&Method{native: func(*VM, object.Value, []object.Value, *Proc) object.Value { return object.NilV }}) {
		t.Error("a native method is not Ruby code")
	}
	if definedWithRubyCode(&Method{proc: &Proc{}}) {
		t.Error("a define_method body is not Ruby code")
	}
	names := sortedMethodNames(map[string]*Method{"b": {}, "a": {}, "c": {}})
	if strings.Join(names, ",") != "a,b,c" {
		t.Errorf("sortedMethodNames = %v, want a,b,c", names)
	}
	if len(sortedMethodNames(nil)) != 0 {
		t.Error("sortedMethodNames(nil) must be empty")
	}
}

func TestCallerRefinedInstanceMethod(t *testing.T) {
	vm := New(&strings.Builder{})
	// Nothing has ever called refine: the fast exit.
	if m := vm.callerRefinedInstanceMethod(vm.cString, "upcase"); m != nil {
		t.Error("no refinements anywhere must yield nil")
	}
	vm.anyRefinements = true
	if m := vm.callerRefinedInstanceMethod(nil, "upcase"); m != nil {
		t.Error("a nil module must yield nil")
	}
	// No frame cref recorded (a bare native call at bootstrap).
	if m := vm.callerRefinedInstanceMethod(vm.cString, "upcase"); m != nil {
		t.Error("no frame cref must yield nil")
	}
	vm.frameCrefs = append(vm.frameCrefs, nil)
	if m := vm.callerRefinedInstanceMethod(vm.cString, "upcase"); m != nil {
		t.Error("a nil cref must yield nil")
	}
	vm.frameCrefs[len(vm.frameCrefs)-1] = vm.cObject
	if m := vm.callerRefinedInstanceMethod(vm.cString, "upcase"); m != nil {
		t.Error("a scope with no active refinement must yield nil")
	}
}

func TestInstanceMethodHonoursRefinements(t *testing.T) {
	tests := []struct{ src, want string }{
		{`
		  class K; end
		  module R; refine(K) { def foo; "r"; end }; end
		  module S
		    using R
		    p K.instance_method(:foo).class
		    p K.instance_method(:foo).owner == R.refinements.first
		  end`, "UnboundMethod\ntrue\n"},
		// Outside the using scope the refinement is not visible.
		{`
		  class K; end
		  module R; refine(K) { def foo; "r"; end }; end
		  begin; K.instance_method(:foo); rescue NameError; p :none; end`, ":none\n"},
		// The class's OWN definition wins over a lower-priority refinement.
		{`
		  class K; def foo; "k"; end; end
		  module R; refine(K) { def bar; "r"; end }; end
		  module S
		    using R
		    p K.instance_method(:foo).owner
		  end`, "K\n"},
		// An active refinement of a DIFFERENT class is skipped, and a name no
		// refinement and no ancestor defines still raises.
		{`
		  class K; end
		  class J; end
		  module R
		    refine(J) { def foo; "j"; end }
		    refine(K) { def bar; "k"; end }
		  end
		  module S
		    using R
		    p K.instance_method(:bar).owner == R.refinements[R.refinements.index(R.refinements.find { |m| m.target == K })]
		    begin; K.instance_method(:nope); rescue NameError; p :none; end
		  end`, "true\n:none\n"},
	}
	for _, tc := range tests {
		if got := eval(t, tc.src); got != tc.want {
			t.Errorf("%s => %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestAutoloadVisibleDirect(t *testing.T) {
	vm := New(&strings.Builder{})
	if vm.autoloadVisible(nil, "X") {
		t.Error("a nil class has no autoload")
	}
	if vm.autoloadVisible(vm.cObject, "NoSuchConstantHere") {
		t.Error("a class with no autoload table has none")
	}
	mod := newClass("AV", nil)
	mod.isModule = true
	mod.autoloads = map[string]string{}
	if vm.autoloadVisible(mod, "X") {
		t.Error("an empty autoload table has no entry")
	}
	// A path that resolves to no file on disk was never loaded, so the entry is
	// visible.
	mod.autoloads["X"] = "rbgo_wave28_no_such_feature"
	if !vm.autoloadVisible(mod, "X") {
		t.Error("an unloaded feature must leave the entry visible")
	}
}

func TestAutoloadInProgressIsInvisible(t *testing.T) {
	dir := t.TempDir()
	feature := dir + "/rbgo_w28_al.rb"
	if err := writeTestFile(feature, "$probe = [defined?(MZ::K), MZ.autoload?(:K), MZ.const_defined?(:K), MZ.constants(false).include?(:K)]\nmodule MZ; K = 1; end\n"); err != nil {
		t.Fatal(err)
	}
	src := `
	  module MZ; end
	  MZ.autoload :K, ` + object.NewString(feature).Inspect() + `
	  require ` + object.NewString(feature).Inspect() + `
	  p $probe`
	// From inside the file, the constant is undefined and carries no autoload,
	// but it is still listed in Module#constants.
	if got := eval(t, src); got != "[nil, nil, false, true]\n" {
		t.Errorf("in-progress autoload probe = %q, want [nil, nil, false, true]", got)
	}
}

func TestWarnAutoloadDidNotDefine(t *testing.T) {
	dir := t.TempDir()
	feature := dir + "/rbgo_w28_empty.rb"
	if err := writeTestFile(feature, "# defines nothing\n"); err != nil {
		t.Fatal(err)
	}
	lit := object.NewString(feature).Inspect()

	// Not verbose: silent.
	quiet := `
	  require "stringio"
	  $stderr = StringIO.new
	  module MQ; end
	  MQ.autoload :MISSING, ` + lit + `
	  begin; MQ::MISSING; rescue NameError; end
	  out = $stderr.string
	  $stderr = STDERR
	  p out`
	if got := eval(t, quiet); got != "\"\"\n" {
		t.Errorf("non-verbose warning = %q, want empty", got)
	}

	// Verbose: the module-qualified form.
	loud := `
	  require "stringio"
	  $VERBOSE = true
	  $stderr = StringIO.new
	  module ML; end
	  ML.autoload :MISSING, ` + lit + `
	  begin; ML::MISSING; rescue NameError; end
	  out = $stderr.string
	  $stderr = STDERR
	  p out.include?("Expected ") && out.include?("to define ML::MISSING but it didn't")`
	if got := eval(t, loud); got != "true\n" {
		t.Errorf("verbose warning = %q, want true", got)
	}

	// On Object the name is NOT qualified.
	top := `
	  require "stringio"
	  $VERBOSE = true
	  $stderr = StringIO.new
	  autoload :MISSING_AT_TOP, ` + lit + `
	  begin; MISSING_AT_TOP; rescue NameError; end
	  out = $stderr.string
	  $stderr = STDERR
	  p out.include?("to define MISSING_AT_TOP but it didn't")`
	if got := eval(t, top); got != "true\n" {
		t.Errorf("top-level warning = %q, want true", got)
	}
}

func TestWarnAutoloadDidNotDefineSkipsWhenDefined(t *testing.T) {
	vm := New(&strings.Builder{})
	mod := newClass("WD", nil)
	mod.isModule = true
	mod.consts["K"] = object.IntValue(1)
	// The constant IS defined, so nothing is written even in verbose mode.
	vm.globals["$VERBOSE"] = object.Bool(true)
	vm.warnAutoloadDidNotDefine(mod, "K", "feature.rb")
}
