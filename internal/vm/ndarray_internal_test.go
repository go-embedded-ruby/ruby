// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"testing"

	nd "github.com/go-ndarray/ndarray"

	"github.com/go-embedded-ruby/ruby/internal/bytecode"
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// TestNDArrayVMlessPaths covers the two NDArray entry points a Ruby program run
// through the VM does not reach.
//
// A number on the LEFT of an operator (10 - a) goes through MRI's coercion
// protocol in binaryOp, i.e. NDArray#coerce; the VM-less binary() — which
// Complex arithmetic and other fast paths call directly — still lands in
// ndarrayOp's scalar-left branch, and must broadcast the same way. And ToS is the
// Go-side rendering a host embedding the VM calls (puts goes through #to_s).
func TestNDArrayVMlessPaths(t *testing.T) {
	a, _ := nd.FromData([]float64{1, 2, 3}, 3)
	got := binary(bytecode.OpSub, object.IntValue(10), &NDArray{a: a})
	if s := got.Inspect(); s != "array([9., 8., 7.])" { // 10 - np.array([1., 2., 3.])
		t.Errorf("10 - a via binary(): got %s", s)
	}
	if s := (&NDArray{a: a}).ToS(); s != "array([1., 2., 3.])" {
		t.Errorf("ToS: got %s", s)
	}
}
