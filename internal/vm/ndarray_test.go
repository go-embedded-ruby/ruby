package vm_test

import (
	"math"
	"strconv"
	"strings"
	"testing"

	nd "github.com/go-ndarray/ndarray"

	"github.com/go-embedded-ruby/ruby/internal/vm"
)

// The NDArray binding implements the Ruby API the go-ndarray playground emits
// (ruby-ndarray-contract.md: Go | Ruby | NumPy). Every expected value in this
// file is derived by hand from NumPy's definition of the operation — never by
// running the binding — and, where go-ndarray has the operation, the same value
// is also computed through go-ndarray's Go API directly (the oracle the
// playground's Go column uses), so a disagreement names which side moved.

// TestNDArray covers the binding's original surface: constructors, shape /
// reshape / to_a, element-wise and scalar arithmetic (both operand orders),
// reductions, and linear algebra.
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
		// inspect / to_s / class: NumPy's repr.
		{`p NDArray.from([1, 2, 3, 4], [2, 2]).inspect`, "\"array([[1., 2.],\\n       [3., 4.]])\"\n"},
		{`p NDArray.from([1, 2], [2]).to_s`, "\"array([1., 2.])\"\n"},
		{`p NDArray.zeros(2).class`, "NDArray\n"},
		// Unary ufuncs, prod, argmax/argmin, element access.
		{`p NDArray.from([1, 4, 9, 16], [4]).sqrt.to_a`, "[1.0, 2.0, 3.0, 4.0]\n"},
		{`p NDArray.from([0, 1, 2], [3]).exp.to_a.map { |x| x.round(4) }`, "[1.0, 2.7183, 7.3891]\n"},
		{`p NDArray.from([1, -2, 3], [3]).abs.to_a`, "[1.0, 2.0, 3.0]\n"},
		{`p NDArray.from([1, 2, 3, 4, 5, 6], [2, 3]).transpose.shape`, "[3, 2]\n"},
		{`p NDArray.from([1, 2, 3, 4, 5, 6], [2, 3]).transpose.to_a`, "[1.0, 4.0, 2.0, 5.0, 3.0, 6.0]\n"},
		{`p NDArray.from([1, 2, 3, 4], [2, 2]).flatten.shape`, "[4]\n"},
		{`p NDArray.from([1, 2, 3, 4], [4]).prod`, "24.0\n"},
		{`p NDArray.from([3, 1, 4, 1, 5], [5]).argmax`, "4\n"},
		{`p NDArray.from([3, 1, 4, 1, 5], [5]).argmin`, "1\n"},
		{`p NDArray.from([1, 2, 3, 4], [2, 2]).at(1, 0)`, "3.0\n"},
		{`p NDArray.from([1, 2, 3, 4], [2, 2])[0, 1]`, "2.0\n"},
		{`p NDArray.from([1, 2], [2]).neg.to_a`, "[-1.0, -2.0]\n"},
		{`p NDArray.from([1, 100], [2]).log.to_a.map { |x| x.round(4) }`, "[0.0, 4.6052]\n"},
		{`p NDArray.from([0], [1]).sin.to_a`, "[0.0]\n"},
		{`p NDArray.from([0], [1]).cos.to_a`, "[1.0]\n"},
		// The Go Value-interface methods: p → Inspect, puts → ToS, ?: → Truthy.
		{`p NDArray.zeros(2)`, "array([0., 0.])\n"},
		{`puts NDArray.zeros(2)`, "array([0., 0.])\n"},
		{`p(NDArray.zeros(2) ? "y" : "n")`, "\"y\"\n"},
		{`p "#{NDArray.ones(1)}"`, "\"array([1.])\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNDArrayContract covers every row of the contract outside slicing. Each
// expectation is NumPy's, worked out by hand from the operation's definition;
// the comment beside a row is the NumPy expression it mirrors.
func TestNDArrayContract(t *testing.T) {
	// m is [[0, 1, 2], [3, 4, 5]] throughout.
	const m = `m = NDArray.arange(6).reshape(2, 3); `
	cases := []struct{ src, want string }{
		// --- Creation ---
		{`p NDArray.arange(5).to_a`, "[0.0, 1.0, 2.0, 3.0, 4.0]\n"},                   // np.arange(5)
		{`p NDArray.arange(2, 5).to_a`, "[2.0, 3.0, 4.0]\n"},                          // np.arange(2, 5)
		{`p NDArray.arange(1, 2, 0.25).to_a`, "[1.0, 1.25, 1.5, 1.75]\n"},             // stop excluded
		{`p NDArray.arange(5, 0, -2).to_a`, "[5.0, 3.0, 1.0]\n"},                      // negative step
		{`p NDArray.arange(5, 0).shape`, "[0]\n"},                                     // empty
		{`p NDArray.arange(2.5).to_a`, "[0.0, 1.0, 2.0]\n"},                           // ceil(2.5) = 3 elements
		{`p NDArray.linspace(0, 1, 5).to_a`, "[0.0, 0.25, 0.5, 0.75, 1.0]\n"},         // endpoint included
		{`p NDArray.linspace(2, 3, 1).to_a`, "[2.0]\n"},                               // num=1 is [start]
		{`p NDArray.linspace(0, 1, 0).shape`, "[0]\n"},                                // num=0 is empty
		{`a = NDArray.linspace(0, 49); p [a.size, a[1], a[-1]]`, "[50, 1.0, 49.0]\n"}, // num defaults to 50
		{`p NDArray.zeros(2, 3).shape`, "[2, 3]\n"},
		{`p NDArray.zeros([2, 3]).shape`, "[2, 3]\n"}, // np.zeros((2, 3))
		{`p NDArray.ones(2, 2).to_a`, "[1.0, 1.0, 1.0, 1.0]\n"},
		{`z = NDArray.zeros(); p [z.shape, z.ndim, z.size, z.to_a]`, "[[], 0, 1, [0.0]]\n"}, // 0-d
		{`p NDArray.zeros()`, "array(0.)\n"},                                                // repr(np.zeros(()))
		{`p NDArray.full(7, 2, 2).to_a`, "[7.0, 7.0, 7.0, 7.0]\n"},
		{`p NDArray.full(1.5).shape`, "[]\n"},
		{`p NDArray.eye(3).to_a`, "[1.0, 0.0, 0.0, 0.0, 1.0, 0.0, 0.0, 0.0, 1.0]\n"},        // np.eye(3)
		{`p NDArray.eye(2, 3).to_a`, "[1.0, 0.0, 0.0, 0.0, 1.0, 0.0]\n"},                    // np.eye(2, 3)
		{`p NDArray.eye(3, 3, 1).to_a`, "[0.0, 1.0, 0.0, 0.0, 0.0, 1.0, 0.0, 0.0, 0.0]\n"},  // superdiagonal
		{`p NDArray.eye(3, 3, -1).to_a`, "[0.0, 0.0, 0.0, 1.0, 0.0, 0.0, 0.0, 1.0, 0.0]\n"}, // subdiagonal
		{`p NDArray.eye(2, 2, 5).to_a`, "[0.0, 0.0, 0.0, 0.0]\n"},                           // k past the edge
		{`p NDArray.from([1, 2, 3, 4], [2, 2]).to_a`, "[1.0, 2.0, 3.0, 4.0]\n"},

		// --- Views and shape ---
		{m + `p m.reshape(3, -1).shape`, "[3, 2]\n"},
		{m + `p m.reshape([3, 2]).shape`, "[3, 2]\n"},
		{m + `p m.reshape(-1).to_a`, "[0.0, 1.0, 2.0, 3.0, 4.0, 5.0]\n"},
		{m + `p m.transpose.to_a`, "[0.0, 3.0, 1.0, 4.0, 2.0, 5.0]\n"}, // m.T
		{m + `p m.T.shape`, "[3, 2]\n"},
		{m + `p m.T.flatten.to_a`, "[0.0, 3.0, 1.0, 4.0, 2.0, 5.0]\n"}, // row-major of m.T
		{m + `c = m.dup; p [c.equal?(m), c.shape, c.to_a == m.to_a]`, "[false, [2, 3], true]\n"},
		{m + `c = m.copy; p [c.equal?(m), c.to_a == m.to_a]`, "[false, true]\n"},
		{`p NDArray.zeros(1, 3, 1).squeeze.shape`, "[3]\n"},       // every length-1 axis
		{`p NDArray.zeros(1, 3, 1).squeeze(0).shape`, "[3, 1]\n"}, // np.squeeze(a, 0)
		{`p NDArray.zeros(1, 3, 1).squeeze(0, 2).shape`, "[3]\n"},
		{`p NDArray.zeros(1, 3, 1).squeeze(-1).shape`, "[1, 3]\n"},
		{m + `p m.expand_dims(0).shape`, "[1, 2, 3]\n"},
		{m + `p m.expand_dims(1).shape`, "[2, 1, 3]\n"},
		{m + `p m.expand_dims(-1).shape`, "[2, 3, 1]\n"},

		// --- Elementwise ---
		{`p NDArray.from([1, -2, 3], [3]).square.to_a`, "[1.0, 4.0, 9.0]\n"},
		{`p (NDArray.from([1, -2, 3], [3]) ** 2).to_a`, "[1.0, 4.0, 9.0]\n"},
		{`p (NDArray.from([1, 4, 9], [3]) ** 0.5).to_a`, "[1.0, 2.0, 3.0]\n"},
		{`p (NDArray.from([1, 2, 4], [3]) ** -1).to_a`, "[1.0, 0.5, 0.25]\n"},
		{`p (NDArray.from([0], [1]) ** -1).to_a`, "[Infinity]\n"},                                                                  // 0.0 ** -1 = inf
		{`p (NDArray.from([1, 2, 3, 4], [2, 2]) ** NDArray.from([2, 1], [2])).to_a`, "[1.0, 2.0, 9.0, 4.0]\n"},                     // (2,2)**(2,)
		{`r = NDArray.from([2, 3], [2]) ** NDArray.from([1, 2], [2, 1]); p [r.shape, r.to_a]`, "[[2, 2], [2.0, 3.0, 4.0, 9.0]]\n"}, // (2,)**(2,1)
		{`p (2 ** NDArray.from([0, 1, 3], [3])).to_a`, "[1.0, 2.0, 8.0]\n"},                                                        // 2 ** a
		{`p (2.0 ** NDArray.from([-1, 0.5], [2])).to_a`, "[0.5, 1.4142135623730951]\n"},
		{`p NDArray.from([1, 2, 3, 4, 5], [5]).clip(2, 4).to_a`, "[2.0, 2.0, 3.0, 4.0, 4.0]\n"},
		{`p NDArray.from([-1.5, 0.5], [2]).clip(0, 1).to_a`, "[0.0, 0.5]\n"},
		{`p NDArray.maximum(NDArray.from([1, 5, 3], [3]), NDArray.from([4, 2, 6], [3])).to_a`, "[4.0, 5.0, 6.0]\n"},
		{`p NDArray.minimum(NDArray.from([1, 5, 3], [3]), NDArray.from([4, 2, 6], [3])).to_a`, "[1.0, 2.0, 3.0]\n"},
		{m + `p NDArray.maximum(m, 2.5).to_a`, "[2.5, 2.5, 2.5, 3.0, 4.0, 5.0]\n"},                          // scalar broadcasts
		{m + `p NDArray.minimum(2, m).to_a`, "[0.0, 1.0, 2.0, 2.0, 2.0, 2.0]\n"},                            // either side
		{m + `p NDArray.maximum(m, NDArray.from([4, 0, 4], [3])).to_a`, "[4.0, 1.0, 4.0, 4.0, 4.0, 5.0]\n"}, // (2,3) vs (3,)
		{m + `p (m + NDArray.from([10, 20, 30], [3])).to_a`, "[10.0, 21.0, 32.0, 13.0, 24.0, 35.0]\n"},
		{m + `p (1 - m).to_a`, "[1.0, 0.0, -1.0, -2.0, -3.0, -4.0]\n"},
		{`p ((2**64) - NDArray.zeros(1)).to_a`, "[1.8446744073709552e+19]\n"}, // a Bignum on the left
		{`p (Rational(1, 2) + NDArray.zeros(2)).to_a`, "[0.5, 0.5]\n"},

		// --- Reductions ---
		{m + `p m.sum`, "15.0\n"},
		{m + `p m.sum(nil)`, "15.0\n"},
		{m + `p m.sum(0).to_a`, "[3.0, 5.0, 7.0]\n"},
		{m + `p m.sum(1).to_a`, "[3.0, 12.0]\n"},
		{m + `p m.sum(-1).to_a`, "[3.0, 12.0]\n"},
		{m + `r = m.sum(1, keepdims: true); p [r.shape, r.to_a]`, "[[2, 1], [3.0, 12.0]]\n"},
		{m + `r = m.sum(keepdims: true); p [r.shape, r.to_a]`, "[[1, 1], [15.0]]\n"}, // axis=None, keepdims
		{m + `r = m.sum(0, keepdims: false); p r.shape`, "[3]\n"},
		{`p NDArray.full(2).sum(keepdims: true).shape`, "[]\n"},
		{`p NDArray.zeros(0).sum`, "0.0\n"}, // the empty sum is 0
		{m + `p m.mean`, "2.5\n"},
		{m + `p m.mean(0).to_a`, "[1.5, 2.5, 3.5]\n"},
		{m + `p m.mean(1).to_a`, "[1.0, 4.0]\n"},
		{m + `p m.max`, "5.0\n"},
		{m + `p m.max(0).to_a`, "[3.0, 4.0, 5.0]\n"},
		{m + `r = m.max(1, keepdims: true); p [r.shape, r.to_a]`, "[[2, 1], [2.0, 5.0]]\n"},
		{m + `p m.min`, "0.0\n"},
		{m + `p m.min(1).to_a`, "[0.0, 3.0]\n"},
		{`p NDArray.from([1, 2, 3, 4], [2, 2]).prod(0).to_a`, "[3.0, 8.0]\n"},
		{`p NDArray.from([1, 2, 3, 4], [2, 2]).prod(1).to_a`, "[2.0, 12.0]\n"},
		// argmax/argmin of [[3, 1, 4], [1, 5, 9]]: flat index; ties take the first.
		{`p NDArray.from([3, 1, 4, 1, 5, 9], [2, 3]).argmax`, "5\n"},
		{`p NDArray.from([3, 1, 4, 1, 5, 9], [2, 3]).argmin`, "1\n"},
		{`p NDArray.from([1, 3, 3], [3]).argmax`, "1\n"},
		{`p NDArray.from([3, 1, 4, 1, 5, 9], [2, 3]).argmax(0).to_a`, "[0.0, 1.0, 1.0]\n"},
		{`p NDArray.from([3, 1, 4, 1, 5, 9], [2, 3]).argmax(1).to_a`, "[2.0, 2.0]\n"},
		{`p NDArray.from([3, 1, 4, 1, 5, 9], [2, 3]).argmin(1).to_a`, "[1.0, 0.0]\n"},
		{`r = NDArray.from([3, 1, 4, 1, 5, 9], [2, 3]).argmax(1, keepdims: true); p [r.shape, r.to_a]`, "[[2, 1], [2.0, 2.0]]\n"},
		{`r = NDArray.from([3, 1, 4, 1, 5, 9], [2, 3]).argmax(keepdims: true); p [r.shape, r.to_a]`, "[[1, 1], [5.0]]\n"},
		{`p NDArray.from([3, 1, 4, 1, 5, 9], [2, 3]).argmax(1)`, "array([2, 2])\n"}, // integer indices
		{`p NDArray.from([3, 1, 4, 1, 5, 9], [2, 3]).argmin(nil)`, "1\n"},
		{m + `p m.cumsum.to_a`, "[0.0, 1.0, 3.0, 6.0, 10.0, 15.0]\n"}, // flattened
		{m + `p m.cumsum(nil).shape`, "[6]\n"},
		{m + `p m.cumsum(0).to_a`, "[0.0, 1.0, 2.0, 3.0, 5.0, 7.0]\n"},
		{m + `p m.cumsum(1).to_a`, "[0.0, 1.0, 3.0, 3.0, 7.0, 12.0]\n"},

		// --- Products and joins ---
		{`r = NDArray.outer(NDArray.from([1, 2], [2]), NDArray.from([3, 4, 5], [3])); p [r.shape, r.to_a]`, "[[2, 3], [3.0, 4.0, 5.0, 6.0, 8.0, 10.0]]\n"},
		{m + `p NDArray.outer(m, NDArray.from([1, 2], [2])).shape`, "[6, 2]\n"}, // inputs are flattened
		{`p NDArray.outer(2, NDArray.from([1, 2], [2])).to_a`, "[2.0, 4.0]\n"},
		{m + `r = NDArray.concatenate([m, NDArray.ones(1, 3)], axis: 0); p [r.shape, r.to_a]`, "[[3, 3], [0.0, 1.0, 2.0, 3.0, 4.0, 5.0, 1.0, 1.0, 1.0]]\n"},
		{m + `p NDArray.concatenate([m, m]).shape`, "[4, 3]\n"}, // axis defaults to 0
		{m + `r = NDArray.concatenate([m, m], axis: 1); p [r.shape, r.to_a]`, "[[2, 6], [0.0, 1.0, 2.0, 0.0, 1.0, 2.0, 3.0, 4.0, 5.0, 3.0, 4.0, 5.0]]\n"},
		{m + `p NDArray.concatenate([m, m], axis: -1).shape`, "[2, 6]\n"},
		{`p NDArray.concatenate([NDArray.arange(2), NDArray.arange(3)]).to_a`, "[0.0, 1.0, 0.0, 1.0, 2.0]\n"},
		{`r = NDArray.stack([NDArray.from([1, 2], [2]), NDArray.from([3, 4], [2])]); p [r.shape, r.to_a]`, "[[2, 2], [1.0, 2.0, 3.0, 4.0]]\n"},
		{`r = NDArray.stack([NDArray.from([1, 2], [2]), NDArray.from([3, 4], [2])], axis: 1); p [r.shape, r.to_a]`, "[[2, 2], [1.0, 3.0, 2.0, 4.0]]\n"},
		{m + `p NDArray.stack([m, m], axis: 1).shape`, "[2, 2, 3]\n"},
		{m + `p NDArray.stack([m, m], axis: -1).shape`, "[2, 3, 2]\n"},

		// --- Inspecting ---
		{m + `p m`, "array([[0., 1., 2.],\n       [3., 4., 5.]])\n"}, // repr(np.arange(6.).reshape(2, 3))
		{m + `puts m.to_s`, "array([[0., 1., 2.],\n       [3., 4., 5.]])\n"},
		{`p NDArray.arange(3)`, "array([0., 1., 2.])\n"},
		{`p NDArray.zeros(0)`, "array([])\n"},
		{m + `p [m.shape, m.ndim, m.size]`, "[[2, 3], 2, 6]\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}

// rubyFloats renders a []float64 the way Ruby's Array#inspect prints Floats.
func rubyFloats(fs []float64) string {
	parts := make([]string, len(fs))
	for i, f := range fs {
		s := strconv.FormatFloat(f, 'f', -1, 64)
		switch {
		case math.IsInf(f, 1):
			s = "Infinity"
		case math.IsInf(f, -1):
			s = "-Infinity"
		case !strings.Contains(s, "."):
			s += ".0"
		}
		parts[i] = s
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// rubyInts renders a shape the way Ruby's Array#inspect prints Integers.
func rubyInts(is []int) string {
	parts := make([]string, len(is))
	for i, n := range is {
		parts[i] = strconv.Itoa(n)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// flat reads an array's elements in row-major order.
func flat(a *nd.Array) []float64 {
	f := a.Flatten()
	out := make([]float64, f.Size())
	for i := range out {
		out[i] = f.At(i)
	}
	return out
}

// TestNDArraySlicing walks the contract's indexing table row by row, plus the
// inclusive ranges, the negative steps and the multi-axis forms. Each row holds
// three things that must agree: the Ruby index, the elements NumPy's slice
// rules give (worked out by hand: start, stop exclusive, step, negative bounds
// counted from the end, absent bounds at the natural extreme for the step's
// sign), and the same slice taken through go-ndarray's Go API.
func TestNDArraySlicing(t *testing.T) {
	v, _ := nd.Arange(0, 10, 1) // [0 .. 9]
	m0, _ := nd.Arange(0, 12, 1)
	m, _ := m0.Reshape(3, 4) // [[0 1 2 3] [4 5 6 7] [8 9 10 11]]
	const rv = `a = NDArray.arange(10); `
	const rm = `a = NDArray.arange(12).reshape(3, 4); `
	cases := []struct {
		setup, ruby, numpy string
		base               *nd.Array
		oracle             []nd.Index
		shape              []int
		want               []float64
	}{
		// The contract's table, in order.
		{rv, `true`, `:`, v, []nd.Index{nd.All()}, []int{10}, []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{rv, `1...3`, `1:3`, v, []nd.Index{nd.R(1, 3)}, []int{2}, []float64{1, 2}},
		{rv, `1..`, `1:`, v, []nd.Index{nd.From(1)}, []int{9}, []float64{1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{rv, `...3`, `:3`, v, []nd.Index{nd.To(3)}, []int{3}, []float64{0, 1, 2}},
		{rv, `(0..) % 2`, `::2`, v, []nd.Index{nd.Step(2)}, []int{5}, []float64{0, 2, 4, 6, 8}},
		{rv, `(1...7) % 3`, `1:7:3`, v, []nd.Index{nd.Rng(1, 7, 3)}, []int{2}, []float64{1, 4}},
		{rv, `(-1..) % -1`, `::-1`, v, []nd.Index{nd.Step(-1)}, []int{10}, []float64{9, 8, 7, 6, 5, 4, 3, 2, 1, 0}},
		{rv, `(5...1) % -2`, `5:1:-2`, v, []nd.Index{nd.Rng(5, 1, -2)}, []int{2}, []float64{5, 3}},
		// Inclusive ranges (as in Numo): the end is kept.
		{rv, `1..2`, `1:3`, v, []nd.Index{nd.R(1, 3)}, []int{2}, []float64{1, 2}},
		{rv, `1..-1`, `1:`, v, []nd.Index{nd.From(1)}, []int{9}, []float64{1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{rv, `0..-2`, `0:-1`, v, []nd.Index{nd.R(0, -1)}, []int{9}, []float64{0, 1, 2, 3, 4, 5, 6, 7, 8}},
		{rv, `..2`, `:3`, v, []nd.Index{nd.To(3)}, []int{3}, []float64{0, 1, 2}},
		{rv, `-3...-1`, `-3:-1`, v, []nd.Index{nd.R(-3, -1)}, []int{2}, []float64{7, 8}},
		{rv, `-3..-1`, `-3:`, v, []nd.Index{nd.From(-3)}, []int{3}, []float64{7, 8, 9}},
		{rv, `(5..1) % -2`, `5:0:-2`, v, []nd.Index{nd.Rng(5, 0, -2)}, []int{3}, []float64{5, 3, 1}},
		{rv, `(5..0) % -1`, `5::-1`, v, []nd.Index{nd.Rng(5, -11, -1)}, []int{6}, []float64{5, 4, 3, 2, 1, 0}},
		{rv, `(-2..-5) % -1`, `-2:-6:-1`, v, []nd.Index{nd.Rng(-2, -6, -1)}, []int{4}, []float64{8, 7, 6, 5}},
		{rv, `(..-3) % -1`, `:-4:-1`, v, []nd.Index{nd.Rng(-1, -4, -1)}, []int{3}, []float64{9, 8, 7}},
		{rv, `(..5) % 2`, `:6:2`, v, []nd.Index{nd.Rng(0, 6, 2)}, []int{3}, []float64{0, 2, 4}},
		{rv, `(8..) % -3`, `8::-3`, v, []nd.Index{nd.Rng(8, -11, -3)}, []int{3}, []float64{8, 5, 2}},
		// Numeric#step and Range#step build the same ArithmeticSequence.
		{rv, `1.step(7, 3)`, `1:8:3`, v, []nd.Index{nd.Rng(1, 8, 3)}, []int{3}, []float64{1, 4, 7}},
		{rv, `(5..1).step(-1)`, `5:0:-1`, v, []nd.Index{nd.Rng(5, 0, -1)}, []int{5}, []float64{5, 4, 3, 2, 1}},
		{rv, `(5...1).step(-1)`, `5:1:-1`, v, []nd.Index{nd.Rng(5, 1, -1)}, []int{4}, []float64{5, 4, 3, 2}},
		// Bounds clamp as in NumPy; an empty or out-of-range span is empty.
		{rv, `8...2`, `8:2`, v, []nd.Index{nd.R(8, 2)}, []int{0}, nil},
		{rv, `5..100`, `5:101`, v, []nd.Index{nd.R(5, 101)}, []int{5}, []float64{5, 6, 7, 8, 9}},
		{rv, `(0..) % 20`, `::20`, v, []nd.Index{nd.Step(20)}, []int{1}, []float64{0}},
		// Several axes; an Integer drops its axis, trailing axes are whole.
		{rm, `1`, `[1]`, m, []nd.Index{nd.A(1)}, []int{4}, []float64{4, 5, 6, 7}},
		{rm, `-1`, `[-1]`, m, []nd.Index{nd.A(-1)}, []int{4}, []float64{8, 9, 10, 11}},
		{rm, `true, 1`, `[:, 1]`, m, []nd.Index{nd.All(), nd.A(1)}, []int{3}, []float64{1, 5, 9}},
		{rm, `1..2, (0..) % 2`, `[1:3, ::2]`, m, []nd.Index{nd.R(1, 3), nd.Step(2)}, []int{2, 2}, []float64{4, 6, 8, 10}},
		{rm, `(-1..) % -1, (-1..) % -1`, `[::-1, ::-1]`, m, []nd.Index{nd.Step(-1), nd.Step(-1)}, []int{3, 4}, []float64{11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0}},
		{rm, `(-1..) % -1`, `[::-1]`, m, []nd.Index{nd.Step(-1)}, []int{3, 4}, []float64{8, 9, 10, 11, 4, 5, 6, 7, 0, 1, 2, 3}},
		{rm, `0...0`, `[0:0]`, m, []nd.Index{nd.R(0, 0)}, []int{0, 4}, nil},
		{rm, `1, 1...3`, `[1, 1:3]`, m, []nd.Index{nd.A(1), nd.R(1, 3)}, []int{2}, []float64{5, 6}},
		{rm, `(2..0) % -2, -1`, `[2::-2, -1]`, m, []nd.Index{nd.Rng(2, -4, -2), nd.A(-1)}, []int{2}, []float64{11, 3}},
	}
	for _, c := range cases {
		// The oracle first: go-ndarray must agree with the hand-derived answer,
		// or the row itself is wrong.
		o, err := c.base.Slice(c.oracle...)
		if err != nil {
			t.Fatalf("numpy %s: oracle: %v", c.numpy, err)
		}
		if rubyInts(o.Shape()) != rubyInts(c.shape) || rubyFloats(flat(o)) != rubyFloats(c.want) {
			t.Fatalf("numpy %s: oracle gives %v %v, hand-derived %v %v", c.numpy, o.Shape(), flat(o), c.shape, c.want)
		}
		src := c.setup + `r = a[` + c.ruby + `]; p r.shape; p r.to_a`
		want := rubyInts(c.shape) + "\n" + rubyFloats(c.want) + "\n"
		if got := eval(t, src); got != want {
			t.Errorf("a[%s] (numpy %s): got %q want %q", c.ruby, c.numpy, got, want)
		}
	}

	// All-Integer indexing, one per axis, is the element as a Float.
	for _, c := range []struct{ src, want string }{
		{rv + `p a[0]`, "0.0\n"},
		{rv + `p a[-1]`, "9.0\n"},
		{rm + `p a[1, 2]`, "6.0\n"},
		{rm + `p a[-1, -1]`, "11.0\n"},
		{rm + `p a.at(2, 0)`, "8.0\n"},
		{rm + `p a[1].class`, "NDArray\n"}, // fewer indices than axes: a view
		{`p NDArray.full(3)[]`, "3.0\n"},   // 0-d: no index is one per axis
		{rm + `p a[]`, "array([[ 0.,  1.,  2.,  3.],\n       [ 4.,  5.,  6.,  7.],\n       [ 8.,  9., 10., 11.]])\n"},
		{rm + `p a[true, 1...3][1, 0]`, "5.0\n"}, // a view is indexed in its own coordinates
	} {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestNDArrayMatchesGoOracle compares the Ruby result's repr with go-ndarray's
// Repr of the same computation done in Go, for the operations whose printed
// form the playground shows side by side.
func TestNDArrayMatchesGoOracle(t *testing.T) {
	must := func(a *nd.Array, err error) *nd.Array {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	m := func() *nd.Array { return must(must(nd.Arange(0, 6, 1)).Reshape(2, 3)) }
	const rm = `m = NDArray.arange(6).reshape(2, 3); `
	cases := []struct {
		src string
		gov func() *nd.Array
	}{
		{`p NDArray.linspace(0, 1, 5)`, func() *nd.Array { return must(nd.Linspace(0, 1, 5)) }},
		{`p NDArray.arange(1, 2, 0.25)`, func() *nd.Array { return must(nd.Arange(1, 2, 0.25)) }},
		{`p NDArray.eye(3, 4, 1)`, func() *nd.Array { return must(nd.Eye(3, 4, 1)) }},
		{`p NDArray.full(7, 2, 2)`, func() *nd.Array { return must(nd.Full(7, 2, 2)) }},
		{rm + `p m.T`, func() *nd.Array { return m().Transpose() }},
		{rm + `p m ** 2`, func() *nd.Array { return m().Power(2) }},
		{rm + `p m ** 0.5`, func() *nd.Array { return m().Power(0.5) }},
		{rm + `p m.square`, func() *nd.Array { return m().Square() }},
		{rm + `p m.exp`, func() *nd.Array { return m().Exp() }},
		{rm + `p m.clip(1, 4)`, func() *nd.Array { return must(m().Clip(1, 4)) }},
		{rm + `p m.sum(0)`, func() *nd.Array { return must(m().SumAxis(0, false)) }},
		{rm + `p m.mean(1, keepdims: true)`, func() *nd.Array { return must(m().MeanAxis(1, true)) }},
		{rm + `p m.argmax(0)`, func() *nd.Array { return must(m().ArgMaxAxis(0, false)) }},
		{rm + `p m.cumsum(1)`, func() *nd.Array { return must(m().CumSum(1)) }},
		{rm + `p m.cumsum`, func() *nd.Array { return m().CumSumFlat() }},
		{rm + `p NDArray.outer(m, m)`, func() *nd.Array { return m().Outer(m()) }},
		{rm + `p NDArray.maximum(m, 2)`, func() *nd.Array { return must(m().Maximum(must(nd.Full(2)))) }},
		{rm + `p NDArray.concatenate([m, m], axis: 1)`, func() *nd.Array { return must(nd.Concatenate([]*nd.Array{m(), m()}, 1)) }},
		{rm + `p NDArray.stack([m, m], axis: 2)`, func() *nd.Array { return must(nd.Stack([]*nd.Array{m(), m()}, 2)) }},
		{rm + `p m.reshape(3, -1)`, func() *nd.Array { return must(m().Reshape(3, -1)) }},
		{rm + `p m.expand_dims(0).squeeze`, func() *nd.Array { return must(must(m().ExpandDims(0)).Squeeze()) }},
		{rm + `p m[(-1..) % -1, 1..]`, func() *nd.Array { return must(m().Slice(nd.Step(-1), nd.From(1))) }},
		{rm + `p m.matmul(m.T)`, func() *nd.Array { return must(m().MatMul(m().Transpose())) }},
		{`p NDArray.from([0.5, -1, 1e10], [3])`, func() *nd.Array { return must(nd.FromData([]float64{0.5, -1, 1e10}, 3)) }},
		{`p NDArray.arange(2000)`, func() *nd.Array { return must(nd.Arange(0, 2000, 1)) }}, // summarised
	}
	for _, c := range cases {
		want := c.gov().Repr() + "\n"
		if got := eval(t, c.src); got != want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, want)
		}
	}

	// array ** array has no go-ndarray counterpart; NumPy's definition is
	// elementwise math.Pow after broadcasting, which is checked here directly.
	x := []float64{-2, 0, 0.5, 3}
	y := []float64{3, -1, 2, 0.5}
	want := make([]float64, len(x))
	for i := range x {
		want[i] = math.Pow(x[i], y[i])
	}
	got := eval(t, `p (NDArray.from([-2, 0, 0.5, 3], [4]) ** NDArray.from([3, -1, 2, 0.5], [4])).to_a`)
	if exp := rubyFloats(want) + "\n"; got != exp {
		t.Errorf("array ** array: got %q want %q", got, exp)
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
		{`NDArray.zeros(0).argmax`, "ArgumentError"}, // empty arg-reduction
	} {
		if err := runErr(t, c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}

// TestNDArrayArgumentChecks calls every method with too few arguments, too many,
// and wrong types. Each must raise the named Ruby exception — and none may leak
// a Go runtime fault: before this check, NDArray.arange(5) raised
// "ArgumentError: arange: runtime error: index out of range [1] with length 1",
// a recovered panic from indexing a missing argument.
func TestNDArrayArgumentChecks(t *testing.T) {
	const a = `a = NDArray.arange(6).reshape(2, 3); `
	for _, c := range []struct{ src, class, msg string }{
		// Class methods.
		{`NDArray.zeros("x")`, "TypeError", "no implicit conversion of String into Integer"},
		{`NDArray.zeros(-1)`, "ArgumentError", "negative dimension"},
		{`NDArray.ones(nil)`, "TypeError", "no implicit conversion of nil into Integer"},
		{`NDArray.ones([2, 1.5])`, "TypeError", "Float into Integer"},
		{`NDArray.full`, "ArgumentError", "wrong number of arguments (given 0, expected 1+)"},
		{`NDArray.full("x")`, "TypeError", "can't convert String into Float"},
		{`NDArray.full(1, true)`, "TypeError", "no implicit conversion of true into Integer"},
		{`NDArray.arange`, "ArgumentError", "wrong number of arguments (given 0, expected 1..3)"},
		{`NDArray.arange(1, 2, 3, 4)`, "ArgumentError", "wrong number of arguments (given 4, expected 1..3)"},
		{`NDArray.arange("x")`, "TypeError", "can't convert String into Float"},
		{`NDArray.arange(0, 5, 0)`, "ArgumentError", "step must be non-zero"},
		{`NDArray.linspace`, "ArgumentError", "wrong number of arguments (given 0, expected 2..3)"},
		{`NDArray.linspace(0)`, "ArgumentError", "(given 1, expected 2..3)"},
		{`NDArray.linspace(0, 1, 2, 3)`, "ArgumentError", "(given 4, expected 2..3)"},
		{`NDArray.linspace(:a, 1)`, "TypeError", "can't convert Symbol into Float"},
		{`NDArray.linspace(0, 1, 2.5)`, "TypeError", "Float into Integer"},
		{`NDArray.linspace(0, 1, -1)`, "ArgumentError", "num must be non-negative"},
		{`NDArray.eye`, "ArgumentError", "(given 0, expected 1..3)"},
		{`NDArray.eye(1, 2, 3, 4)`, "ArgumentError", "(given 4, expected 1..3)"},
		{`NDArray.eye("3")`, "TypeError", "String into Integer"},
		{`NDArray.eye(2, "x")`, "TypeError", "String into Integer"},
		{`NDArray.eye(2, 2, 1.0)`, "TypeError", "Float into Integer"},
		{`NDArray.eye(-1)`, "ArgumentError", "negative dimension"},
		{`NDArray.from`, "ArgumentError", "(given 0, expected 2)"},
		{`NDArray.from([1])`, "ArgumentError", "(given 1, expected 2)"},
		{`NDArray.from("x", [1])`, "TypeError", "into Array"},
		{`NDArray.from([1], "x")`, "TypeError", "into Array"},
		{`NDArray.from([1, 2], [3])`, "ArgumentError", "shape mismatch"},
		{`NDArray.maximum`, "ArgumentError", "(given 0, expected 2)"},
		{a + `NDArray.maximum(a)`, "ArgumentError", "(given 1, expected 2)"},
		{a + `NDArray.maximum(a, "x")`, "TypeError", "no implicit conversion of String into NDArray"},
		{a + `NDArray.minimum(nil, a)`, "TypeError", "no implicit conversion of nil into NDArray"},
		{a + `NDArray.maximum(a, NDArray.zeros(4))`, "ArgumentError", "not broadcastable"},
		{a + `NDArray.outer(a)`, "ArgumentError", "(given 1, expected 2)"},
		{a + `NDArray.outer(a, [1])`, "TypeError", "Array into NDArray"},
		{`NDArray.concatenate`, "ArgumentError", "(given 0, expected 1)"},
		{a + `NDArray.concatenate(a)`, "TypeError", "no implicit conversion of NDArray into Array"},
		{a + `NDArray.concatenate([a, 1])`, "TypeError", "Integer into NDArray"},
		{a + `NDArray.concatenate([a], axis: "0")`, "TypeError", "String into Integer"},
		{a + `NDArray.concatenate([a], axes: 0)`, "ArgumentError", "unknown keyword: :axes"},
		{a + `NDArray.concatenate([a], {"axis" => 0})`, "ArgumentError", `unknown keyword: "axis"`},
		{a + `NDArray.concatenate([a, NDArray.zeros(3)])`, "ArgumentError", "shape mismatch"},
		{`NDArray.concatenate([])`, "ArgumentError", "at least one array"},
		{a + `NDArray.concatenate([a], axis: 2)`, "IndexError", "axis out of range"},
		{`NDArray.stack`, "ArgumentError", "(given 0, expected 1)"},
		{`NDArray.stack([1])`, "TypeError", "Integer into NDArray"},
		{a + `NDArray.stack([a, NDArray.zeros(3)])`, "ArgumentError", "shape mismatch"},

		// Instance methods of no argument.
		{a + `a.shape(1)`, "ArgumentError", "(given 1, expected 0)"},
		{a + `a.ndim(1)`, "ArgumentError", "(given 1, expected 0)"},
		{a + `a.size(1)`, "ArgumentError", "(given 1, expected 0)"},
		{a + `a.to_a(1)`, "ArgumentError", "(given 1, expected 0)"},
		{a + `a.inspect(1)`, "ArgumentError", "(given 1, expected 0)"},
		{a + `a.to_s(1)`, "ArgumentError", "(given 1, expected 0)"},
		{a + `a.exp(1)`, "ArgumentError", "(given 1, expected 0)"},
		{a + `a.square(1, 2)`, "ArgumentError", "(given 2, expected 0)"},
		{a + `a.T(1)`, "ArgumentError", "(given 1, expected 0)"},
		{a + `a.dup(1)`, "ArgumentError", "(given 1, expected 0)"},
		{a + `a.flatten(0)`, "ArgumentError", "(given 1, expected 0)"},

		// Instance methods with arguments.
		{a + `a.reshape("x")`, "TypeError", "String into Integer"},
		{a + `a.reshape(4, -1)`, "ArgumentError", "cannot infer -1"},
		{a + `a.reshape`, "ArgumentError", "shape mismatch"},
		{a + `a.squeeze("x")`, "TypeError", "String into Integer"},
		{a + `a.squeeze(0)`, "ArgumentError", "cannot squeeze axis 0"},
		{a + `a.squeeze(7)`, "IndexError", "axis out of range"},
		{a + `a.expand_dims`, "ArgumentError", "(given 0, expected 1)"},
		{a + `a.expand_dims(nil)`, "TypeError", "nil into Integer"},
		{a + `a.expand_dims(5)`, "IndexError", "axis out of range"},
		{a + `a.clip`, "ArgumentError", "(given 0, expected 2)"},
		{a + `a.clip(1)`, "ArgumentError", "(given 1, expected 2)"},
		{a + `a.clip("a", 1)`, "TypeError", "can't convert String into Float"},
		{a + `a.clip(1, nil)`, "TypeError", "can't convert nil into Float"},
		{a + `a.clip(4, 1)`, "ArgumentError", "clip bounds lo=4 > hi=1"},
		{a + `a.send(:**)`, "ArgumentError", "(given 0, expected 1)"},
		{a + `a ** "x"`, "TypeError", "no implicit conversion of String into NDArray"},
		{a + `a ** NDArray.zeros(4)`, "ArgumentError", "not broadcastable"},
		{a + `"x" ** a`, "NoMethodError", ""},
		{a + `a.coerce`, "ArgumentError", "(given 0, expected 1)"},
		{a + `a.coerce("x")`, "TypeError", "can't convert String into Float"},
		{a + `a.matmul`, "ArgumentError", "(given 0, expected 1)"},
		{a + `a.matmul(a)`, "ArgumentError", "MatMul"},
		{a + `a.dot`, "ArgumentError", "(given 0, expected 1)"},
		{a + `a.dot(1)`, "TypeError", "into NDArray"},
		{a + `a.sum(0, 1)`, "ArgumentError", "(given 2, expected 0..1)"},
		{a + `a.sum("x")`, "TypeError", "String into Integer"},
		{a + `a.sum(2)`, "IndexError", "axis out of range"},
		{a + `a.sum(keep: true)`, "ArgumentError", "unknown keyword: :keep"},
		{a + `a.mean(0, 1)`, "ArgumentError", "(given 2, expected 0..1)"},
		{a + `a.max(:x)`, "TypeError", "Symbol into Integer"},
		{a + `a.min(-3)`, "IndexError", "axis out of range"},
		{a + `a.prod(1, 2)`, "ArgumentError", "(given 2, expected 0..1)"},
		{`NDArray.zeros(0).max(keepdims: true)`, "ArgumentError", "shape mismatch"},
		{`NDArray.zeros(2, 0).max(1)`, "ArgumentError", "zero-length axis"},
		{a + `a.argmax(1, 2)`, "ArgumentError", "(given 2, expected 0..1)"},
		{a + `a.argmax("x")`, "TypeError", "String into Integer"},
		{a + `a.argmin(axis: 1)`, "ArgumentError", "unknown keyword: :axis"},
		{a + `a.argmax(4)`, "IndexError", "axis out of range"},
		{`NDArray.zeros(0).argmin(keepdims: true)`, "ArgumentError", "empty"},
		{a + `a.cumsum(1, 2)`, "ArgumentError", "(given 2, expected 0..1)"},
		{a + `a.cumsum("x")`, "TypeError", "String into Integer"},
		{a + `a.cumsum(3)`, "IndexError", "axis out of range"},

		// Indexing.
		{a + `a["x"]`, "TypeError", "no implicit conversion of String into an NDArray index"},
		{a + `a[nil]`, "TypeError", "nil into an NDArray index"},
		{a + `a[false]`, "TypeError", "false into an NDArray index"},
		{a + `a[1.5]`, "TypeError", "Float into an NDArray index"},
		{a + `a[[0, 1]]`, "TypeError", "Array into an NDArray index"},
		{a + `a[[1, 2].each]`, "TypeError", "Enumerator into an NDArray index"},
		{a + `a[1.0..2]`, "TypeError", "Float into Integer"},
		{a + `a[0..2.5]`, "TypeError", "Float into Integer"},
		{a + `a[(0..) % 0.5]`, "TypeError", "Float into Integer"},
		{a + `a[2]`, "IndexError", "index 2 for axis of length 2"},
		{a + `a[0, -4]`, "IndexError", "index out of range"},
		{a + `a[0, 0, 0]`, "IndexError", "got 3 indices for 2-dimensional array"},
		{a + `a[true, true, 1..]`, "IndexError", "got 3 indices for 2-dimensional array"},
		{a + `a.at(9, 9)`, "IndexError", "index out of range"},
	} {
		err := runErr(t, c.src)
		if err == nil {
			t.Errorf("src=%q raised nothing, want %s", c.src, c.class)
			continue
		}
		re, ok := err.(vm.RubyError)
		if !ok || re.Class != c.class || !strings.Contains(re.Message, c.msg) {
			t.Errorf("src=%q\n got %v\nwant %s containing %q", c.src, err, c.class, c.msg)
		}
		if strings.Contains(err.Error(), "runtime error") {
			t.Errorf("src=%q leaked a Go runtime fault: %v", c.src, err)
		}
	}
}
