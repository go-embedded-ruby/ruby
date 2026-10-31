package ruby

import (
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-embedded-ruby/ruby/internal/vm"
)

// Interp is a Ruby interpreter that keeps its state between evaluations: one
// VM, and one top-level binding whose local variables persist from one Eval to
// the next, as if every evaluated source were a further part of one program.
// Methods, classes, constants and globals persist too, because they always live
// in the VM.
//
// An Interp is not safe for concurrent use. A Func that the interpreter calls
// runs on the goroutine that called Eval, and may call back into the same
// Interp (Block.Call, Value.Call, Get, Set); another goroutine may not.
type Interp struct {
	vm     *vm.VM
	top    *vm.Binding
	stdout *switchWriter
	stderr *switchWriter
	types  map[reflect.Type]*vm.RClass
	opaque *vm.RClass // GoValue, made the first time an unbound Go value is wrapped
}

// Option configures New.
type Option func(*Interp)

// WithStdout sends what the program writes to $stdout to w.
func WithStdout(w io.Writer) Option { return func(in *Interp) { in.stdout.w = w } }

// WithStderr sends what the program writes to $stderr, and its warnings, to w.
func WithStderr(w io.Writer) Option { return func(in *Interp) { in.stderr.w = w } }

// New returns an interpreter writing to os.Stdout and os.Stderr unless an
// option says otherwise.
func New(opts ...Option) *Interp {
	in := &Interp{
		stdout: &switchWriter{w: os.Stdout},
		stderr: &switchWriter{w: os.Stderr},
		types:  map[reflect.Type]*vm.RClass{},
	}
	for _, o := range opts {
		o(in)
	}
	in.vm = vm.NewWithStderr(in.stdout, in.stderr)
	in.top = in.vm.TopBinding()
	return in
}

// SetStdout sends what the program writes to $stdout from now on to w.
func (in *Interp) SetStdout(w io.Writer) { in.stdout.w = w }

// SetStderr sends what the program writes to $stderr from now on to w.
func (in *Interp) SetStderr(w io.Writer) { in.stderr.w = w }

// switchWriter is the writer the VM holds, so the destination can change after
// the VM was made.
type switchWriter struct{ w io.Writer }

func (s *switchWriter) Write(p []byte) (int, error) { return s.w.Write(p) }

// Eval parses, compiles and runs src as top-level code, and returns the value
// of its last expression. file and line are where src comes from: they are
// what __FILE__, __LINE__, backtraces and errors report, so line is the line
// of the source's first line in that file (1 for a whole file). A Ruby
// exception, a syntax error included, comes back as an *Error.
//
// Local variables assigned at the top level of src persist: a later Eval reads
// and assigns the same variables, and Get and Set reach them.
func (in *Interp) Eval(src, file string, line int) (Value, error) {
	v, err := in.vm.EvalIn(in.top, src, file, line)
	if err != nil {
		return Value{}, in.newError(err, file)
	}
	return in.wrap(v), nil
}

// Close runs the at_exit handlers the program registered. The Interp must not
// be used afterwards.
func (in *Interp) Close() error {
	_, err := in.vm.Protect(func() object.Value {
		in.vm.RunAtExit()
		return object.NilV
	})
	if err != nil {
		return in.newError(err, "")
	}
	return nil
}

// Set assigns the top-level local variable name, converted to Ruby as Value
// describes. It creates the variable when it does not exist.
func (in *Interp) Set(name string, v any) error {
	if !IsLocalName(name) {
		return fmt.Errorf("ruby: %q is not a local variable name", name)
	}
	rv, err := in.toRuby(v)
	if err != nil {
		return err
	}
	in.top.LocalSet(name, rv)
	return nil
}

// Get returns the top-level local variable name converted to Go (see
// Value.Go). It is an error if there is no such variable.
func (in *Interp) Get(name string) (any, error) {
	v, err := in.Lookup(name)
	if err != nil {
		return nil, err
	}
	return v.Go(), nil
}

// Lookup returns the top-level local variable name as a Ruby value.
func (in *Interp) Lookup(name string) (Value, error) {
	v, ok := in.top.LocalGet(name)
	if !ok {
		return Value{}, fmt.Errorf("ruby: no local variable %q", name)
	}
	return in.wrap(v), nil
}

// Locals lists the top-level local variables, in the order they were created.
func (in *Interp) Locals() []string { return in.top.LocalNames() }

// IsLocalName reports whether s is spelled like a Ruby local variable: a
// lower-case letter or an underscore, then letters, digits and underscores, and
// not a keyword.
func IsLocalName(s string) bool {
	if s == "" || rubyKeywords[s] {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || r >= 0x80:
		case i > 0 && ((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')):
		default:
			return false
		}
	}
	return true
}

var rubyKeywords = map[string]bool{}

func init() {
	for _, k := range strings.Fields(`__ENCODING__ __LINE__ __FILE__ BEGIN END alias and begin
		break case class def defined? do else elsif end ensure false for if in module next nil
		not or redo rescue retry return self super then true undef unless until when while yield`) {
		rubyKeywords[k] = true
	}
}

// Error is a Ruby exception that reached the host: its class, its message, the
// file and line it was raised at, and its backtrace, innermost first.
//
// File and Line are those of the innermost frame in the file the Eval was
// given, when there is one: an exception raised inside a method the host
// defined, or deep in a library, is placed at the line of the evaluated source
// that led to it. Otherwise they are the innermost frame's.
type Error struct {
	Class     string
	Message   string
	File      string
	Line      int
	Backtrace []string

	in   *Interp
	rerr *vm.RubyError // the exception itself, when it came from the VM
}

// NewError returns an error that, returned by a Func, raises an exception of
// the named class with message msg. class is a class name such as
// "ArgumentError" or "TypeError" ("RuntimeError" when empty).
func NewError(class, msg string) *Error {
	if class == "" {
		class = "RuntimeError"
	}
	return &Error{Class: class, Message: msg}
}

func (e *Error) Error() string {
	if e.File != "" && e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s (%s)", e.File, e.Line, e.Message, e.Class)
	}
	return e.Class + ": " + e.Message
}

// ExitStatus reports whether the exception is a SystemExit (Kernel#exit or
// #abort), and the status it carries.
func (e *Error) ExitStatus() (int, bool) {
	if e.rerr == nil || e.in == nil {
		return 0, false
	}
	return e.in.vm.ExitStatus(*e.rerr)
}

// newError turns what the VM returned into an *Error, placing it in file.
func (in *Interp) newError(err error, file string) error {
	var rerr vm.RubyError
	if !errors.As(err, &rerr) {
		return err // not a Ruby exception: a host writer's error, say
	}
	e := &Error{Class: rerr.Class, Message: rerr.Message, Backtrace: rerr.Backtrace(), in: in, rerr: &rerr}
	e.File, e.Line = locate(e.Backtrace, file)
	if rerr.Class == "SyntaxError" {
		// "file:line: message": the front end's line, already translated.
		if f, l, rest, ok := splitLocation(rerr.Message); ok && (file == "" || f == file) {
			e.File, e.Line, e.Message = f, l, rest
		}
	}
	return e
}

// locate picks the frame an error is reported at: the innermost one in file,
// else the innermost one.
func locate(bt []string, file string) (string, int) {
	first, firstLine := "", 0
	for _, fr := range bt {
		f, l, _, ok := splitLocation(fr)
		if !ok {
			continue
		}
		if file != "" && f == file {
			return f, l
		}
		if first == "" {
			first, firstLine = f, l
		}
	}
	return first, firstLine
}

// splitLocation splits "file:line: rest" or "file:line:in 'm'".
func splitLocation(s string) (file string, line int, rest string, ok bool) {
	for i := 0; i < len(s); i++ {
		if s[i] != ':' {
			continue
		}
		j := i + 1
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == i+1 || (j < len(s) && s[j] != ':') {
			continue
		}
		n, _ := strconv.Atoi(s[i+1 : j])
		rest = strings.TrimPrefix(s[min(j+1, len(s)):], " ")
		return s[:i], n, rest, true
	}
	return "", 0, "", false
}

// raise turns an error a Func returned into the Ruby exception it stands for.
func (in *Interp) raise(err error) {
	var e *Error
	if errors.As(err, &e) {
		if e.rerr != nil {
			vm.Reraise(*e.rerr)
		}
		vm.Raise(e.Class, e.Message)
	}
	vm.Raise("RuntimeError", err.Error())
}
