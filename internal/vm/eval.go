package vm

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// A string-eval's LOCATION — the (file, line) the compiled source is stamped
// with — is a parameter of every eval entry point in MRI, not a property of the
// caller: rb_f_eval scans `eval(src [, scope [, file [, line]]])` and hands the
// pair down through eval_string_with_cref / eval_string_with_scope into
// eval_make_iseq, which passes `line` to the parser (pm_options_line_set) and
// both to rb_iseq_new_eval (vm_eval.c ruby_4_0:1929-1980, 2059-2098).
// specific_eval does the same for `instance_eval`/`module_eval`'s optional
// second and third arguments (vm_eval.c ruby_4_0:2276-2302).
//
// So the ISeq an eval produces reports the GIVEN path and the given first line —
// `__FILE__`, `__LINE__`, a backtrace entry, and the #source_location of any
// method, proc or binding the source creates. When no file is given MRI does not
// borrow the caller's path either: get_eval_default_path (vm_eval.c
// ruby_4_0:1666) builds the synthetic "(eval at FILE:LINE)" naming where the
// eval was WRITTEN. evalLocation is that pair, and compileEval stamps it on.
type evalLocation struct {
	file string
	line int
}

// evalDefaultPath is get_eval_default_path (vm_eval.c ruby_4_0:1666): the path
// an eval with no explicit filename reports, "(eval at <caller file>:<caller
// line>)". MRI resolves the pair with rb_source_location, the innermost Ruby
// frame — which for a native method such as Kernel#eval is the frame that called
// it, since a native pushes none. vm.sourceLocation is that function, and it
// must be used rather than (currentFile, frameLine(len(frameCode)-1)): frameCode
// is truncated only on a PUSH, so one past the live top still holds a returned
// sibling's pc and reports its line. MRI falls back to the bare "(eval)" when
// there is no Ruby frame at all (Qnil path).
func (vm *VM) evalDefaultPath() string {
	file, line := vm.sourceLocation()
	if file == "" {
		return "(eval)"
	}
	return fmt.Sprintf("(eval at %s:%d)", file, line)
}

// evalLoc builds the location for an eval whose optional filename is fileArg and
// optional first line lineArg — a Go nil for either means the argument was not
// passed at all, which takes the default.
//
// An EXPLICIT Ruby nil filename is where the two families part, and the
// difference is measurable: rb_f_eval runs `StringValue(vfile)` whenever
// `argc >= 3` and only THEN tests NIL_P (vm_eval.c ruby_4_0:2066-2073), so
// eval(src, nil, nil) is a TypeError; specific_eval tests `NIL_P(file)` BEFORE
// StringValue (vm_eval.c ruby_4_0:2297-2302), so instance_eval(src, nil) takes
// the default path. Binding#eval is rb_f_eval with the binding spliced in, so it
// raises like eval. All four verified against ruby 4.0.5. nilFileAllowed picks
// the arm.
//
// An explicit nil LINE is a TypeError in both families: NUM2INT on nil.
func (vm *VM) evalLoc(fileArg, lineArg object.Value, nilFileAllowed bool) evalLocation {
	loc := evalLocation{line: 1}
	switch {
	case fileArg == nil, nilFileAllowed && object.IsNil(fileArg):
		loc.file = vm.evalDefaultPath()
	default:
		loc.file = vm.coerceFormatString(fileArg)
	}
	if lineArg != nil {
		loc.line = vm.evalLineno(lineArg)
	}
	return loc
}

// evalLineno is NUM2INT on an eval's lineno argument. It differs from
// toIntCoerce only in the message MRI reports when #to_int answers with
// something that is not an Integer — rb_to_integer's "can't convert X to
// Integer (X#to_int gives Y)" rather than the plain implicit-conversion
// TypeError, which is what BasicObject#instance_eval's spec pins.
func (vm *VM) evalLineno(v object.Value) int {
	if i, ok := v.(object.Integer); ok {
		return int(i)
	}
	// nil is rb_to_int's own arm, with its own wording ("from nil to integer",
	// not "of nil into Integer"): eval(src, nil, "f", nil) raises it.
	if object.IsNil(v) {
		raise("TypeError", "no implicit conversion from nil to integer")
	}
	if vm.respondsToDynamic(v, "to_int") {
		r := vm.send(v, "to_int", nil, nil)
		if i, ok := r.(object.Integer); ok {
			return int(i)
		}
		raise("TypeError", "can't convert %s to Integer (%s#to_int gives %s)",
			classNameOf(v), classNameOf(v), classNameOf(r))
	}
	raise("TypeError", "no implicit conversion of %s into Integer", classNameOf(v))
	return 0
}

// compileEval compiles src at loc and stamps the path onto the ISeq and every one
// of its children (setISeqFile, so a proc or method the source defines reports
// the eval's path too, as MRI's whole eval ISeq tree carries one path).
//
// The first LINE has to go into the compilation rather than be applied to the
// finished ISeq: MRI hands it to the parser (pm_options_line_set,
// rb_parser_compile_string_path), so it reaches not only every insns_info entry
// and nested first_lineno but also the Integer literal `__LINE__` becomes — and
// that literal is indistinguishable in the constant pool from any other Integer
// once compilation is over. compiler.CompileEval takes it for that reason.
func (vm *VM) compileEval(src string, loc evalLocation) *bytecode.ISeq {
	iseq, cerr := parseCompileEvalFn(src, loc.line)
	if cerr != nil {
		raiseEvalSyntaxError(loc, cerr)
	}
	iseq.Name = "(eval)"
	setISeqFile(iseq, loc.file)
	return iseq
}

// raiseEvalSyntaxError raises the SyntaxError for a string that would not parse,
// with the LOCATION in front of the message as MRI reports it: MRI's parser
// prefixes "file:line: " (rb_syntax_error_append), so `eval('if true',
// TOPLEVEL_BINDING, 'speccing.rb')` says "speccing.rb:1: …" and not merely what
// went wrong. rbgo's front end reports its own 1-based line inside the string
// ("parse error at line N: …"); the line reported here is that N translated into
// the eval's numbering, which is also why a negative first line surfaces
// unchanged.
func raiseEvalSyntaxError(loc evalLocation, cerr error) {
	msg := cerr.Error()
	line := loc.line
	if n, rest, ok := parseErrorLine(msg); ok {
		line = n + loc.line - 1
		msg = rest
	}
	raise("SyntaxError", "%s:%d: %s", loc.file, line, msg)
}

// parseErrorLine splits the front end's "parse error at line N: rest" into N and
// rest. A message in any other shape is left alone (ok == false), so a change of
// wording upstream degrades to the eval's first line rather than to a wrong one.
func parseErrorLine(msg string) (int, string, bool) {
	const pfx = "parse error at line "
	if !strings.HasPrefix(msg, pfx) {
		return 0, msg, false
	}
	rest := msg[len(pfx):]
	i := strings.Index(rest, ": ")
	if i < 0 {
		return 0, msg, false
	}
	n, err := strconv.Atoi(rest[:i])
	if err != nil {
		return 0, msg, false
	}
	return n, rest[i+2:], true
}

// instanceEvalString backs the String form of BasicObject#instance_eval: it
// compiles src and runs it with self as the receiver and the receiver's
// singleton class as the definee, so a `def` in the source becomes a singleton
// method and `self`/instance variables resolve to the receiver. An immediate
// receiver has no singleton class, so its own class stands in as the definee
// (source that only reads still works).
func (vm *VM) instanceEvalString(self object.Value, src string, loc evalLocation) object.Value {
	iseq := vm.compileEval(src, loc)
	definee, ok := vm.ensureSingleton(self)
	if !ok {
		definee = vm.classOf(self)
	}
	vm.pendingMethodCtx = vm.currentMethodCtxPtr()
	return vm.exec(iseq, self, nil, definee, "", nil, nil, nil, nil, nil)
}

// classEvalString backs the String form of Module#module_eval / #class_eval: the
// class is both self and the method-definition target, with a fresh local scope,
// so a top-level `def` in the source becomes an instance method of cls — the
// mechanism racc's runtime uses to graft do_parse/yyparse.
func (vm *VM) classEvalString(cls *RClass, src string, loc evalLocation) object.Value {
	iseq := vm.compileEval(src, loc)
	return vm.exec(iseq, cls, nil, cls, "", nil, nil, nil, nil, nil)
}

// registerEval installs Kernel#eval — the embedded front-end's reason for being.
// Because the lexer/parser/compiler ship inside the binary, a running program
// can compile and run new Ruby at runtime.
//
// eval(str) runs str against the caller's `self` (so `self`, instance variables,
// methods and constants are visible, and a `def` lands where it would in the
// caller's context), with a fresh local scope. To run against the caller's
// *local variables* too, capture a `binding` and pass it: eval(str, binding) —
// see bindingEval in binding.go.
func (vm *VM) registerEval() {
	vm.cObject.define("eval", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		// eval(src [, scope [, file [, line]]]) — rb_scan_args(argc, argv, "13") in
		// rb_f_eval, so 1..4 arguments.
		if len(args) < 1 || len(args) > 4 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 1..4)", len(args))
		}
		src := vm.coerceFormatString(args[0])
		var fileArg, lineArg object.Value
		if len(args) >= 3 {
			fileArg = args[2]
		}
		if len(args) >= 4 {
			lineArg = args[3]
		}
		loc := vm.evalLoc(fileArg, lineArg, false)
		// A scope argument must be a Binding or nil: rb_f_eval routes a non-nil
		// scope through Check_TypedStruct, which rejects anything else with "wrong
		// argument type X (expected binding)". rbgo used to IGNORE a non-Binding
		// scope, so eval(str, proc {}) silently ran against the caller instead.
		if len(args) >= 2 && !object.IsNil(args[1]) {
			b, ok := args[1].(*Binding)
			if !ok {
				raise("TypeError", "wrong argument type %s (expected binding)", vm.wrongTypeName(args[1]))
			}
			return vm.bindingEval(b, src, loc)
		}
		// Run against the caller's self/definee with a fresh local scope; a runtime
		// RubyError from the evaluated code propagates (and is rescuable) as usual.
		// eval is transparent to Kernel#__method__ / #__callee__: the evaluated code
		// inherits the caller's method context (eval "__method__" inside a method
		// reports that method), so hand exec the caller's pair.
		iseq := vm.compileEval(src, loc)
		vm.pendingMethodCtx = vm.currentMethodCtxPtr()
		return vm.exec(iseq, self, nil, vm.evalDefinee(self), "", nil, nil, nil, nil, nil)
	})
}

// wrongTypeName is the name Check_TypedStruct puts in "wrong argument type X
// (expected binding)". MRI names the TYPED-DATA STRUCT, not the class, whenever
// the value has one — so a Proc is "proc" and a Method or UnboundMethod is
// "method" (both share rb_method_data_type), all three verified against ruby
// 4.0.5 — and falls back to rb_obj_classname otherwise.
//
// classNameOf must not be used here: it answers "Object" for every one of those
// three, which is how eval(str, proc {}) came to report the wrong type once it
// reported one at all. A kind whose MRI struct name is not reproduced here
// (Thread's is "VM/thread") reports its class name.
func (vm *VM) wrongTypeName(v object.Value) string {
	switch v.(type) {
	case *Proc:
		return "proc"
	case *BoundMethod, *UnboundMethod:
		return "method"
	}
	return vm.classOf(v).name
}

// evalDefinee is where a `def` inside eval(str) — no binding, so no captured
// definee — lands. MRI takes the CALLER's cref (eval_string_with_cref copies the
// calling frame's, vm_eval.c ruby_4_0:2010-2013), which rbgo has no reified
// stack of; classOf(self) stood in for it.
//
// That stand-in is right for an ordinary object, whose cref while its methods run
// is its class. It is never right when self is a Module or Class: the cref there
// is the module ITSELF — that is what a class body, and a `def self.x` inside
// one, run under — and classOf answered Class or Module, so
// `Class.new { eval("def m; end") }` defined m on Class rather than on the new
// class, and the new class reported no such method.
//
// It remains a stand-in: a cref pushed by instance_eval over a module (self is
// the module, the cref its singleton class) is not recoverable from self alone.
func (vm *VM) evalDefinee(self object.Value) *RClass {
	if cls, ok := self.(*RClass); ok {
		return cls
	}
	return vm.classOf(self)
}
