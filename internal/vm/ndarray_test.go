package vm_test

import (
	"strings"
	"testing"
)

// TestNDArray covers the go-ndarray binding: constructors, shape/reshape/to_a,
// element-wise and scalar arithmetic (both operand orders), reductions, and
// linear algebra. Values follow NumPy semantics.
func TestNDArray(t *testing.T) {
	cases := []struct{ src, want string }{
		// Constructors + shape/size/ndim.
		{`p NDArray.from([1, 2, 3, 4], [2, 2]).shape`, "[2, 2]\n"},
		{`p NDArray.from([1, 2, 3, 4], [2, 2]).ndim`, "2\n"},
		{`p NDArray.from([1, 2, 3, 4], [2, 2]).size`, "4\n"},
		{`p NDArray.zeros(2, 3).to_a`, "[0.0, 0.0, 0.0, 0.0, 0.0, 0.0]\n"},
		{`p NDArray.ones(3).to_a`, "[1.0, 1.0, 1.0]\n"},
		{`p NDArray.full(7, 2).to_a`, "[7.0, 7.0]\n"},
		{`p NDArray.arange(0, 5, 1).to_a`, "[0.0, 1.0, 2.0, 3.0, 4.0]\n"},
		{`p NDArray.arange(0, 4).to_a`, "[0.0, 1.0, 2.0, 3.0]\n"}, // default step 1
		{`p NDArray.from([1, 2, 3, 4], [2, 2]).reshape(4).shape`, "[4]\n"},
		// Element-wise arithmetic.
		{`a = NDArray.from([1, 2, 3, 4], [2, 2]); p (a + a).to_a`, "[2.0, 4.0, 6.0, 8.0]\n"},
		{`a = NDArray.from([4, 6, 8, 10], [2, 2]); p (a - NDArray.from([1, 2, 3, 4], [2, 2])).to_a`, "[3.0, 4.0, 5.0, 6.0]\n"},
		{`a = NDArray.from([1, 2, 3, 4], [2, 2]); p (a * a).to_a`, "[1.0, 4.0, 9.0, 16.0]\n"},
		{`a = NDArray.from([2, 4, 6, 8], [2, 2]); p (a / NDArray.from([2, 2, 2, 2], [2, 2])).to_a`, "[1.0, 2.0, 3.0, 4.0]\n"},
		// Scalar arithmetic, array on the left.
		{`a = NDArray.from([1, 2, 3, 4], [2, 2]); p (a + 10).to_a`, "[11.0, 12.0, 13.0, 14.0]\n"},
		{`a = NDArray.from([1, 2, 3, 4], [2, 2]); p (a - 1).to_a`, "[0.0, 1.0, 2.0, 3.0]\n"},
		{`a = NDArray.from([1, 2, 3, 4], [2, 2]); p (a * 2).to_a`, "[2.0, 4.0, 6.0, 8.0]\n"},
		{`a = NDArray.from([2, 4, 6, 8], [2, 2]); p (a / 2).to_a`, "[1.0, 2.0, 3.0, 4.0]\n"},
		// Scalar on the left (broadcast; non-commutative order matters).
		{`a = NDArray.from([1, 2, 3, 4], [2, 2]); p (10 - a).to_a`, "[9.0, 8.0, 7.0, 6.0]\n"},
		{`a = NDArray.from([1, 2, 3, 4], [2, 2]); p (2 * a).to_a`, "[2.0, 4.0, 6.0, 8.0]\n"},
		// Reductions.
		{`a = NDArray.from([1, 2, 3, 4], [2, 2]); p a.sum`, "10.0\n"},
		{`a = NDArray.from([1, 2, 3, 4], [2, 2]); p a.mean`, "2.5\n"},
		{`a = NDArray.from([1, 2, 3, 4], [2, 2]); p a.max`, "4.0\n"},
		{`a = NDArray.from([1, 2, 3, 4], [2, 2]); p a.min`, "1.0\n"},
		// Linear algebra.
		{`a = NDArray.from([1, 2, 3, 4], [2, 2]); p a.matmul(a).to_a`, "[7.0, 10.0, 15.0, 22.0]\n"},
		{`p NDArray.from([1, 2, 3], [3]).dot(NDArray.from([4, 5, 6], [3])).to_a`, "[32.0]\n"},
		// inspect / to_s / class.
		{`p NDArray.from([1, 2, 3, 4], [2, 2]).inspect`, "\"Array(shape=[2 2], data=[1 2 3 4])\"\n"},
		{`p NDArray.from([1, 2], [2]).to_s`, "\"Array(shape=[2], data=[1 2])\"\n"},
		{`p NDArray.zeros(2).class`, "NDArray\n"},
		// The Go Value-interface methods: p → Inspect, puts → ToS, ?: → Truthy.
		{`p NDArray.zeros(2)`, "Array(shape=[2], data=[0 0])\n"},
		{`puts NDArray.zeros(2)`, "Array(shape=[2], data=[0 0])\n"},
		{`p(NDArray.zeros(2) ? "y" : "n")`, "\"y\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNDArrayErrors covers the raising paths: a bad reshape, an empty reduction,
// a non-array matmul operand, non-numeric operands (both orders), and an
// unsupported operator.
func TestNDArrayErrors(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`NDArray.from([1, 2, 3, 4], [2, 2]).reshape(3)`, "ArgumentError"},
		{`NDArray.zeros(0).mean`, "ArgumentError"},
		{`NDArray.zeros(2).matmul("x")`, "TypeError"},
		{`NDArray.from([1, 2, 3, 4], [2, 2]) + "x"`, "TypeError"},
		{`true + NDArray.zeros(2)`, "TypeError"},
		{`NDArray.zeros(2) % NDArray.zeros(2)`, "NoMethodError"},
		{`NDArray.zeros(2) % 2`, "NoMethodError"},
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
