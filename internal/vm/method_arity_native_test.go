// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// coreArityClasses names the population every count in this file is taken over:
// the classes whose native instance methods #714 measured. A class removed from
// this list would shrink every count silently, so coreNativeFloor puts a floor
// under the population itself.
func coreArityClasses(vm *VM) map[string]*RClass {
	return map[string]*RClass{
		"String": vm.cString, "Array": vm.cArray, "Hash": vm.cHash,
		"Integer": vm.cInteger, "Symbol": vm.cSymbol, "Float": vm.cFloat,
		"Range": vm.cRange,
	}
}

// coreNativeFloor is a floor on how many native methods those seven classes
// carry at boot. It was 476 when this was written; the point of the floor is
// that a change which stops REGISTERING methods cannot make the completeness
// test below pass by emptying the tables it walks.
const coreNativeFloor = 460

// argcUndeclarableCoreMethods are the only core-class natives left without a
// declared argument count, and each is here because NO oracle states one:
// neither ruby 4.0.5, nor activesupport 8.1.3, nor the stdlib libraries rbgo's
// other shims came from define a method of that name on that class. Declaring a
// count for them would mean inventing one from rbgo's own body, which is how a
// wrong expectation gets written into a test that then agrees with the bug.
//
// Every other entry is a method some oracle answers, and the completeness test
// requires it to carry that oracle's count. The set is asserted to be EXACTLY
// this, in both directions: a new undeclared native fails, and so does an entry
// here that has since been declared.
var argcUndeclarableCoreMethods = map[string]bool{
	"Float#pow":               false, // no Float#pow in ruby 4.0.5 (Integer#pow only) or activesupport
	"String#to_html":          false, // rbgo's own name; kramdown 2.5.2 defines no String#to_html
	"String#to_kramdown_html": false, // likewise
	"Array#page":              false, // kaminari/pagy shim; neither gem defines Array#page
	"String#truncate":         true,  // activesupport arity -2 (truncate_to, options = {})
	"Array#in_groups":         true,  // activesupport arity -2 (number, fill_with = nil, &block)
	"Array#in_groups_of":      true,  // activesupport arity -2
	"Array#pack":              true,  // ruby arity -2, from <internal:pack>
	"String#unpack":           true,  // ruby arity -2, from <internal:pack>
	"String#unpack1":          true,  // ruby arity -2, from <internal:pack>
}

// isPlainNative reports whether m is a native method whose arity can only come
// from a declared argc: no ISeq, no Proc body, and not an attr_* accessor (both
// of which methodArity answers from their own shape).
func isPlainNative(m *Method) bool {
	return m.native != nil && m.iseq == nil && m.proc == nil && m.attrKind == notAttr && !m.undefined
}

// coreNatives lists the plain-native methods of the core classes as
// "Class#name", together with their Method records.
func coreNatives(vm *VM) map[string]*Method {
	out := map[string]*Method{}
	for cn, cls := range coreArityClasses(vm) {
		for name, m := range cls.methods {
			if isPlainNative(m) {
				out[cn+"#"+name] = m
			}
		}
	}
	return out
}

// TestNativeArityDeclaredForCoreClasses is the barrier the whole of #714 rests
// on. Nothing in Go's type system can force a registration site to declare an
// argument count — `define` still compiles, and roughly three thousand sites
// across the VM still use it — so what stops a core-class site from omitting one
// is this test: an undeclared native is DISTINGUISHABLE from a declared -1
// (nativeArgc carries the fact of the declaration, not just the number), and
// this enumerates the tables and names every method that has not declared.
//
// Before #714 there was no such distinction: every native answered -1 and no
// test could tell a truthful -1 from a missing one, which is why 98% of the
// table went unnoticed for as long as it did.
func TestNativeArityDeclaredForCoreClasses(t *testing.T) {
	vm := newTestVM()
	natives := coreNatives(vm)
	if len(natives) < coreNativeFloor {
		t.Fatalf("only %d native core-class methods, below the %d floor: the population this test walks has shrunk, so its other counts mean nothing", len(natives), coreNativeFloor)
	}
	var undeclared []string
	for key, m := range natives {
		if !m.argc.declared {
			undeclared = append(undeclared, key)
		}
	}
	sort.Strings(undeclared)
	got := map[string]bool{}
	for _, k := range undeclared {
		got[k] = true
		if _, ok := argcUndeclarableCoreMethods[k]; !ok {
			t.Errorf("%s registers a native with no declared argc: call defineArgc with the count a measured oracle gives (see testdata/native_arity_oracle.tsv), or add it to argcUndeclarableCoreMethods saying which oracle has no answer", k)
		}
	}
	for k := range argcUndeclarableCoreMethods {
		if !got[k] {
			t.Errorf("%s is listed as having no declarable argc but now declares one (or is gone): remove the exemption", k)
		}
	}
	t.Logf("population %d native core-class methods, %d undeclared", len(natives), len(undeclared))
}

type oracleRow struct {
	arity  int
	params string
	source string
}

// loadArityOracle reads the measured oracle, asserting the sweep integrity the
// table itself needs: a non-empty key on every row, no duplicate key, a floor on
// the row count, and more than one arity verdict — a table that answered the same
// arity for every method would not be an oracle, it would be the defect.
func loadArityOracle(t *testing.T) map[string]oracleRow {
	t.Helper()
	f, err := os.Open("testdata/native_arity_oracle.tsv")
	if err != nil {
		t.Fatalf("oracle: %v", err)
	}
	defer f.Close()
	out := map[string]oracleRow{}
	verdicts := map[int]int{}
	rows := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p := strings.Split(line, "\t")
		if len(p) != 5 {
			t.Fatalf("oracle row %d has %d columns, want 5: %q", rows+1, len(p), line)
		}
		if p[0] == "" || p[1] == "" {
			t.Fatalf("oracle row %d has an empty key column", rows+1)
		}
		a, err := strconv.Atoi(p[2])
		if err != nil {
			t.Fatalf("oracle row %d: arity %q: %v", rows+1, p[2], err)
		}
		key := p[0] + "#" + p[1]
		if _, dup := out[key]; dup {
			t.Fatalf("oracle names %s twice", key)
		}
		out[key] = oracleRow{a, p[3], p[4]}
		verdicts[a]++
		rows++
	}
	if rows < coreNativeFloor {
		t.Fatalf("oracle has %d rows, below the %d floor", rows, coreNativeFloor)
	}
	if len(verdicts) < 4 {
		t.Fatalf("oracle records only %d distinct arities (%v): an oracle that answers one value for everything cannot tell a fix from no fix", len(verdicts), verdicts)
	}
	return out
}

// TestNativeArityMatchesMRIOracle pins every declared count against the measured
// oracle rather than against rbgo's behaviour, so an expectation cannot drift
// into agreeing with us and disagreeing with MRI.
//
// The three rows it excuses are MRI's arity -2, which no argc can express: -2 is
// min 1 with an unbounded max, and MRI reaches it by implementing pack/unpack in
// RUBY (<internal:pack>), not through rb_define_method. Every cfunc with a
// negative argc — -1 and -2 alike — reports -1.
func TestNativeArityMatchesMRIOracle(t *testing.T) {
	oracle := loadArityOracle(t)
	vm := newTestVM()
	natives := coreNatives(vm)
	var checked, diverged int
	for key, m := range natives {
		row, ok := oracle[key]
		if !ok {
			continue
		}
		got := methodArity(m)
		if row.arity == -2 {
			if !argcUndeclarableCoreMethods[key] {
				t.Errorf("%s: oracle arity -2 but the method is not listed as inexpressible", key)
			}
			if got != -1 {
				t.Errorf("%s: arity %d, want -1 (the answer any negative argc gives)", key, got)
			}
			diverged++
			continue
		}
		checked++
		if got != row.arity {
			t.Errorf("%s: arity %d, want %d (%s)", key, got, row.arity, row.source)
		}
	}
	if checked < coreNativeFloor-len(argcUndeclarableCoreMethods) {
		t.Fatalf("only %d of %d natives were checked against the oracle: the join is losing rows", checked, len(natives))
	}
	t.Logf("%d natives match the oracle's arity, %d diverge by MRI arity -2", checked, diverged)
}

// TestNativeAccessorsCannotDisagree asserts the property that makes this one
// change rather than several hundred: for a native, #parameters and the
// #inspect signature are RENDERINGS of #arity, so no method can be found where
// two of the three tell different stories. MRI holds the same property by the
// same construction — method_inspect walks rb_method_parameters, which falls
// through to rb_unnamed_parameters(arity) for a cfunc.
//
// Asserting it over the whole table is the point: giving one method an arity
// while #parameters still said [[:rest]] is the inconsistency this fix could
// have introduced, and one method at a time is exactly how it would arrive.
func TestNativeAccessorsCannotDisagree(t *testing.T) {
	vm := newTestVM()
	natives := coreNatives(vm)
	if len(natives) < coreNativeFloor {
		t.Fatalf("population %d below the %d floor", len(natives), coreNativeFloor)
	}
	seen := map[int]int{}
	for key, m := range natives {
		a := methodArity(m)
		seen[a]++
		if got, want := methodParameters(m).Inspect(), unnamedParameters(a).Inspect(); got != want {
			t.Errorf("%s: arity %d but parameters %s, want %s", key, a, got, want)
		}
		if got, want := formatParamList(m), formatUnnamedParams(a); got != want {
			t.Errorf("%s: arity %d but inspect signature %s, want %s", key, a, got, want)
		}
	}
	if len(seen) < 4 {
		t.Fatalf("the table answers only %d distinct arities (%v): before #714 it answered -1 for 98%% of it, and a property that holds trivially over one verdict proves nothing", len(seen), seen)
	}
	t.Logf("arity verdicts across %d natives: %v", len(natives), seen)
}

// TestNativeArityThroughEveryAccessor checks the Ruby-level surfaces agree with
// each other on one method of each shape: a Method and an UnboundMethod, reached
// through instance_method, method, public_method and singleton_method, all report
// the same arity and parameters, and Method#to_proc carries them too (MRI's
// rb_proc_parameters answers rb_unnamed_parameters(rb_proc_arity) for a
// Go-backed proc, so :upcase.to_proc — arity -2 — is [[:req], [:rest]]).
func TestNativeArityThroughEveryAccessor(t *testing.T) {
	const src = `
s = "abc"
probes = {
  "instance_method" => String.instance_method(:length),
  "method"          => s.method(:length),
  "public_method"   => s.public_method(:length),
  "unbound"         => s.method(:length).unbind,
  "rebound"         => String.instance_method(:length).bind(s),
  "to_proc"         => s.method(:length).to_proc,
}
probes.each { |k, p| puts "#{k} #{p.arity} #{p.parameters.inspect}" }
def String.tagged; 1; end
puts "singleton #{String.singleton_method(:tagged).arity} #{String.singleton_method(:tagged).parameters.inspect}"
# the optional-argument shapes, where -1 is the CORRECT answer
%w[sub].each { |m| puts "String##{m} #{String.instance_method(m).arity} #{String.instance_method(m).parameters.inspect}" }
puts "Array#first #{Array.instance_method(:first).arity} #{Array.instance_method(:first).parameters.inspect}"
puts "Hash#fetch #{Hash.instance_method(:fetch).arity} #{Hash.instance_method(:fetch).parameters.inspect}"
puts "Hash#store #{Hash.instance_method(:store).arity} #{Hash.instance_method(:store).parameters.inspect}"
puts "sym_to_proc #{:upcase.to_proc.arity} #{:upcase.to_proc.parameters.inspect}"
puts String.instance_method(:length).inspect
# Hash#store is deliberately NOT probed for #inspect: rbgo aliases it to #[]= and
# renders "#<UnboundMethod: Hash#store([]=)(_, _)>" where ruby 4.0.5 gives
# "#<UnboundMethod: Hash#store(_, _)>" (MRI registers a separate cfunc). That is
# an alias/origName divergence, not an arity one -- its signature fragment is
# right -- and softening this expectation to match rbgo would bury it.
puts String.instance_method(:tr).inspect
puts String.instance_method(:sub).inspect
`
	// Every expectation below was read from ruby 4.0.5, not from rbgo.
	want := strings.Join([]string{
		"instance_method 0 []",
		"method 0 []",
		"public_method 0 []",
		"unbound 0 []",
		"rebound 0 []",
		"to_proc 0 []",
		"singleton 0 []",
		"String#sub -1 [[:rest]]",
		"Array#first -1 [[:rest]]",
		"Hash#fetch -1 [[:rest]]",
		"Hash#store 2 [[:req], [:req]]",
		"sym_to_proc -2 [[:req], [:rest]]",
		"#<UnboundMethod: String#length()>",
		"#<UnboundMethod: String#tr(_, _)>",
		"#<UnboundMethod: String#sub(*)>",
	}, "\n")
	if got := runSrc(t, src); got != want {
		t.Errorf("accessors disagree:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestDeclareArgcRejectsOutOfRange pins the validation. MRI's rb_define_method
// raises "arity out of range: %d for -2..15" (vm_method.c r4:877); rbgo's lower
// bound is -1 because it has no (VALUE self, VALUE args) shape to name with -2,
// and offering a value no site could honestly use would be an untested one.
func TestDeclareArgcRejectsOutOfRange(t *testing.T) {
	for _, argc := range []int{-3, -2, 16, 99} {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Errorf("declareArgc(%d) did not panic", argc)
					return
				}
				if want := fmt.Sprintf("arity out of range: %d for -1..15", argc); r != want {
					t.Errorf("declareArgc(%d) panicked with %v, want %q", argc, r, want)
				}
			}()
			declareArgc(argc)
		}()
	}
	for _, argc := range []int{-1, 0, 1, 15} {
		a := declareArgc(argc)
		wantArity := argc
		if argc < 0 {
			wantArity = -1
		}
		if !a.declared || a.nativeArity() != wantArity {
			t.Errorf("declareArgc(%d) = %+v, arity %d want %d", argc, a, a.nativeArity(), wantArity)
		}
	}
	if got := declareArgc(3).nativeArity(); got != 3 {
		t.Errorf("declared argc 3 reports arity %d", got)
	}
	if got := (nativeArgc{}).nativeArity(); got != -1 {
		t.Errorf("an undeclared argc reports arity %d, want -1", got)
	}
}

// TestGuardArgcKeepsTheMethodRecord pins the defect guardArgc had: it rebuilt the
// Method it wrapped, so every field on the record was reset. The declared argc is
// the one that made it visible (Integer#gcd declared 1 and answered -1), but
// nonRetaining went the same way, and that one is a silent performance loss
// rather than a wrong answer.
func TestGuardArgcKeepsTheMethodRecord(t *testing.T) {
	vm := newTestVM()
	for name, want := range map[string]int{"gcd": 1, "lcm": 1, "gcdlcm": 1, "to_r": 0, "bit_length": 0} {
		m := vm.cInteger.methods[name]
		if m == nil {
			t.Fatalf("Integer#%s is not defined", name)
		}
		if got := methodArity(m); got != want {
			t.Errorf("Integer#%s arity %d, want %d (the guard must not discard the declared argc)", name, got, want)
		}
	}
	// A guard with equal bounds declares the count it enforces, so a site that
	// declared nothing still gets a truthful arity rather than -1.
	cls := newClass("GuardProbe", vm.cObject)
	cls.define("undeclared", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return self })
	cls.define("stays_variadic", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value { return self })
	guardArgc(cls, 2, 2, "undeclared")
	guardArgc(cls, 0, 1, "stays_variadic")
	if got := methodArity(cls.methods["undeclared"]); got != 2 {
		t.Errorf("a [2,2] guard left arity %d, want 2", got)
	}
	if got := methodArity(cls.methods["stays_variadic"]); got != -1 {
		t.Errorf("a [0,1] guard gave arity %d, want -1: unequal bounds ARE variadic", got)
	}
}
