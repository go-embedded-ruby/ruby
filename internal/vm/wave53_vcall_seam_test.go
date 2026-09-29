// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"bytes"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestSetSendVCallDrivesTheMissVerdict exercises the seam AOT-lowered Go uses
// to say what an OpSend says through its flags, the way TestSetSendNoKW does
// for the keyword verdict.
//
// It plants a compiled body of exactly the shape internal/aot/codegen.go now
// emits for a bare identifier -- both verdicts stated, then a bare dispatchSend
// -- and asserts the miss is judged by it. Without the emitted setSendVCall the
// lowered method answers NoMethodError where the interpreter and MRI answer
// NameError, which is the divergence this change exists to remove.
//
// Both directions, because a seam that always said "true" would satisfy one.
func TestSetSendVCallDrivesTheMissVerdict(t *testing.T) {
	for _, tc := range []struct {
		name  string
		vcall bool
		want  string
	}{
		{"a lowered bare identifier", true, "NameError"},
		{"a lowered ordinary call", false, "NoMethodError"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			vm := New(&buf)
			vcall := tc.vcall
			vm.cKernel.methods["wave53_lowered_probe"] = &Method{
				name:  "wave53_lowered_probe",
				owner: vm.cKernel,
				compiled: func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
					vm.setSendNoKW(true)
					vm.setSendVCall(vcall)
					return vm.dispatchSend(self, "nope_xyz", nil, nil)
				},
			}
			bumpMethodSerial()
			got := evalOn(t, vm, &buf, "begin; wave53_lowered_probe; rescue Exception => e; p e.class; end")
			if got != tc.want {
				t.Errorf("a lowered body stating vcall=%v got %s, want %s", vcall, got, tc.want)
			}
		})
	}
}
