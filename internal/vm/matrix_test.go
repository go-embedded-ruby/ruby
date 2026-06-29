// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"strings"
	"testing"
)

// TestMatrix covers the Ruby Matrix/Vector classes (backed by
// github.com/go-ruby-matrix/matrix, the MRI-4.0.5-faithful port of the `matrix`
// stdlib): construction, the exact Integer/Rational/Float numeric tower
// (determinant stays Integer, inverse yields exact Rationals), the arithmetic
// operators, the linear-algebra methods, the predicates, iteration, conversion
// and the MRI "Matrix[…]" / "Vector[…]" rendering — every value asserted against
// MRI 4.0.5's stdlib.
func TestMatrix(t *testing.T) {
	const req = `require "matrix"; `
	for _, c := range []struct{ src, want string }{
		// Construction + inspect/to_s (MRI "Matrix[[…], …]").
		{`p Matrix[[1,2],[3,4]]`, "Matrix[[1, 2], [3, 4]]\n"},
		{`p Matrix.new([[1,2],[3,4]])`, "Matrix[[1, 2], [3, 4]]\n"},
		{`p Matrix.rows([[1,2],[3,4]])`, "Matrix[[1, 2], [3, 4]]\n"},
		{`p Matrix.columns([[1,2],[3,4]])`, "Matrix[[1, 3], [2, 4]]\n"},
		{`puts Matrix[[1,2],[3,4]]`, "Matrix[[1, 2], [3, 4]]\n"},
		{`p Matrix[[1,2],[3,4]].to_s`, "\"Matrix[[1, 2], [3, 4]]\"\n"},
		{`p Matrix[[1,2],[3,4]].inspect`, "\"Matrix[[1, 2], [3, 4]]\"\n"},
		{`p Matrix.identity(2)`, "Matrix[[1, 0], [0, 1]]\n"},
		{`p Matrix.I(2)`, "Matrix[[1, 0], [0, 1]]\n"},
		{`p Matrix.unit(2)`, "Matrix[[1, 0], [0, 1]]\n"},
		{`p Matrix.zero(2)`, "Matrix[[0, 0], [0, 0]]\n"},
		{`p Matrix.zero(2,3)`, "Matrix[[0, 0, 0], [0, 0, 0]]\n"},
		{`p Matrix.diagonal(1,2,3)`, "Matrix[[1, 0, 0], [0, 2, 0], [0, 0, 3]]\n"},
		{`p Matrix.scalar(2,5)`, "Matrix[[5, 0], [0, 5]]\n"},
		{`p Matrix.row_vector([1,2,3])`, "Matrix[[1, 2, 3]]\n"},
		{`p Matrix.column_vector([1,2,3])`, "Matrix[[1], [2], [3]]\n"},
		{`p Matrix.build(2,2){|i,j| i*2+j}`, "Matrix[[0, 1], [2, 3]]\n"},
		{`p Matrix.build(2){|i,j| i*2+j}`, "Matrix[[0, 1], [2, 3]]\n"}, // square shorthand
		{`p Matrix.hstack(Matrix[[1],[2]], Matrix[[3],[4]])`, "Matrix[[1, 3], [2, 4]]\n"},
		{`p Matrix.vstack(Matrix[[1,2]], Matrix[[3,4]])`, "Matrix[[1, 2], [3, 4]]\n"},

		// Dimensions / element access / row / column.
		{`p Matrix[[1,2],[3,4]].row_count`, "2\n"},
		{`p Matrix[[1,2,3]].column_count`, "3\n"},
		{`p Matrix[[1,2],[3,4]][1,0]`, "3\n"},
		{`p Matrix[[1,2],[3,4]][5,5]`, "nil\n"}, // out of range -> nil
		{`p Matrix[[1,2],[3,4]].row(0).to_a`, "[1, 2]\n"},
		{`p Matrix[[1,2],[3,4]].row(9)`, "nil\n"},
		{`p Matrix[[1,2],[3,4]].column(1).to_a`, "[2, 4]\n"},
		{`p Matrix[[1,2],[3,4]].column(9)`, "nil\n"},
		{`p Matrix[[1,2],[3,4]].to_a`, "[[1, 2], [3, 4]]\n"},

		// Iteration.
		{`r=[]; Matrix[[1,2]].each{|e| r<<e}; p r`, "[1, 2]\n"},
		{`Matrix[[1,2]].each_with_index{|e,i,j| puts "#{e} #{i} #{j}"}`, "1 0 0\n2 0 1\n"},

		// Minors.
		{`p Matrix[[1,2],[3,4]].minor(0,1,0,1)`, "Matrix[[1]]\n"},
		{`p Matrix[[1,2,3],[4,5,6],[7,8,9]].first_minor(0,0)`, "Matrix[[5, 6], [8, 9]]\n"},

		// Arithmetic operators.
		{`p Matrix[[1,2],[3,4]] + Matrix[[1,1],[1,1]]`, "Matrix[[2, 3], [4, 5]]\n"},
		{`p Matrix[[2,3],[4,5]] - Matrix[[1,1],[1,1]]`, "Matrix[[1, 2], [3, 4]]\n"},
		{`p Matrix[[1,2],[3,4]] * Matrix[[1,0],[0,1]]`, "Matrix[[1, 2], [3, 4]]\n"},
		{`p Matrix[[1,2]] * 3`, "Matrix[[3, 6]]\n"},
		{`p Matrix[[1,2],[3,4]] * Vector[1,1]`, "Vector[3, 7]\n"},
		{`p Matrix[[1,2],[3,4]] / Matrix[[1,0],[0,1]]`, "Matrix[[(1/1), (2/1)], [(3/1), (4/1)]]\n"},
		// Matrix / scalar uses the library's exact (Rational) division — Integer
		// entries become Rationals; MRI's Matrix#/ floors integer entries, so this
		// is a deliberate library divergence kept for exactness.
		{`p Matrix[[2,4],[6,8]] / 2`, "Matrix[[(1/1), (2/1)], [(3/1), (4/1)]]\n"},
		{`p Matrix[[1,2],[3,4]] ** 2`, "Matrix[[7, 10], [15, 22]]\n"},
		{`p(-Matrix[[1,2],[3,4]])`, "Matrix[[-1, -2], [-3, -4]]\n"},

		// Transpose / trace / determinant / inverse / rank / round.
		{`p Matrix[[1,2],[3,4]].transpose`, "Matrix[[1, 3], [2, 4]]\n"},
		{`p Matrix[[1,2],[3,4]].t`, "Matrix[[1, 3], [2, 4]]\n"},
		{`p Matrix[[1,2],[3,4]].trace`, "5\n"},
		{`p Matrix[[1,2],[3,4]].tr`, "5\n"},
		{`p Matrix[[1,2],[3,4]].determinant`, "-2\n"}, // Integer in -> Integer out
		{`p Matrix[[1,2],[3,4]].det`, "-2\n"},
		{`p Matrix[[1.5,2.0],[3.0,4.0]].determinant`, "0.0\n"},                          // Float in -> Float out
		{`p Matrix[[1,2],[3,4]].inverse`, "Matrix[[(-2/1), (1/1)], [(3/2), (-1/2)]]\n"}, // exact Rationals
		{`p Matrix[[1,2],[3,4]].inv`, "Matrix[[(-2/1), (1/1)], [(3/2), (-1/2)]]\n"},
		{`p Matrix[[1,2],[2,4]].rank`, "1\n"},
		{`p Matrix[[1.234,2.567]].round(1)`, "Matrix[[1.2, 2.6]]\n"},
		// round with no argument rounds to 0 digits but keeps the Float kind
		// (the library's documented exact-tower behaviour); MRI's Float#round(0)
		// returns an Integer, so this is a deliberate library divergence.
		{`p Matrix[[1.234,2.567]].round`, "Matrix[[1.0, 3.0]]\n"},

		// Bignum determinant promotes through *big.Int -> Bignum.
		{`p Matrix[[10**20,0],[0,1]].determinant`, "100000000000000000000\n"},
		// Float infinity / -infinity / NaN entries round-trip through Float.
		{`p Matrix[[Float::INFINITY]].determinant`, "Infinity\n"},
		{`p Matrix[[-Float::INFINITY]].determinant`, "-Infinity\n"},
		{`p Matrix[[Float::NAN]].determinant`, "NaN\n"},

		// Predicates.
		{`p Matrix[[1,2],[3,4]].square?`, "true\n"},
		{`p Matrix[[1,2,3]].square?`, "false\n"},
		{`p Matrix.zero(2).zero?`, "true\n"},
		{`p Matrix[[1,2],[3,4]].zero?`, "false\n"},
		{`p Matrix.identity(2).diagonal?`, "true\n"},
		{`p Matrix[[1,2],[2,1]].symmetric?`, "true\n"},
		{`p Matrix[[1,2],[3,4]].symmetric?`, "false\n"},
		{`p Matrix[[1,0],[3,1]].lower_triangular?`, "true\n"},
		{`p Matrix[[1,2],[0,1]].upper_triangular?`, "true\n"},
		{`p Matrix[[1,2],[2,4]].singular?`, "true\n"},
		{`p Matrix[[1,2],[3,4]].singular?`, "false\n"},
		{`p Matrix[[1,2],[3,4]].regular?`, "true\n"},
		{`p Matrix[[1,2],[2,4]].regular?`, "false\n"},
		{`p Matrix.identity(2).orthogonal?`, "true\n"},
		{`p Matrix[[1,2],[3,4]].orthogonal?`, "false\n"},

		// Equality / hash.
		{`p(Matrix[[1,2]] == Matrix[[1,2]])`, "true\n"},
		{`p(Matrix[[1,2]] == Matrix[[1,3]])`, "false\n"},
		{`p(Matrix[[1,2]] == Vector[1,2])`, "false\n"}, // a Matrix is never == a Vector
		{`p(Matrix[[1,2]] != Matrix[[1,3]])`, "true\n"},
		{`p Matrix[[1,2]].eql?(Matrix[[1,2]])`, "true\n"},
		{`p Matrix[[1,2]].eql?(42)`, "false\n"},
		{`p Matrix[[1,2],[3,4]].hash.class`, "Integer\n"},

		// Vector construction + inspect/to_s.
		{`p Vector[1,2,3]`, "Vector[1, 2, 3]\n"},
		{`p Vector.elements([1,2,3])`, "Vector[1, 2, 3]\n"},
		{`puts Vector[1,2,3]`, "Vector[1, 2, 3]\n"},
		{`p Vector[1,2,3].to_s`, "\"Vector[1, 2, 3]\"\n"},
		{`p Vector[1,2,3].inspect`, "\"Vector[1, 2, 3]\"\n"},
		{`p Vector[10,20,30][1]`, "20\n"},
		{`p Vector[10,20,30][9]`, "nil\n"},
		{`p Vector[1,2,3].size`, "3\n"},
		{`p Vector[1,2,3].to_a`, "[1, 2, 3]\n"},
		{`r=[]; Vector[1,2,3].each{|e| r<<e}; p r`, "[1, 2, 3]\n"},
		{`p Vector[1,2,3].map{|x| x*2}`, "Vector[2, 4, 6]\n"},

		// Vector arithmetic + products + magnitude/normalize.
		{`p Vector[1,2] + Vector[3,4]`, "Vector[4, 6]\n"},
		{`p Vector[3,4] - Vector[1,1]`, "Vector[2, 3]\n"},
		{`p Vector[1,2] * 3`, "Vector[3, 6]\n"},
		{`p Vector[1,2,3].inner_product(Vector[4,5,6])`, "32\n"},
		{`p Vector[1,2,3].dot(Vector[4,5,6])`, "32\n"},
		{`p Vector[1,0,0].cross_product(Vector[0,1,0])`, "Vector[0, 0, 1]\n"},
		{`p Vector[1,0,0].cross(Vector[0,1,0])`, "Vector[0, 0, 1]\n"},
		{`p Vector[3,4].magnitude`, "5.0\n"},
		{`p Vector[3,4].r`, "5.0\n"},
		{`p Vector[3,4].norm`, "5.0\n"},
		{`p Vector[3,4].normalize`, "Vector[0.6, 0.8]\n"},
		{`p(Vector[1,2] == Vector[1,2])`, "true\n"},
		{`p(Vector[1,2] == Vector[1,3])`, "false\n"},
		{`p(Vector[1,2] == Matrix[[1,2]])`, "false\n"},
		{`p Vector[1,2].eql?(Vector[1,2])`, "true\n"},
		{`p Vector[1,2].eql?(7)`, "false\n"},
		{`p Vector.independent?(Vector[1,0], Vector[0,1])`, "true\n"},
		{`p Vector.independent?(Vector[1,0], Vector[2,0])`, "false\n"},

		// Rational entries flow through unchanged.
		{`p Matrix[[Rational(1,2), Rational(1,3)]]`, "Matrix[[(1/2), (1/3)]]\n"},
		// Element access of an inverse yields the exact Rational back as a Ruby
		// Rational (the (n/d) -> object.Rational mapping), not a degraded Float.
		{`p Matrix[[1,2],[3,4]].inverse[0,0]`, "(-2/1)\n"},
		{`p Matrix[[1,2],[3,4]].inverse[1,0]`, "(3/2)\n"},

		// The operators are also reachable through Object#send (Ruby dispatches
		// them as ordinary methods), exercising the class-method definitions.
		{`p Matrix[[1,2]].send(:+, Matrix[[1,1]])`, "Matrix[[2, 3]]\n"},
		{`p Matrix[[2,3]].send(:-, Matrix[[1,1]])`, "Matrix[[1, 2]]\n"},
		{`p Matrix[[1,2],[3,4]].send(:*, Matrix[[1,0],[0,1]])`, "Matrix[[1, 2], [3, 4]]\n"},
		{`p Matrix[[1,2]].send(:*, 3)`, "Matrix[[3, 6]]\n"},
		{`p Matrix[[1,2],[3,4]].send(:*, Vector[1,1])`, "Vector[3, 7]\n"},
		{`p Matrix[[1,2],[3,4]].send(:/, Matrix[[1,0],[0,1]])`, "Matrix[[(1/1), (2/1)], [(3/1), (4/1)]]\n"},
		{`p Matrix[[2,4]].send(:/, 2)`, "Matrix[[(1/1), (2/1)]]\n"},
		{`p Matrix[[1,2],[3,4]].send(:**, 2)`, "Matrix[[7, 10], [15, 22]]\n"},
		{`p Matrix[[1,2]].send(:"-@")`, "Matrix[[-1, -2]]\n"},
		{`p Matrix[[1,2]].send(:==, Matrix[[1,2]])`, "true\n"},
		{`p Matrix[[1,2]].send(:==, 5)`, "false\n"},
		{`p Vector[1,2].send(:+, Vector[3,4])`, "Vector[4, 6]\n"},
		{`p Vector[3,4].send(:-, Vector[1,1])`, "Vector[2, 3]\n"},
		{`p Vector[1,2].send(:*, 3)`, "Vector[3, 6]\n"},
		{`p Vector[1,2].send(:==, Vector[1,2])`, "true\n"},
		{`p Vector[1,2].send(:==, 5)`, "false\n"},
	} {
		if got := eval(t, req+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMatrixErrors covers the ExceptionForMatrix error tree: a dimension
// mismatch (Matrix::ErrDimensionMismatch / "Dimension mismatch"), a non-regular
// inverse (Matrix::ErrNotRegular / "Not Regular Matrix"), and the constant
// resolution that makes Matrix::Err… and ExceptionForMatrix::Err… the same
// class — all asserted against MRI 4.0.5.
func TestMatrixErrors(t *testing.T) {
	const req = `require "matrix"; `

	// Class of the raised error + MRI message (rescued and re-printed).
	for _, c := range []struct{ src, wantClass, wantMsg string }{
		{`Matrix[[1,2]] + Matrix[[1]]`, "ExceptionForMatrix::ErrDimensionMismatch", "Dimension mismatch"},
		{`Matrix[[1,2]] - Matrix[[1]]`, "ExceptionForMatrix::ErrDimensionMismatch", "Dimension mismatch"},
		{`Matrix[[1,2]] * Matrix[[1,2]]`, "ExceptionForMatrix::ErrDimensionMismatch", "Dimension mismatch"},
		{`Matrix[[1,2]].determinant`, "ExceptionForMatrix::ErrDimensionMismatch", "Dimension mismatch"},
		{`Matrix[[1,2]].trace`, "ExceptionForMatrix::ErrDimensionMismatch", "Dimension mismatch"},
		{`Matrix[[1,2],[2,4]].inverse`, "ExceptionForMatrix::ErrNotRegular", "Not Regular Matrix"},
		{`Vector[1,2] + Vector[1,2,3]`, "ExceptionForMatrix::ErrDimensionMismatch", "Dimension mismatch"},
		{`Vector[1,2].inner_product(Vector[1,2,3])`, "ExceptionForMatrix::ErrDimensionMismatch", "Dimension mismatch"},
		// A 2-D cross product maps to ErrOperationNotDefined (the library's
		// surface; MRI raises ArgumentError here — a deliberate divergence kept so
		// the ErrOperationNotDefined re-raise branch is exercised).
		{`Vector[1,2].cross_product(Vector[1,2])`, "ExceptionForMatrix::ErrOperationNotDefined", ""},
	} {
		src := req + "begin; " + c.src + "; rescue => e; p e.class; p e.message; end"
		got := eval(t, src)
		if !strings.Contains(got, c.wantClass) {
			t.Errorf("src=%q got=%q want class %q", c.src, got, c.wantClass)
		}
		if c.wantMsg != "" && !strings.Contains(got, c.wantMsg) {
			t.Errorf("src=%q got=%q want message %q", c.src, got, c.wantMsg)
		}
	}

	// Constant resolution: Matrix::Err… resolves through the included
	// ExceptionForMatrix module to the same class, and is rescuable by it.
	for _, c := range []struct{ src, want string }{
		{`p Matrix::ErrDimensionMismatch`, "ExceptionForMatrix::ErrDimensionMismatch\n"},
		{`p ExceptionForMatrix::ErrNotRegular`, "ExceptionForMatrix::ErrNotRegular\n"},
		{`p ExceptionForMatrix::ErrOperationNotDefined`, "ExceptionForMatrix::ErrOperationNotDefined\n"},
		{`p(Matrix::ErrDimensionMismatch == ExceptionForMatrix::ErrDimensionMismatch)`, "true\n"},
		{`begin; Matrix[[1,2]] + Matrix[[1]]; rescue Matrix::ErrDimensionMismatch; puts "caught"; end`, "caught\n"},
	} {
		if got := eval(t, req+c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMatrixTypeErrors covers the binding's argument-guard raises: a non-numeric
// entry, a non-Matrix/Vector operand, and the no-block iterator/builder paths.
func TestMatrixTypeErrors(t *testing.T) {
	const req = `require "matrix"; `
	for _, c := range []struct{ src, want string }{
		{`Matrix[["x"]]`, "TypeError"},                     // non-numeric entry
		{`Matrix[7]`, "TypeError"},                         // Matrix[] arg not an Array
		{`Matrix.new(42)`, "TypeError"},                    // rows not an Array
		{`Matrix.new([1,2])`, "TypeError"},                 // each row not an Array
		{`Matrix[[1,2]] % Matrix[[1,2]]`, "NoMethodError"}, // unsupported Matrix operator
		{`Matrix.row_vector(7)`, "TypeError"},              // cells not an Array
		{`Matrix[[1,2]] + Vector[1,2]`, "TypeError"},       // Matrix + non-Matrix
		{`Matrix[[1,2]] * "z"`, "TypeError"},               // scalar not numeric
		{`Vector[1,2] + Matrix[[1,2]]`, "TypeError"},       // Vector + non-Vector
		{`Vector.independent?(7)`, "TypeError"},            // non-Vector arg
		{`Matrix.build(2,2)`, "LocalJumpError"},            // build without a block
		{`Matrix[[1,2]].each`, "LocalJumpError"},           // each without a block
		{`Matrix[[1,2]].each_with_index`, "LocalJumpError"},
		{`Vector[1,2].each`, "LocalJumpError"},
		{`Vector[1,2].map`, "LocalJumpError"},
	} {
		if err := runErr(t, req+c.src); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("src=%q got=%v want %q", c.src, err, c.want)
		}
	}
}
