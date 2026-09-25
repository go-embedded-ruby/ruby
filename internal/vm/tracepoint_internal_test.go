package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// tpRun runs a Ruby program and returns its trimmed stdout, failing the test on
// an uncaught exception. It is runSrc with the error surfaced as the test
// failure message rather than a bare "run:" so a TracePoint mistake reads.
func tpRun(t *testing.T, src string) string {
	t.Helper()
	return runSrc(t, src)
}

// TestTracePointLineEvents pins the shape the whole subsystem rests on: a :line
// event per line of the enabled block, reporting the line about to run.
func TestTracePointLineEvents(t *testing.T) {
	got := tpRun(t, `
lines = []
TracePoint.new(:line) { |tp| lines << tp.lineno }.enable do
  a = 1
  b = 2
end
p lines
`)
	if got != "[4, 5]" {
		t.Errorf("line events = %s, want [4, 5]", got)
	}
}

// TestTracePointEnableWithoutBlock covers the no-block form of enable/disable:
// each answers the PREVIOUS state, and a TracePoint enabled with no block keeps
// tracing the frame it was enabled from — which is the branch that builds the
// traceFrame lazily, because that frame started untraced.
func TestTracePointEnableWithoutBlock(t *testing.T) {
	got := tpRun(t, `
called = false
t = TracePoint.new(:line) { called = true }
p t.enable
x = 1
p called
p t.enable
p t.disable
p t.disable
`)
	want := "false\ntrue\ntrue\ntrue\nfalse"
	if got != want {
		t.Errorf("enable/disable = %q, want %q", got, want)
	}
}

// TestTracePointNewValidation covers tracepoint_new_s's three refusals and its
// no-argument default (every event).
func TestTracePointNewValidation(t *testing.T) {
	got := tpRun(t, `
begin; TracePoint.new(:nope) {}; rescue ArgumentError => e; puts e.message; end
begin; TracePoint.new(:line); rescue ArgumentError => e; puts e.message; end
begin; TracePoint.new(Object.new) {}; rescue TypeError; puts "TypeError"; end
p TracePoint.new {}.enabled?
`)
	want := "unknown event: nope\nmust be called with a block\nTypeError\nfalse"
	if got != want {
		t.Errorf("validation = %q, want %q", got, want)
	}
}

// TestTracePointEventCoercion covers rb_to_symbol_type's two live paths: a
// String (which answers #to_sym with a Symbol) and an object whose #to_sym
// answers something else, which is a TypeError.
func TestTracePointEventCoercion(t *testing.T) {
	got := tpRun(t, `
p TracePoint.new("line") {}.enabled?
class Bad; def to_sym; 1; end; end
begin; TracePoint.new(Bad.new) {}; rescue TypeError; puts "TypeError"; end
`)
	want := "false\nTypeError"
	if got != want {
		t.Errorf("coercion = %q, want %q", got, want)
	}
}

// TestTracePointCallReturn covers the :call/:return pair and the accessors that
// only those events answer: method_id, callee_id (which differ under an alias),
// defined_class, return_value and parameters.
func TestTracePointCallReturn(t *testing.T) {
	got := tpRun(t, `
class Holder
  def m(a, b = 1); a; end
  alias_method :m2, :m
end
seen = []
TracePoint.new(:call, :return) do |tp|
  seen << [tp.event, tp.method_id, tp.callee_id, tp.defined_class.to_s, tp.parameters]
  seen << tp.return_value if tp.event == :return
end.enable { Holder.new.m2(7) }
seen.each { |s| p s }
`)
	want := strings.Join([]string{
		`[:call, :m, :m2, "Holder", [[:req, :a], [:opt, :b]]]`,
		`[:return, :m, :m2, "Holder", [[:req, :a], [:opt, :b]]]`,
		`7`,
	}, "\n")
	if got != want {
		t.Errorf("call/return = %q, want %q", got, want)
	}
}

// TestTracePointBlockEvents covers :b_call/:b_return and the non-lambda
// #parameters rule (a proc reports its positionals as :opt, a lambda as :req).
func TestTracePointBlockEvents(t *testing.T) {
	got := tpRun(t, `
seen = []
pr = proc { |x| }
la = ->(y) { }
tp = TracePoint.new(:b_call) { |t| seen << t.parameters }
tp.enable(target: pr) { pr.call(1) }
tp.enable(target: la) { la.call(1) }
p seen
`)
	if got != "[[[:opt, :x]], [[:req, :y]]]" {
		t.Errorf("block parameters = %s, want [[[:opt, :x]], [[:req, :y]]]", got)
	}
}

// TestTracePointClassEnd covers the :class/:end pair for a class body, a module
// body and a singleton-class body — the three sites that mark a frame as one.
func TestTracePointClassEnd(t *testing.T) {
	got := tpRun(t, `
seen = []
TracePoint.new(:class, :end) { |tp| seen << [tp.event, tp.self.to_s] }.enable do
  class TPC; end
  module TPM; end
  o = Object.new
  class << o; end
end
seen.each { |s| p s[0] }
p seen[0][1]
`)
	want := ":class\n:end\n:class\n:end\n:class\n:end\n\"TPC\""
	if got != want {
		t.Errorf("class/end = %q, want %q", got, want)
	}
}

// TestTracePointRescue covers the :rescue event and #raised_exception.
func TestTracePointRescue(t *testing.T) {
	got := tpRun(t, `
seen = nil
TracePoint.new(:rescue) { |tp| seen = tp.raised_exception.class.to_s }.enable do
  begin
    raise ArgumentError, "boom"
  rescue => e
  end
end
p seen
`)
	if got != `"ArgumentError"` {
		t.Errorf("rescue = %s, want \"ArgumentError\"", got)
	}
}

// TestTracePointAccessorsOutside covers get_trace_arg's refusal and the two
// accessors that refuse an event that does not carry their datum.
func TestTracePointAccessorsOutside(t *testing.T) {
	got := tpRun(t, `
tp = TracePoint.new(:line) {}
begin; tp.lineno; rescue RuntimeError => e; puts e.message; end
TracePoint.new(:line) do |t|
  begin; t.return_value; rescue RuntimeError => e; puts e.message; end
  begin; t.raised_exception; rescue RuntimeError => e; puts e.message; end
  begin; t.parameters; rescue RuntimeError => e; puts e.message; end
  t.disable
end.enable { x = 1 }
`)
	want := strings.Join([]string{
		"access from outside",
		"not supported by this event",
		"not supported by this event",
		"not supported by this event",
	}, "\n")
	if got != want {
		t.Errorf("outside access = %q, want %q", got, want)
	}
}

// TestTracePointInspectShapes covers tracepoint_inspect's four live shapes: the
// enabled/disabled form, the bare event form, the "in 'method'" form a :line
// inside a method takes, and the quoted-method form of :call.
func TestTracePointInspectShapes(t *testing.T) {
	got := tpRun(t, `
t = TracePoint.new(:line) {}
p t.inspect
t.enable
p t.inspect
t.disable
seen = []
def tp_inspect_probe; 42; end
TracePoint.new(:line, :call) { |tp| seen << tp.inspect }.enable { tp_inspect_probe }
puts seen.map { |s| s.sub(/:\d+>/, ':N>').sub(/ [^ ]*:N>/, ' F:N>') }.uniq
`)
	lines := strings.Split(got, "\n")
	if len(lines) < 2 || lines[0] != `"#<TracePoint:disabled>"` || lines[1] != `"#<TracePoint:enabled>"` {
		t.Fatalf("inspect enabled/disabled = %q", got)
	}
	joined := strings.Join(lines[2:], "\n")
	for _, want := range []string{"#<TracePoint:line ", "#<TracePoint:call 'tp_inspect_probe' ", "in 'tp_inspect_probe'"} {
		if !strings.Contains(joined, want) {
			t.Errorf("inspect shapes %q missing %q", joined, want)
		}
	}
}

// TestTracePointBinding covers rb_tracearg_binding: a frame event hands back a
// Binding over that frame's locals.
func TestTracePointBinding(t *testing.T) {
	got := tpRun(t, `
def tp_binding_probe; secret = 42; end
names = nil
TracePoint.new(:return) { |tp| names = tp.binding.local_variables }.enable { tp_binding_probe }
p names
`)
	if got != "[:secret]" {
		t.Errorf("binding local_variables = %s, want [:secret]", got)
	}
}

// TestTracePointTargetErrors covers every refusal of
// rb_tracepoint_enable_for_target and of the nesting rules around it.
func TestTracePointTargetErrors(t *testing.T) {
	got := tpRun(t, `
def msg
  yield
  "no raise"
rescue ArgumentError, TypeError => e
  e.message
end
p msg { TracePoint.new(:line) {}.enable(target_line: 3) {} }
p msg { TracePoint.new(:call) {}.enable(target: Object.new) {} }
p msg { TracePoint.new(:call) {}.enable(target: proc {}) {} }
p msg { TracePoint.new(:call) {}.enable(target_line: 1, target: -> {}) {} }
p msg { TracePoint.new(:line) {}.enable(target_line: 1, target: -> {}) {} }
p msg { TracePoint.new(:line) {}.enable(target_line: -2, target: -> {}) {} }
p msg { TracePoint.new(:line) {}.enable(target_line: Object.new, target: -> {}) {} }
t = TracePoint.new(:b_call) {}
p msg { t.enable(target: -> {}) { t.enable(target: -> {}) {} } }
p msg { t.enable { t.enable(target: -> {}) {} } }
p msg { t.enable(target: -> {}) { t.enable {} } }
p msg { t.enable(target: -> {}) { t.disable {} } }
`)
	want := strings.Join([]string{
		`"only target_line is specified"`,
		`"specified target is not supported"`,
		`"can not enable any hooks"`,
		`"target_line is specified, but line event is not specified"`,
		`"can not enable any hooks"`,
		`"can not enable any hooks"`,
		`"no implicit conversion of Object into Integer"`,
		`"can't nest-enable a targeting TracePoint"`,
		`"can't nest-enable a targeting TracePoint"`,
		`"can't nest-enable a targeting TracePoint"`,
		`"can't disable a targeting TracePoint in a block"`,
	}, "\n")
	if got != want {
		t.Errorf("target errors =\n%s\nwant\n%s", got, want)
	}
}

// TestTracePointTargetScoping covers the working target: paths — a Method, an
// UnboundMethod and a Proc — and the rule that a target does NOT reach the
// methods it calls.
func TestTracePointTargetScoping(t *testing.T) {
	got := tpRun(t, `
seen = []
o = Object.new
def o.foo; bar; end
def o.bar; end
TracePoint.new(:call) { |tp| seen << tp.method_id }.enable(target: o.method(:foo)) { o.foo }
p seen

seen = []
k = Class.new { def foo; end }
TracePoint.new(:call) { |tp| seen << tp.method_id }.enable(target: k.instance_method(:foo)) { k.new.foo }
p seen

seen = []
blk = proc { }
TracePoint.new(:b_call) { |tp| seen << tp.event }.enable(target: blk) { blk.call }
p seen
`)
	want := "[:foo]\n[:foo]\n[:b_call]"
	if got != want {
		t.Errorf("target scoping = %q, want %q", got, want)
	}
}

// TestTracePointTargetLine covers the target_line: filter: only that line is
// delivered, and a value that answers #to_int is accepted.
func TestTracePointTargetLine(t *testing.T) {
	got := tpRun(t, `
target = lambda do
  x = 1
  y = 2
  z = x + y
end
all = []
TracePoint.new(:line) { |tp| all << tp.lineno }.enable(target: target) { target.call }
mid = all[1]
seen = []
TracePoint.new(:line) { |tp| seen << tp.lineno }.enable(target_line: mid, target: target) { target.call }
p seen == [mid]
class Coerce; def initialize(n); @n = n; end; def to_int; @n; end; end
seen = []
TracePoint.new(:line) { |tp| seen << tp.lineno }.enable(target_line: Coerce.new(mid), target: target) { target.call }
p seen == [mid]
`)
	if got != "true\ntrue" {
		t.Errorf("target_line = %q, want \"true\\ntrue\"", got)
	}
}

// TestTracePointAllowReentry covers TracePoint.allow_reentry in both of its
// states: inside a handler it lifts the reentry guard for its block, and
// outside one it refuses.
func TestTracePointAllowReentry(t *testing.T) {
	got := tpRun(t, `
begin
  TracePoint.allow_reentry {}
rescue RuntimeError => e
  puts e.message
end
n = 0
tp = TracePoint.new(:line) do |t|
  n += 1
  next if n > 3
  TracePoint.allow_reentry { q = 1 }
end
tp.enable { w = 1 }
p n > 1
`)
	want := "No need to allow reentrance.\ntrue"
	if got != want {
		t.Errorf("allow_reentry = %q, want %q", got, want)
	}
}

// TestTracePointTraceAndThreadFilter covers TracePoint.trace (which hands back
// an already-enabled TracePoint) and the target_thread: default, which confines
// the block form of enable to the thread that called it.
func TestTracePointTraceAndThreadFilter(t *testing.T) {
	got := tpRun(t, `
t = TracePoint.trace(:line) {}
p t.enabled?
t.disable

threads = []
main = Thread.current
TracePoint.new(:line) { threads << Thread.current }.enable do
  x = 1
  th = Thread.new { y = 2 }
  th.join
end
p threads.uniq == [main]
`)
	want := "true\ntrue"
	if got != want {
		t.Errorf("trace/thread filter = %q, want %q", got, want)
	}
}

// TestTracePointThreadFilterOverride covers the "can not override
// target_thread filter" refusal, which needs a TracePoint that already carries
// one when a second enable names another.
func TestTracePointThreadFilterOverride(t *testing.T) {
	vm := New(&bytes.Buffer{})
	tp := &tracePoint{events: evLine, blk: &Proc{}, targetTh: vm.mainThread}
	defer func() {
		r := recover()
		e, ok := r.(RubyError)
		if !ok || e.Class != "ArgumentError" {
			t.Fatalf("recover = %v, want an ArgumentError", r)
		}
		if e.Message != "can not override target_thread filter" {
			t.Errorf("message = %q", e.Message)
		}
	}()
	h := object.NewHash()
	h.Set(object.SymVal("target_thread"), vm.mainThread)
	vm.tracePointEnableM(tp, []object.Value{h}, nil)
}

// TestTracePointThreadFilterType covers the non-Thread target_thread: refusal.
func TestTracePointThreadFilterType(t *testing.T) {
	vm := New(&bytes.Buffer{})
	tp := &tracePoint{events: evLine, blk: &Proc{}}
	defer func() {
		e, ok := recover().(RubyError)
		if !ok || e.Class != "TypeError" {
			t.Fatalf("want a TypeError, got %v", e)
		}
	}()
	h := object.NewHash()
	h.Set(object.SymVal("target_thread"), object.IntValue(1))
	vm.tracePointEnableM(tp, []object.Value{h}, nil)
}

// TestTracePointBadReceiver covers tpOf's two refusals: a receiver that is not
// an RObject at all, and one that is but carries no state.
func TestTracePointBadReceiver(t *testing.T) {
	for _, v := range []object.Value{object.IntValue(1), &RObject{ivars: map[string]object.Value{}}} {
		func() {
			defer func() {
				e, ok := recover().(RubyError)
				if !ok || e.Class != "TypeError" {
					t.Errorf("tpOf(%v) = %v, want a TypeError", v, e)
				}
			}()
			tpOf(v)
		}()
	}
}

// TestTracePointAllowReentryNeedsBlock covers allow_reentry's block check,
// which Ruby cannot reach: the method is only callable from inside a handler,
// and a handler calling it without a block is what this asserts.
func TestTracePointAllowReentryNeedsBlock(t *testing.T) {
	vm := New(&bytes.Buffer{})
	vm.traceArg = &traceArg{event: evLine}
	defer func() {
		e, ok := recover().(RubyError)
		if !ok || e.Class != "ArgumentError" {
			t.Fatalf("want an ArgumentError, got %v", e)
		}
	}()
	vm.traceAllowReentry(nil)
}

// TestTracePointEventNameUnknown covers get_event_id's default: a mask that is
// not one named event has no name.
func TestTracePointEventNameUnknown(t *testing.T) {
	if got := eventName(evLine | evCall); got != "unknown" {
		t.Errorf("eventName(line|call) = %q, want %q", got, "unknown")
	}
	if got := eventName(evRescue); got != "rescue" {
		t.Errorf("eventName(rescue) = %q, want %q", got, "rescue")
	}
}

// TestTracePointEventsNamed covers the diagnostic renderer, which must list
// single bits only — an aggregate would double-count its members.
func TestTracePointEventsNamed(t *testing.T) {
	if got := traceEventsNamed(evLine | evBReturn); got != "line,b_return" {
		t.Errorf("traceEventsNamed = %q, want %q", got, "line,b_return")
	}
	if got := traceEventsNamed(0); got != "" {
		t.Errorf("traceEventsNamed(0) = %q, want empty", got)
	}
}

// TestTracePointClassOf covers tpClassOf's fallback: TracePoint.new reached
// with a non-class receiver still makes a TracePoint.
func TestTracePointClassOf(t *testing.T) {
	vm := New(&bytes.Buffer{})
	if got := tpClassOf(vm, object.IntValue(1)); got != vm.cTracePoint {
		t.Errorf("tpClassOf(non-class) = %v, want TracePoint", got)
	}
	if got := tpClassOf(vm, vm.cObject); got != vm.cObject {
		t.Errorf("tpClassOf(Object) = %v, want Object", got)
	}
}

// TestTraceArgBindingNoFrame covers rb_tracearg_binding's nil answer for an
// event that carries no frame.
func TestTraceArgBindingNoFrame(t *testing.T) {
	vm := New(&bytes.Buffer{})
	if got := vm.traceArgBinding(&traceArg{event: evLine}); !object.IsNil(got) {
		t.Errorf("binding with no env = %v, want nil", got)
	}
}

// TestTraceArgParametersEdges covers the two #parameters answers Ruby cannot
// currently reach: a frame event with no ISeq, and the C-call events, which
// answer nil rather than raising.
func TestTraceArgParametersEdges(t *testing.T) {
	if got := traceArgParameters(&traceArg{event: evCall}); !object.IsNil(got) {
		t.Errorf("parameters with no iseq = %v, want nil", got)
	}
	if got := traceArgParameters(&traceArg{event: evCCall}); !object.IsNil(got) {
		t.Errorf("parameters for c_call = %v, want nil", got)
	}
}

// TestTracePointInspectThreadEvent covers tracepoint_inspect's thread shape,
// which rbgo does not raise (thread_begin/thread_end are not in
// supportedTraceEvents) but whose rendering is defined.
func TestTracePointInspectThreadEvent(t *testing.T) {
	vm := New(&bytes.Buffer{})
	tp := &tracePoint{events: evThreadBegin}
	vm.traceArg = &traceArg{event: evThreadBegin, self: vm.mainThread}
	got := vm.tracePointInspect(tp)
	if !strings.HasPrefix(got, "#<TracePoint:thread_begin ") {
		t.Errorf("inspect = %q, want a thread_begin shape", got)
	}
}

// TestTracePointInspectCReturn covers the quoted-method inspect shape for the
// C-call events, which share tracepoint_inspect's branch with :call/:return.
func TestTracePointInspectCReturn(t *testing.T) {
	vm := New(&bytes.Buffer{})
	vm.traceArg = &traceArg{event: evCReturn, methodID: "max", path: "a.rb", line: 3}
	want := "#<TracePoint:c_return 'max' a.rb:3>"
	if got := vm.tracePointInspect(&tracePoint{}); got != want {
		t.Errorf("inspect = %q, want %q", got, want)
	}
}

// TestTracePointDisplayMarkers covers the object.Value markers on the Go state
// box, which is never a receiver and so never renders through Ruby.
func TestTracePointDisplayMarkers(t *testing.T) {
	tp := &tracePoint{}
	if tp.ToS() != "#<TracePoint>" || tp.Inspect() != "#<TracePoint>" || !tp.Truthy() {
		t.Errorf("markers = %q/%q/%v", tp.ToS(), tp.Inspect(), tp.Truthy())
	}
}

// TestFireTraceReentryGuard covers the guard directly: with an event in flight
// nothing is delivered, and with no hooks at all there is nothing to deliver.
func TestFireTraceReentryGuard(t *testing.T) {
	vm := New(&bytes.Buffer{})
	fired := 0
	tp := &tracePoint{events: evLine, tracing: true}
	tp.blk = &Proc{native: func(*VM, []object.Value) object.Value { fired++; return object.NilV }}
	tp.self = object.NilV
	vm.tracePoints = []*tracePoint{tp}

	vm.traceArg = &traceArg{event: evLine}
	vm.fireTrace(&traceArg{event: evLine})
	if fired != 0 {
		t.Errorf("fired %d times while an event was in flight, want 0", fired)
	}
	vm.traceArg = nil
	vm.fireTrace(&traceArg{event: evLine})
	if fired != 1 {
		t.Errorf("fired %d times, want 1", fired)
	}
	// A hook whose thread filter names another thread is skipped.
	tp.targetTh = &RThread{}
	vm.fireTrace(&traceArg{event: evLine})
	if fired != 1 {
		t.Errorf("fired %d times through a foreign thread filter, want 1", fired)
	}
	// And an empty list short-circuits before the copy.
	vm.tracePoints = nil
	vm.fireTrace(&traceArg{event: evLine})
	if fired != 1 {
		t.Errorf("fired %d times with no hooks, want 1", fired)
	}
}

// TestTracepointEnableIdempotent covers rb_tracepoint_enable's no-op branch and
// rb_tracepoint_disable's, neither of which changes the hook list.
func TestTracepointEnableIdempotent(t *testing.T) {
	vm := New(&bytes.Buffer{})
	tp := &tracePoint{events: evLine, blk: &Proc{}}
	vm.tracepointEnable(tp)
	vm.tracepointEnable(tp)
	if len(vm.tracePoints) != 1 {
		t.Errorf("hook list = %d entries after a double enable, want 1", len(vm.tracePoints))
	}
	if vm.traceEvents != evLine {
		t.Errorf("traceEvents = %#x, want %#x", vm.traceEvents, evLine)
	}
	vm.tracepointDisable(tp)
	vm.tracepointDisable(tp)
	if len(vm.tracePoints) != 0 || vm.traceEvents != 0 {
		t.Errorf("after disable: %d hooks, events %#x", len(vm.tracePoints), vm.traceEvents)
	}
}

// TestRecomputeTraceEventsMasksUnsupported pins the narrowing: an event rbgo
// does not raise must not open the interpreter's gate, or every program would
// pay for a hook that can never fire.
func TestRecomputeTraceEventsMasksUnsupported(t *testing.T) {
	vm := New(&bytes.Buffer{})
	vm.tracePoints = []*tracePoint{{events: evCCall | evRaise | evThreadBegin}}
	vm.recomputeTraceEvents()
	if vm.traceEvents != 0 {
		t.Errorf("traceEvents = %#x for unsupported events only, want 0", vm.traceEvents)
	}
	vm.tracePoints = []*tracePoint{{events: evCCall | evLine}}
	vm.recomputeTraceEvents()
	if vm.traceEvents != evLine {
		t.Errorf("traceEvents = %#x, want %#x", vm.traceEvents, evLine)
	}
}

// TestIseqOfTargetNative covers the native-method target: a Method record with
// no ISeq is the "specified target is not supported" ArgumentError, which is
// `RubyVM::InstructionSequence.of` answering nil for a C method.
func TestIseqOfTargetNative(t *testing.T) {
	vm := New(&bytes.Buffer{})
	defer func() {
		e, ok := recover().(RubyError)
		if !ok || e.Class != "ArgumentError" {
			t.Fatalf("want an ArgumentError, got %v", e)
		}
	}()
	vm.iseqOfTarget(&BoundMethod{m: &Method{name: "puts", native: func(*VM, object.Value, []object.Value, *Proc) object.Value { return object.NilV }}})
}

// TestIseqOfTargetNilMethod covers the two nil-record guards, which a Method or
// UnboundMethod built by reflection could reach.
func TestIseqOfTargetNilMethod(t *testing.T) {
	vm := New(&bytes.Buffer{})
	for _, target := range []object.Value{&BoundMethod{}, &UnboundMethod{}} {
		func() {
			defer func() {
				e, ok := recover().(RubyError)
				if !ok || e.Class != "ArgumentError" {
					t.Errorf("iseqOfTarget(%T) = %v, want an ArgumentError", target, e)
				}
			}()
			vm.iseqOfTarget(target)
		}()
	}
}

// TestCollectISeqs covers the recursive walk, including its already-seen guard
// and its nil guard.
func TestCollectISeqs(t *testing.T) {
	leaf := &bytecode.ISeq{Name: "leaf"}
	root := &bytecode.ISeq{Name: "root", Children: []*bytecode.ISeq{leaf, leaf, nil}}
	got := map[*bytecode.ISeq]bool{}
	collectISeqs(root, got)
	if len(got) != 2 || !got[root] || !got[leaf] {
		t.Errorf("collectISeqs = %d entries, want root and leaf", len(got))
	}
	collectISeqs(nil, got)
	if len(got) != 2 {
		t.Errorf("collectISeqs(nil) grew the set to %d", len(got))
	}
}

// TestFrameTracePath covers both answers: an ISeq that names its file, and one
// that does not (the top-level program), which falls back to the frame label.
func TestFrameTracePath(t *testing.T) {
	vm := New(&bytes.Buffer{})
	if got := vm.frameTracePath(&bytecode.ISeq{File: "app.rb"}, 0); got != "app.rb" {
		t.Errorf("path = %q, want %q", got, "app.rb")
	}
	vm.SetScriptName("prog.rb")
	if got := vm.frameTracePath(&bytecode.ISeq{}, 0); got != "prog.rb" {
		t.Errorf("path = %q, want %q", got, "prog.rb")
	}
	// A frame index the stacks no longer cover (exec can outrun them; see the
	// bounds note at the pc publication) reports the script rather than panicking.
	if got := vm.frameTracePath(&bytecode.ISeq{}, 99); got != "prog.rb" {
		t.Errorf("path for an out-of-range frame = %q, want %q", got, "prog.rb")
	}
}

// TestNewTraceFrameKinds pins the frame typing: block, class body, method, and
// the top-level/eval frame that raises neither event.
func TestNewTraceFrameKinds(t *testing.T) {
	is := &bytecode.ISeq{}
	cases := []struct {
		name      string
		selfBlock *Proc
		method    string
		classBody bool
		entry     traceEvents
		exit      traceEvents
	}{
		{"block", &Proc{}, "", false, evBCall, evBReturn},
		{"lambda", &Proc{isLambda: true}, "", false, evBCall, evBReturn},
		{"class body", nil, "", true, evClass, evEnd},
		{"method", nil, "m", false, evCall, evReturn},
		{"top level", nil, "", false, 0, 0},
	}
	for _, c := range cases {
		f := newTraceFrame(is, 0, object.NilV, frameMethod{orig: "m", callee: "m"}, nil, nil, "", c.selfBlock, c.method, c.classBody)
		if f.entry != c.entry || f.exit != c.exit {
			t.Errorf("%s: entry/exit = %#x/%#x, want %#x/%#x", c.name, f.entry, f.exit, c.entry, c.exit)
		}
		if c.name == "class body" && (f.methodID != "" || f.calleeID != "") {
			t.Errorf("class body kept method_id %q/%q", f.methodID, f.calleeID)
		}
		if c.name == "block" && !f.isProc {
			t.Error("a non-lambda block must report isProc")
		}
		if c.name == "lambda" && f.isProc {
			t.Error("a lambda must not report isProc")
		}
	}
}

// TestTraceFrameKlassValue covers defined_class: a method frame names its owner
// and nothing else does.
func TestTraceFrameKlassValue(t *testing.T) {
	c := newClass("K", nil)
	if got := (&traceFrame{definee: c, entry: evCall}).klassValue(); got != c {
		t.Errorf("klassValue for a method frame = %v, want K", got)
	}
	if got := (&traceFrame{definee: c, entry: evBCall}).klassValue(); got != nil {
		t.Errorf("klassValue for a block frame = %v, want nil", got)
	}
	if got := (&traceFrame{entry: evCall}).klassValue(); got != nil {
		t.Errorf("klassValue with no definee = %v, want nil", got)
	}
}

// TestTracePointDefinedClassNil covers the accessor's nil rendering, which a
// non-method event reaches.
func TestTracePointDefinedClassNil(t *testing.T) {
	got := tpRun(t, `
seen = 1
TracePoint.new(:line) { |tp| seen = tp.defined_class }.enable { x = 1 }
p seen
`)
	if got != "nil" {
		t.Errorf("defined_class on a line event = %s, want nil", got)
	}
}

// TestTracePointHookOrder pins the two-list order — global hooks first, then
// the targeting ones, each most-recently-enabled first — which is the one
// property registration order alone cannot produce.
func TestTracePointHookOrder(t *testing.T) {
	got := tpRun(t, `
target = -> {}
out = []
outer = TracePoint.new(:b_call) { out << :outer }
inner = TracePoint.new(:b_call) { out << :inner }
outer.enable(target: target) { inner.enable(target: target) { target.call } }
p out
out = []
outer.enable(target: target) { inner.enable { target.call } }
p out.last(2)
`)
	want := "[:inner, :outer]\n[:inner, :outer]"
	if got != want {
		t.Errorf("hook order = %q, want %q", got, want)
	}
}
