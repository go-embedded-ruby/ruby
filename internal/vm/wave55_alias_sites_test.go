// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestBuiltinSecondNameReportsItself: MRI gives most of the pairs that look like
// built-in aliases a SECOND rb_define_method over the same C function rather
// than an rb_define_alias, so the new name is its own #original_name while #==
// still answers true (rb_method_definition_eq compares the shared cfunc).
//
// Sharing one *Method record, as aliasBuiltin does, gets #== right and
// #original_name wrong. Measured across all 80 reachable call sites, 61 reported
// the wrong #original_name; after the per-site conversion, 0 do. These rows are
// a sample spanning the classes involved, each the byte-for-byte answer of
// ruby 4.0.5.
func TestBuiltinSecondNameReportsItself(t *testing.T) {
	src := `im = ->(c, n) { c.instance_method(n) }
[[Hash, :length, :size], [Hash, :has_key?, :key?], [Array, :collect, :map],
 [Array, :find_index, :index], [Proc, :yield, :call], [Proc, :eql?, :==],
 [Object, :fail, :raise], [Object, :format, :sprintf],
 [String, :size, :length], [String, :slice, :[]], [Symbol, :size, :length],
 [Method, :eql?, :==], [UnboundMethod, :eql?, :==], [Module, :class_eval, :module_eval],
 [Struct, :length, :size], [Range, :entries, :to_a], [MatchData, :length, :size],
 [Numeric, :rect, :rectangular], [Numeric, :phase, :arg], [Float, :quo, :fdiv],
 [Integer, :next, :succ]].each do |c, n, o|
  puts "#{c}##{n} orig=#{im.(c, n).original_name} eq=#{im.(c, n) == im.(c, o)}"
end
`
	const want = `Hash#length orig=length eq=true
Hash#has_key? orig=has_key? eq=true
Array#collect orig=collect eq=true
Array#find_index orig=find_index eq=true
Proc#yield orig=yield eq=true
Proc#eql? orig=eql? eq=true
Object#fail orig=fail eq=true
Object#format orig=format eq=true
String#size orig=size eq=true
String#slice orig=slice eq=true
Symbol#size orig=size eq=true
Method#eql? orig=eql? eq=true
UnboundMethod#eql? orig=eql? eq=true
Module#class_eval orig=class_eval eq=true
Struct#length orig=length eq=true
Range#entries orig=entries eq=true
MatchData#length orig=length eq=true
Numeric#rect orig=rect eq=true
Numeric#phase orig=phase eq=true
Float#quo orig=quo eq=true
Integer#next orig=next eq=true
`
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestGenuineBuiltinAliasesReportTheOldName is the other half: the 20 sites that
// really ARE rb_define_alias, where MRI reports the OLD name. The two helpers
// differ in exactly this observable, so a test for one without the other would
// pass on a blanket conversion in either direction.
func TestGenuineBuiltinAliasesReportTheOldName(t *testing.T) {
	src := `im = ->(c, n) { c.instance_method(n) }
[[Numeric, :imag, :imaginary], [Numeric, :conj, :conjugate], [Integer, :inspect, :to_s],
 [Float, :inspect, :to_s], [Float, :magnitude, :abs], [Hash, :to_s, :inspect],
 [Module, :inspect, :to_s], [Proc, :inspect, :to_s], [Symbol, :id2name, :to_s],
 [Set, :length, :size], [MatchData, :deconstruct, :captures]].each do |c, n, o|
  puts "#{c}##{n} orig=#{im.(c, n).original_name} eq=#{im.(c, n) == im.(c, o)}"
end
`
	const want = `Numeric#imag orig=imaginary eq=true
Numeric#conj orig=conjugate eq=true
Integer#inspect orig=to_s eq=true
Float#inspect orig=to_s eq=true
Float#magnitude orig=abs eq=true
Hash#to_s orig=inspect eq=true
Module#inspect orig=to_s eq=true
Proc#inspect orig=to_s eq=true
Symbol#id2name orig=to_s eq=true
Set#length orig=size eq=true
MatchData#deconstruct orig=captures eq=true
`
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestFloatOwnsToSAndInspect: Float had no own #to_s, so the alias asking for
// #inspect over it installed NOTHING and both names resolved to Kernel. The
// formatted values were already right (Kernel#to_s calls the same
// object.Float.ToS()), so only the reflection surface moved -- which is why it
// survived: nothing that reads a float's text could see it.
func TestFloatOwnsToSAndInspect(t *testing.T) {
	src := `p Float.instance_method(:to_s).owner
p Float.instance_method(:inspect).owner
p Float.instance_methods(false).include?(:to_s)
p Float.instance_methods(false).include?(:inspect)
p 1.5.method(:to_s).owner
p 1.5.to_s
p 1.5.inspect
p (1.0 / 3).to_s
p 1e20.to_s
p (-0.0).to_s
p Float::INFINITY.to_s
p 100.0.to_s
`
	const want = "Float\nFloat\ntrue\ntrue\nFloat\n\"1.5\"\n\"1.5\"\n" +
		"\"0.3333333333333333\"\n\"1.0e+20\"\n\"-0.0\"\n\"Infinity\"\n\"100.0\"\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// TestArityGuardReachesEverySharingName: guardArgc wrapped one entry and left
// every other name over the same definition pointing at the UNGUARDED record,
// which was observable as a missing error, not merely as an identity:
//
//	1.imag(5)   ruby 4.0.5  ArgumentError    before  0
//	1.rect(5)   ruby 4.0.5  ArgumentError    before  [1, 0]
//
// MRI guards the cfunc, so every entry above it is guarded. This is the
// behavioural half of the same defect as Numeric#imag == Numeric#imaginary.
func TestArityGuardReachesEverySharingName(t *testing.T) {
	src := `%i[imaginary imag rectangular rect real].each do |m|
  begin
    puts "#{m}: #{1.send(m, 5).inspect}"
  rescue ArgumentError => e
    puts "#{m}: ArgumentError: #{e.message}"
  end
end
p Numeric.instance_method(:imag) == Numeric.instance_method(:imaginary)
p Numeric.instance_method(:rect) == Numeric.instance_method(:rectangular)
p 1.imag
p 1.rect
`
	const want = `imaginary: ArgumentError: wrong number of arguments (given 1, expected 0)
imag: ArgumentError: wrong number of arguments (given 1, expected 0)
rectangular: ArgumentError: wrong number of arguments (given 1, expected 0)
rect: ArgumentError: wrong number of arguments (given 1, expected 0)
real: ArgumentError: wrong number of arguments (given 1, expected 0)
true
true
0
[1, 0]
`
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestSecondNamesOutsideAliasBuiltin covers the three populations that did NOT
// go through aliasBuiltin and so were invisible to a census of its call sites:
// records shared by direct assignment (Time, Thread, Dir, Rational, Complex,
// IO), the pairs installed through a local closure (ENV, Enumerator::Lazy), and
// the singleton tables (File., Dir., Thread.). All of them had the same defect
// for the same reason, and 139 of the 249 names measured across the four
// populations reported the wrong #original_name before this change.
//
// Note Complex vs Numeric: Complex#imag answers :imag and Complex#conj answers
// :conj, while Numeric#imag answers :imaginary and Numeric#conj :conjugate. The
// NAME does not decide which helper a site wants -- only MRI does, per class.
func TestSecondNamesOutsideAliasBuiltin(t *testing.T) {
	src := `im = ->(c, n) { c.instance_method(n) }
[[Time, :mon, :month], [Time, :tv_sec, :to_i], [Time, :xmlschema, :iso8601],
 [Thread, :exit, :kill], [Dir, :tell, :pos], [Rational, :quo, :/],
 [Complex, :imag, :imaginary], [Complex, :conj, :conjugate], [Complex, :quo, :/],
 [Hash, :each_pair, :each], [Range, :member?, :include?],
 [Enumerator, :with_object, :each_with_object], [Enumerator::Lazy, :collect, :map],
 [IO, :each, :each_line], [IO, :isatty, :tty?], [IO, :to_path, :path]].each do |c, n, o|
  puts "#{c}##{n} orig=#{im.(c, n).original_name} eq=#{im.(c, n) == im.(c, o)}"
end
[[File, :unlink, :delete], [File, :empty?, :zero?], [Dir, :getwd, :pwd],
 [Thread, :fork, :start]].each do |c, n, o|
  puts "#{c}.#{n} orig=#{c.method(n).original_name} eq=#{c.method(n) == c.method(o)}"
end
[[:length, :size], [:store, :[]=], [:has_key?, :include?], [:each, :each_pair]].each do |n, o|
  puts "ENV.#{n} orig=#{ENV.method(n).original_name} eq=#{ENV.method(n) == ENV.method(o)}"
end
`
	const want = `Time#mon orig=mon eq=true
Time#tv_sec orig=tv_sec eq=true
Time#xmlschema orig=xmlschema eq=true
Thread#exit orig=exit eq=true
Dir#tell orig=tell eq=true
Rational#quo orig=quo eq=true
Complex#imag orig=imag eq=true
Complex#conj orig=conj eq=true
Complex#quo orig=quo eq=true
Hash#each_pair orig=each_pair eq=true
Range#member? orig=member? eq=true
Enumerator#with_object orig=with_object eq=true
Enumerator::Lazy#collect orig=collect eq=true
IO#each orig=each eq=true
IO#isatty orig=isatty eq=true
IO#to_path orig=to_path eq=true
File.unlink orig=unlink eq=true
File.empty? orig=empty? eq=true
Dir.getwd orig=getwd eq=true
Thread.fork orig=fork eq=true
ENV.length orig=length eq=true
ENV.store orig=store eq=true
ENV.has_key? orig=has_key? eq=true
ENV.each orig=each eq=true
`
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestGenuineAliasesOutsideAliasBuiltin is the matching other half for those
// populations: the sites where MRI really does rb_define_alias, so the OLD name
// is reported. Without these rows a blanket conversion would pass.
func TestGenuineAliasesOutsideAliasBuiltin(t *testing.T) {
	src := `im = ->(c, n) { c.instance_method(n) }
[[Object, :yield_self, :then], [Array, :to_s, :inspect], [Array, :append, :push],
 [Array, :prepend, :unshift], [Encoding, :to_s, :name], [IO, :to_i, :fileno],
 [Thread, :inspect, :to_s]].each do |c, n, o|
  puts "#{c}##{n} orig=#{im.(c, n).original_name} eq=#{im.(c, n) == im.(c, o)}"
end
`
	const want = `Object#yield_self orig=then eq=true
Array#to_s orig=inspect eq=true
Array#append orig=push eq=true
Array#prepend orig=unshift eq=true
Encoding#to_s orig=name eq=true
IO#to_i orig=fileno eq=true
Thread#inspect orig=to_s eq=true
`
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestKernelModuleFunctionMirrorFollowsTheDefinition: rehomeKernelMethods keeps
// one singleton copy per DEFINITION, not per record. While #fail and #raise were
// one record, keying on the record happened to work -- and that same sharing was
// what made Kernel.method(:fail).original_name answer :raise. Both answers are
// MRI's, and they need the two keys to be different things.
func TestKernelModuleFunctionMirrorFollowsTheDefinition(t *testing.T) {
	src := `[[:fail, :raise], [:format, :sprintf]].each do |n, o|
  puts "Kernel.#{n} orig=#{Kernel.method(n).original_name} eq=#{Kernel.method(n) == Kernel.method(o)}"
  puts "Kernel##{n} orig=#{Kernel.instance_method(n).original_name} eq=#{Kernel.instance_method(n) == Kernel.instance_method(o)}"
end
`
	const want = `Kernel.fail orig=fail eq=true
Kernel#fail orig=fail eq=true
Kernel.format orig=format eq=true
Kernel#format orig=format eq=true
`
	if got := eval(t, src); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestAliasHelpersPanicOnAMissingOldName is the barrier, not a behaviour: both
// helpers used to return quietly when the old name was not in the table yet, and
// two sites were silently dead because of it (Float#inspect, and a duplicate
// Float#quo). A registration path that can fail without saying so is invisible
// to any test that only looks at the names which ARE there, so the helpers panic
// -- at VM construction, so a wrong site cannot reach a user program.
func TestAliasHelpersPanicOnAMissingOldName(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*RClass)
	}{
		{"aliasBuiltin", func(c *RClass) { aliasBuiltin(c, "new_name", "absent") }},
		{"defineBuiltinSecondName", func(c *RClass) { defineBuiltinSecondName(c, "new_name", "absent") }},
		{"aliasBuiltinS", func(c *RClass) { aliasBuiltinS(c, "new_name", "absent") }},
		{"defineBuiltinSecondNameS", func(c *RClass) { defineBuiltinSecondNameS(c, "new_name", "absent") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cls := &RClass{name: "Probe", methods: map[string]*Method{}, smethods: map[string]*Method{}}
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("%s returned quietly for an absent old name; it must panic", tc.name)
				}
				msg, _ := r.(string)
				// The qualifier differs by table -- "Probe#absent" for an instance
				// method, "Probe.absent" for a singleton one -- so the message is
				// checked by its parts rather than as one string.
				for _, want := range []string{tc.name, "Probe", "absent", "new_name"} {
					if !strings.Contains(msg, want) {
						t.Errorf("panic message %q does not name %q", msg, want)
					}
				}
			}()
			tc.call(cls)
		})
	}
}

// TestArityGuardHandlesEachDefinitionOnce: guardArgc carries its wrapper to every
// name over one definition, so a names list that happens to contain two of those
// names must not wrap the second a second time. No current call site does -- the
// one in numeric_edges.go lists imaginary, real and rectangular, whose sharers
// (imag, rect) are not themselves in the list -- so the skip is reachable only
// from here, and it is tested here rather than left to a future caller to
// discover. Double-wrapping would not break the guard, but it would leave the
// second name pointing at a record the first no longer holds, which is the
// identity defect this whole change is about.
func TestArityGuardHandlesEachDefinitionOnce(t *testing.T) {
	body := func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(7)
	}
	cls := &RClass{name: "Probe", methods: map[string]*Method{}, smethods: map[string]*Method{}}
	cls.define("canonical", body)
	defineBuiltinSecondName(cls, "second", "canonical")

	// Both names in the list, so the second one is already marked done.
	guardNoArg(cls, "canonical", "second")

	a, b := cls.methods["canonical"], cls.methods["second"]
	if methodDefKey(a) != methodDefKey(b) {
		t.Errorf("guarding both names split the definition: canonical and second no longer share it")
	}
	if got := methodOriginalName(b); got != "second" {
		t.Errorf("second#original_name = %q after guarding, want %q", got, "second")
	}
	if !a.argc.declared || !b.argc.declared {
		t.Errorf("the guard declared arity on %v/%v, want both", a.argc.declared, b.argc.declared)
	}
	// And a name the table does not hold is skipped, not panicked on: guardArgc
	// is a post-hoc wrapper over whatever was registered, and some registrations
	// are platform-dependent (fork/exec are absent under wasm).
	guardNoArg(cls, "never_registered")
	if _, ok := cls.methods["never_registered"]; ok {
		t.Errorf("guardArgc invented a method for a name that was not registered")
	}
}
