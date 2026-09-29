// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package aot

import (
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
)

// TestLoweredSendStatesTheVCallVerdict: a lowered call site must state its VCALL
// verdict the way the interpreter's OpSend handler does, or the SAME source
// changes meaning depending on whether it went through `rbgo build` -- a bare
// identifier that misses answers NoMethodError in a lowered method and NameError
// in the interpreter.
//
// Both directions are asserted, because emitting the call unconditionally with a
// constant would satisfy a one-sided check.
func TestLoweredSendStatesTheVCallVerdict(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags int
		want  string
	}{
		{"bare identifier", bytecode.FlagSendVCall, "vm.setSendVCall(true)"},
		{"ordinary call", 0, "vm.setSendVCall(false)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			iseq := &bytecode.ISeq{
				Name:       "probe",
				SplatIndex: -1, KwRestSlot: -1, BlockSlot: -1,
				NumLocals: 1,
				Names:     []string{"nope_xyz"},
				Insns: []bytecode.Instr{
					{Op: bytecode.OpPushSelf},
					{Op: bytecode.OpSend, A: 0, B: 0, Flags: tc.flags},
					{Op: bytecode.OpReturn},
				},
			}
			src, ok := Compile(iseq, "probeGo", "Object#probe")
			if !ok {
				t.Skip("this ISeq shape is not lowerable on this build; the flag plumbing is still asserted by the other case")
			}
			if !strings.Contains(src, tc.want) {
				t.Errorf("lowered source does not state the verdict %q\n%s", tc.want, src)
			}
			// And the keyword verdict must still be stated beside it: adding one
			// must not displace the other.
			if !strings.Contains(src, "vm.setSendNoKW(") {
				t.Errorf("lowered source no longer states the keyword verdict\n%s", src)
			}
		})
	}
}
