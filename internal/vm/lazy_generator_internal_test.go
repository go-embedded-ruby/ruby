// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestLazyOverAGeneratorIsLazy covers Enumerator::Lazy driven by an Enumerator
// rather than by an Array or a Range.
//
// lazySource materialised anything it did not recognise, and an
// Enumerator.new { |y| loop { … } } has no end: s.lazy.first(3) allocated 8 GB
// and killed a CI runner, which is lazy being defeated at its own source. It is
// pulled now, through the fiber behind Enumerator#next.
//
// Every expectation here was run against ruby 3 rather than reasoned about.
func TestLazyOverAGeneratorIsLazy(t *testing.T) {
	cases := []struct{ src, want string }{
		// The case that used to take the machine, in the three shapes the
		// ruby/spec lazy files use.
		{`s = Enumerator.new { |y| loop { y << 1 } }; p s.lazy.first(3)`, `[1, 1, 1]`},
		{`s = Enumerator.new { |y| loop { y << 1 } }; p s.lazy.take(3).force`, `[1, 1, 1]`},
		{`s = Enumerator.new { |y| loop { y << [1, 2] } }
p s.lazy.take(3).flat_map { |x| x }.force`, `[1, 2, 1, 2, 1, 2]`},
		// A terminal operation starts from the beginning every time.
		{`s = Enumerator.new { |y| loop { y << 1 } }
a = s.lazy.first(3); b = s.lazy.first(3); p [a, b]`, `[[1, 1, 1], [1, 1, 1]]`},
		// And it leaves the enumerator's own #next cursor where it was, which is
		// the reason the pull runs on a copy.
		{`t = Enumerator.new { |y| y << 1; y << 2; y << 3 }
p t.next
p t.lazy.first(2)
p t.next`, "1\n[1, 2]\n2"},
		// A finite generator still ends, and the ops still compose.
		{`t = Enumerator.new { |y| y << 1; y << 2; y << 3 }
p t.lazy.map { |x| x * 10 }.to_a`, `[10, 20, 30]`},
		{`t = Enumerator.new { |y| y << 1; y << 2; y << 3 }
p t.lazy.select { |x| x.odd? }.force`, `[1, 3]`},
		// An Enumerator over something ordinary is pulled the same way.
		{`p [1, 2, 3].each.lazy.map { |x| x + 1 }.force`, `[2, 3, 4]`},
		// The sources that already worked still do.
		{`p (0..Float::INFINITY).lazy.map { |n| n * 2 }.first(3)`, `[0, 2, 4]`},
		{`p [1, 2, 3].lazy.map { |x| x }.force`, `[1, 2, 3]`},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want+"\n" {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want+"\n")
		}
	}
}
