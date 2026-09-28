package compiler

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
)

// findOp returns the index of the first instruction with op in iseq, or -1.
func waveEFindOp(iseq *bytecode.ISeq, op bytecode.Op) int {
	for i, in := range iseq.Insns {
		if in.Op == op {
			return i
		}
	}
	return -1
}

// TestWaveECPathProductionsAreThreeShapes pins the three cpath productions of
// MRI's grammar (parse.y-ruby_4_0:3831-3845) onto three distinct emissions:
//
//	class Bar        NEW_COLON2(0, …)        -> OpDefineClass, no operand
//	class ::Bar      NEW_COLON3              -> Object pushed, OpDefineClassScoped
//	class Foo::Bar   NEW_COLON2($1, $3)      -> Foo pushed, OpDefineClassScoped
//
// The bare case is the known-bad control for the other two: if the leading-`::`
// test ever collapses back onto it (which is the defect this pins), the bare
// case's assertion still passes while both scoped assertions fail — so the
// three rows cannot all pass on a compiler that has stopped distinguishing them.
func TestWaveECPathProductionsAreThreeShapes(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantOp   bytecode.Op
		wantC    int    // OpDefineClassScoped's C flags (bit 0 = parent pushed)
		wantPush string // name the instruction before the define pushes ("" = none)
	}{
		{"bare_class", "module M; class Bar; end; end", bytecode.OpDefineClass, 0, ""},
		{"global_class", "module M; class ::Bar; end; end", bytecode.OpDefineClassScoped, 1, "Object"},
		{"compact_class", "module M; class Foo::Bar; end; end", bytecode.OpDefineClassScoped, 1, "Foo"},
		{"bare_module", "module M; module Bar; end; end", bytecode.OpDefineModule, 0, ""},
		{"global_module", "module M; module ::Bar; end; end", bytecode.OpDefineModuleScoped, 0, "Object"},
		{"compact_module", "module M; module Foo::Bar; end; end", bytecode.OpDefineModuleScoped, 0, "Foo"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			top := compileSrc(t, tc.src)
			// The outer `module M` body is the only child of the top level.
			if len(top.Children) != 1 {
				t.Fatalf("want 1 child ISeq for the module body, got %d", len(top.Children))
			}
			body := top.Children[0]
			at := waveEFindOp(body, tc.wantOp)
			if at < 0 {
				t.Fatalf("%s: no %v in the module body: %v", tc.src, tc.wantOp, body.Insns)
			}
			if tc.wantOp == bytecode.OpDefineClassScoped && body.Insns[at].C != tc.wantC {
				t.Errorf("%s: C flags = %d, want %d", tc.src, body.Insns[at].C, tc.wantC)
			}
			if tc.wantPush == "" {
				if at != 0 {
					t.Errorf("%s: bare define is at %d, want 0 (nothing pushed before it)", tc.src, at)
				}
				return
			}
			if at == 0 {
				t.Fatalf("%s: scoped define is first — nothing was pushed as its scope", tc.src)
			}
			prev := body.Insns[at-1]
			got := ""
			if prev.A < len(body.Names) {
				got = body.Names[prev.A]
			}
			if got != tc.wantPush {
				t.Errorf("%s: instruction before the define pushes %q (op %v), want %q", tc.src, got, prev.Op, tc.wantPush)
			}
			// `::Bar` must be reached by the TOP-LEVEL constant opcode, so a
			// lexically nested `Object` constant cannot shadow it.
			if tc.wantPush == "Object" && prev.Op != bytecode.OpGetConstTop {
				t.Errorf("%s: Object is pushed with %v, want OpGetConstTop", tc.src, prev.Op)
			}
		})
	}
}

// TestWaveEVCallFlag pins bytecode.FlagSendVCall onto MRI's NODE_VCALL shape:
// a receiver-less, argument-less, block-less name that is not a local
// (parse.y-ruby_4_0:13086, gettable's "method call without arguments").
//
// The false rows are the known-bad control: a probe that set the flag on every
// send would pass every true row and fail all four false ones.
func TestWaveEVCallFlag(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"bare_name", "nope_xyz", true},
		{"bare_name_in_method", "def m; nope_xyz; end", true},
		{"with_argument", "nope_xyz 1", false},
		{"with_paren_argument", "nope_xyz(1)", false},
		{"with_block", "nope_xyz {}", false},
		{"explicit_receiver", "self.nope_xyz", false},
		{"safe_navigation", "nil&.nope_xyz", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			iseq := compileSrc(t, tc.src)
			body := iseq
			if len(iseq.Children) == 1 && len(iseq.Children[0].Insns) > 0 && tc.name == "bare_name_in_method" {
				body = iseq.Children[0]
			}
			found := false
			for _, in := range body.Insns {
				switch in.Op {
				case bytecode.OpSend, bytecode.OpSendArray, bytecode.OpSendBlockArg, bytecode.OpSendArrayBlockArg:
					if in.A < len(body.Names) && body.Names[in.A] == "nope_xyz" {
						found = true
						if got := in.Flags&bytecode.FlagSendVCall != 0; got != tc.want {
							t.Errorf("%s: FlagSendVCall = %v, want %v (flags %d)", tc.src, got, tc.want, in.Flags)
						}
					}
				}
			}
			if !found {
				t.Fatalf("%s: no send of nope_xyz was emitted", tc.src)
			}
		})
	}
}

// A local variable is read, not sent, so it never reaches the VCALL test at all
// — which is why the flag can be set on the whole receiver-less/no-arg shape.
func TestWaveEVCallNotEmittedForALocal(t *testing.T) {
	iseq := compileSrc(t, "nope_xyz = 1\nnope_xyz")
	for _, in := range iseq.Insns {
		if in.Op == bytecode.OpSend && in.A < len(iseq.Names) && iseq.Names[in.A] == "nope_xyz" {
			t.Fatalf("a declared local was compiled as a send: %v", iseq.Insns)
		}
	}
}
