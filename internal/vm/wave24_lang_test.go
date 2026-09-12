package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

// runSrcEnc runs src through a fresh VM with the source encoding its magic
// comment declares, the way a loaded or required file is compiled.
func runSrcEnc(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	iseq, err := compiler.CompileWithEncoding(prog, compiler.MagicSourceEncoding(src))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var buf bytes.Buffer
	if _, err := New(&buf).Run(iseq); err != nil {
		t.Fatalf("run: %v", err)
	}
	return strings.TrimRight(buf.String(), "\n")
}

func checkSrc(t *testing.T, name, src, want string) {
	t.Helper()
	if got := runSrcEnc(t, src); got != want {
		t.Errorf("%s:\n got %q\nwant %q", name, got, want)
	}
}

// A compound assignment to an index evaluates the receiver and every index
// argument exactly once, and yields the assigned value rather than whatever
// `[]=` returns. Every expectation here was taken from MRI 4.0.5.
func TestIndexOpAssignEvaluatesOnce(t *testing.T) {
	checkSrc(t, "receiver and index evaluated once", `
$log = []
def rv(x); $log << :recv; x; end
def ix(x); $log << :idx; x; end
h = {}
rv(h)[ix(:a)] ||= 1
p $log
$log = []
rv(h)[ix(:a)] ||= 2      # the short-circuit path evaluates them once too
p [$log, h]
$log = []
rv(h)[ix(:a)] += 1
p [$log, h]
$log = []
rv(h)[ix(:a)] &&= 9
p [$log, h]
`, "[:recv, :idx]\n[[:recv, :idx], {a: 1}]\n[[:recv, :idx], {a: 2}]\n[[:recv, :idx], {a: 9}]")

	checkSrc(t, "the setter's return value is discarded", `
class C
  def initialize; @h = {}; end
  def []=(*a); @h[a[0...-1]] = a[-1]; :setter_return; end
  def [](*a); @h[a]; end
end
c = C.new
p(c[1] = 99)
p(c[*[1]] = 98)
p(c[1, 2] = 97)
p(c[*[1], 2] = 96)
p(c[3] ||= 5)
p(c[3] ||= 6)
p(c[4] += 1) rescue p $!.class
`, "99\n98\n97\n96\n5\n5\nTypeError")

	checkSrc(t, "a splatted index calls #to_a once", `
k = Object.new
def k.to_a; ($log ||= []) << :to_a; [:k]; end
$log = []
b = {}
p(b[*k] ||= 20)
p [$log, b]
p(b[*[:x], *[:y]] = 3)
`, "20\n[[:to_a], {k: 20}]\n3")

	checkSrc(t, "an attribute is read through one receiver evaluation", `
class A; attr_accessor :v; end
$log = []
def ra(x); $log << :recv; x; end
a = A.new
ra(a).v ||= 10
p [$log, a.v]
$log = []
ra(a).v ||= 20
p [$log, a.v]
$log = []
p(ra(a).v += 5)
p [$log, a.v]
$log = []
p(ra(a).v &&= 1)
p [$log, a.v]
`, "[[:recv], 10]\n[[:recv], 10]\n15\n[[:recv], 15]\n1\n[[:recv], 1]")

	// A hand-written read-then-write is NOT a compound assignment: Ruby evaluates
	// its two subtrees independently, so the receiver is evaluated twice. The
	// pointer-identity gate is what keeps them apart.
	checkSrc(t, "a spelled-out read and write still evaluates twice", `
$log = []
def rv(x); $log << :recv; x; end
h = {}
rv(h)[:a] = (rv(h)[:a] || 1)
p $log
`, "[:recv, :recv]")
}

// `Mod::C op= v` evaluates the module part once, and `||=` does not reach the
// right-hand side when the module part raises.
func TestScopedConstantOpAssignEvaluatesModuleOnce(t *testing.T) {
	checkSrc(t, "module part evaluated once", `
$VERBOSE = nil
module M; end
x = 0
(x += 1; M)::A ||= :assigned
p [x, M::A]
x = 0
(x += 1; M)::A ||= :again
p [x, M::A]
M::B = nil
x = 0
(x += 1; M)::B ||= :assigned
p [x, M::B]
M::D = 1
x = 0
p((x += 1; M)::D += 2)
p [x, M::D]
x = 0
p((x += 1; M)::D &&= 9)
p [x, M::D]
M::E = false
x = 0
p((x += 1; M)::E &&= 9)
p [x, M::E]
`, "[1, :assigned]\n[1, :assigned]\n[1, :assigned]\n3\n[1, 3]\n9\n[1, 9]\nfalse\n[1, false]")

	checkSrc(t, "a raising module part never reaches the right-hand side", `
module M2; end
x = 0
y = 0
begin
  (x += 1; raise "boom"; M2)::C ||= (y += 1; :assigned)
rescue => e
  p [x, y, e.message]
end
p defined?(M2::C)
`, `[1, 0, "boom"]`+"\nnil")

	// A leading `::C` has no module part, so it keeps the ordinary path.
	checkSrc(t, "a toplevel-qualified constant still assigns", `
$VERBOSE = nil
::TOPC ||= 1
::TOPC ||= 2
p ::TOPC
`, "1")
}

// Reassigning an already-initialized constant warns, qualified by the module
// unless that module is Object, and `$VERBOSE = nil` silences it — MRI's
// rb_warn behaviour. (rbgo leaves $VERBOSE unset, where MRI starts it at false,
// so these set it explicitly to reach the warning at all.)
func TestAlreadyInitializedConstantWarning(t *testing.T) {
	checkSrc(t, "toplevel constant", "$VERBOSE = false\nX = 1\nX = 2\np X\n",
		"warning: already initialized constant X\n2")
	checkSrc(t, "qualified constant", "$VERBOSE = false\nmodule M; end\nM::Y = 1\nM::Y = 2\np M::Y\n",
		"warning: already initialized constant M::Y\n2")
	checkSrc(t, "inside a class body", "$VERBOSE = false\nclass K; Z = 1; Z = 2; end\np K::Z\n",
		"warning: already initialized constant K::Z\n2")
	checkSrc(t, "silenced by $VERBOSE = nil", "$VERBOSE = nil\nX = 1\nX = 2\np X\n", "2")
	checkSrc(t, "a first assignment does not warn", "$VERBOSE = false\nmodule M; end\nM::Q = 1\np M::Q\n", "1")
}

// A two-sided range in condition position is the flip-flop operator. Every
// expectation here was taken from MRI 4.0.5.
func TestFlipFlop(t *testing.T) {
	checkSrc(t, "inclusive and exclusive ends", `
$s = []; 10.times { |i| $s << i if (i == 4)..(i == 4) }; p $s
$s = []; 10.times { |i| $s << i if (i == 4)..(i == 7) }; p $s
$s = []; 10.times { |i| $s << i if (i == 4)...(i == 4) }; p $s
$s = []; 10.times { |i| $s << i if (i == 4)...(i == 5) }; p $s
`, "[4]\n[4, 5, 6, 7]\n[4, 5, 6, 7, 8, 9]\n[4, 5]")

	checkSrc(t, "combined with or, and negated by unless", `
$s = []; 10.times { |i| $s << i if (i == 4)...(i == 5) or (i == 7)...(i == 8) }; p $s
$s = []; 10.times { |i| $s << i unless (i == 4)..(i == 7) }; p $s
$s = []; 10.times { |i| $s << i if (i == 4)..(i == 7) and i.odd? }; p $s
`, "[4, 5, 7, 8]\n[0, 1, 2, 3, 8, 9]\n[5, 7]")

	checkSrc(t, "each side is evaluated lazily", `
$s = []; c = proc { |i| $s << i }; 10.times { |i| i if c[i]...false }; p $s
$s = []; c = proc { |i| $s << i }; 10.times { |i| i if c[i]..false }; p $s
$s = []; c = proc { |i| $s << i }; 10.times { |i| i if (i == 4)...c[i] }; p $s
$s = []; c = proc { |i| $s << i }; 10.times { |i| i if (i == 4)..c[i] }; p $s
`, "[0]\n[0]\n[5]\n[4]")

	checkSrc(t, "state is per flip-flop and shared across calls of one proc", `
$s = []
store_me = proc { |i| $s << i if (i == 4)..(i == 7) }
store_me[1]; store_me[4]; proc { store_me[1] }.call; store_me[7]; store_me[5]
p $s
`, "[4, 1, 7]")

	checkSrc(t, "two identical flip-flops do not interfere", `
$s = []
a = eval "proc { |i| $s << i if (i == 4)..(i == 7) }"
b = eval "proc { |i| $s << i if (i == 4)..(i == 7) }"
6.times(&a); 6.times(&b); p $s
`, "[4, 5, 4, 5]")

	checkSrc(t, "in a while condition and in a ternary", `
$s = []; i = 0; while i < 10; $s << i if (i == 3)..(i == 5); i += 1; end; p $s
$s = []; i = 0; until (i == 3)..(i == 5); $s << i; i += 1; end; p $s
$s = []; 6.times { |i| $s << ((i == 2)..(i == 3) ? :on : :off) }; p $s
`, "[3, 4, 5]\n[0, 1, 2]\n[:off, :off, :on, :on, :off, :off]")

	// A one-sided range in condition position is still a Range value, which is
	// truthy — not a flip-flop.
	checkSrc(t, "a one-sided range is a Range, not a flip-flop", `
p((1..5).to_a)
x = 1..5; p x.class
$s = []; 3.times { |i| $s << i if (i..) }; p $s
`, "[1, 2, 3, 4, 5]\nRange\n[0, 1, 2]")
}

// __ENCODING__ and string literals take the encoding a magic comment declares,
// in every spelling MRI accepts.
func TestMagicCommentSourceEncoding(t *testing.T) {
	checkSrc(t, "plain", "# encoding: big5\np __ENCODING__.name\np \"abc\".encoding.name\n",
		`"Big5"`+"\n"+`"Big5"`)
	checkSrc(t, "case-insensitive", "# CoDiNg:   bIg5\np __ENCODING__.name\n", `"Big5"`)
	checkSrc(t, "after a shebang", "#!/usr/bin/ruby\n# encoding: big5\np __ENCODING__.name\n", `"Big5"`)
	checkSrc(t, "Emacs style", "# -*- encoding: big5 -*-\np __ENCODING__.name\n", `"Big5"`)
	checkSrc(t, "vim style", "# vim: filetype=ruby, fileencoding=big5, tabsize=3\np __ENCODING__.name\n", `"Big5"`)
	checkSrc(t, "no magic comment", "p __ENCODING__.name\n", `"UTF-8"`)
	checkSrc(t, "utf-8 is the default", "# encoding: utf-8\np __ENCODING__.name\n", `"UTF-8"`)
	checkSrc(t, "not the first token", "puts 1 # encoding: big5\np __ENCODING__.name\n", "1\n"+`"UTF-8"`)
	checkSrc(t, "binary", "# encoding: binary\np __ENCODING__.name\np \"abc\".encoding.name\n",
		`"ASCII-8BIT"`+"\n"+`"ASCII-8BIT"`)
}
