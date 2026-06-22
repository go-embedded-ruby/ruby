package vm

import "testing"

// TestThreadCaptureErr covers both arms of the thread panic handler: a Ruby
// exception is preserved, any other panic is wrapped as a RuntimeError.
func TestThreadCaptureErr(t *testing.T) {
	if e := threadCaptureErr(RubyError{Class: "ArgumentError", Message: "bad"}); e.Class != "ArgumentError" || e.Message != "bad" {
		t.Errorf("RubyError not preserved: %+v", e)
	}
	if e := threadCaptureErr("kaboom"); e.Class != "RuntimeError" || e.Message != "kaboom" {
		t.Errorf("non-Ruby panic not wrapped: %+v", e)
	}
}

// TestThreadDisplayMarkers covers the ToS/Inspect/Truthy methods of the
// concurrency value types (their string forms differ from MRI's address-bearing
// inspect, so they are not part of the differential suite).
func TestThreadDisplayMarkers(t *testing.T) {
	for _, v := range []interface {
		ToS() string
		Inspect() string
		Truthy() bool
	}{
		&RThread{status: "run"}, &RMutex{}, &RQueue{},
	} {
		if v.ToS() == "" || v.Inspect() == "" || !v.Truthy() {
			t.Errorf("bad display markers for %T: ToS=%q Inspect=%q Truthy=%v", v, v.ToS(), v.Inspect(), v.Truthy())
		}
	}
}
