package vm_test

import "testing"

// A Comparable-mixing class used across the ordering tests.
const cmpClass = `
class Temp
  include Comparable
  def initialize(d)
    @d = d
  end
  def degrees
    @d
  end
  def <=>(other)
    degrees <=> other.degrees
  end
end
`

// An Enumerable-mixing class wrapping an array.
const enumClass = `
class Nums
  include Enumerable
  def initialize(a)
    @a = a
  end
  def each
    @a.each { |x| yield x }
  end
end
`

func TestSpaceship(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"int_lt", `p(1 <=> 2)`, "-1\n"},
		{"int_eq", `p(2 <=> 2)`, "0\n"},
		{"int_gt", `p(3 <=> 2)`, "1\n"},
		{"int_float", `p(1 <=> 1.5)`, "-1\n"},
		{"float_recv", `p(2.5 <=> 2)`, "1\n"},
		{"int_non_numeric", `p(1 <=> "x")`, "nil\n"},
		{"str_lt", `p("a" <=> "b")`, "-1\n"},
		{"str_gt", `p("b" <=> "a")`, "1\n"},
		{"str_eq", `p("a" <=> "a")`, "0\n"},
		{"str_non_string", `p("a" <=> 1)`, "nil\n"},
		{"obj_default_eq", "a = Object.new\np(a <=> a)", "0\n"},
		{"obj_default_ne", `p(Object.new <=> Object.new)`, "nil\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestObjectIdentityEquality(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"same", "a = Object.new\nputs(a == a)", "true\n"},
		{"different", `puts(Object.new == Object.new)`, "false\n"},
		{"ne_same", "a = Object.new\nputs(a != a)", "false\n"},
		{"ne_different", `puts(Object.new != Object.new)`, "true\n"},
		{"eq_non_object", `puts(Object.new == 5)`, "false\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestComparable(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"lt", "a = Temp.new(10)\nb = Temp.new(20)\nputs(a < b)", "true\n"},
		{"gt", "a = Temp.new(10)\nb = Temp.new(20)\nputs(a > b)", "false\n"},
		{"le", `puts(Temp.new(10) <= Temp.new(10))`, "true\n"},
		{"ge", `puts(Temp.new(20) >= Temp.new(10))`, "true\n"},
		{"eq", `puts(Temp.new(10) == Temp.new(10))`, "true\n"},
		{"ne", `puts(Temp.new(10) == Temp.new(11))`, "false\n"},
		{"between_true", `puts(Temp.new(20).between?(Temp.new(10), Temp.new(30)))`, "true\n"},
		{"between_low", `puts(Temp.new(5).between?(Temp.new(10), Temp.new(30)))`, "false\n"},
		{"between_high", `puts(Temp.new(40).between?(Temp.new(10), Temp.new(30)))`, "false\n"},
		{"clamp_low", `puts(Temp.new(5).clamp(Temp.new(10), Temp.new(30)).degrees)`, "10\n"},
		{"clamp_high", `puts(Temp.new(50).clamp(Temp.new(10), Temp.new(30)).degrees)`, "30\n"},
		{"clamp_mid", `puts(Temp.new(20).clamp(Temp.new(10), Temp.new(30)).degrees)`, "20\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, cmpClass+tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// Fast-path ordering on the built-in numeric/string types stays intact.
func TestFastOrdering(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"int_lt", `puts(1 < 2)`, "true\n"},
		{"int_ge", `puts(3 >= 3)`, "true\n"},
		{"str_lt", `puts("a" < "b")`, "true\n"},
		{"str_gt", `puts("b" > "a")`, "true\n"},
		{"str_le", `puts("a" <= "a")`, "true\n"},
		{"str_ge", `puts("b" >= "a")`, "true\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

func TestEnumerable(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"to_a", `p(Nums.new([3, 1, 2]).to_a)`, "[3, 1, 2]\n"},
		{"map", `p(Nums.new([1, 2, 3]).map { |x| x * 10 })`, "[10, 20, 30]\n"},
		{"select", `p(Nums.new([1, 2, 3]).select { |x| x > 1 })`, "[2, 3]\n"},
		{"reject", `p(Nums.new([1, 2, 3]).reject { |x| x > 1 })`, "[1]\n"},
		{"count", `p(Nums.new([1, 2, 3]).count)`, "3\n"},
		{"include_true", `p(Nums.new([1, 2, 3]).include?(2))`, "true\n"},
		{"include_false", `p(Nums.new([1, 2, 3]).include?(9))`, "false\n"},
		{"sum", `p(Nums.new([1, 2, 3]).sum)`, "6\n"},
		{"min", `p(Nums.new([3, 1, 2]).min)`, "1\n"},
		{"max", `p(Nums.new([3, 1, 2]).max)`, "3\n"},
		{"reduce", `p(Nums.new([1, 2, 3]).reduce(10) { |a, x| a + x })`, "16\n"},
		{"find", `p(Nums.new([1, 2, 3]).find { |x| x > 1 })`, "2\n"},
		{"find_none", `p(Nums.new([1, 2, 3]).find { |x| x > 9 })`, "nil\n"},
		{"any_true", `p(Nums.new([1, 2, 3]).any? { |x| x > 2 })`, "true\n"},
		{"any_false", `p(Nums.new([1, 2, 3]).any? { |x| x > 9 })`, "false\n"},
		{"all_true", `p(Nums.new([1, 2, 3]).all? { |x| x > 0 })`, "true\n"},
		{"all_false", `p(Nums.new([1, 2, 3]).all? { |x| x > 1 })`, "false\n"},
		{"none_true", `p(Nums.new([1, 2, 3]).none? { |x| x > 9 })`, "true\n"},
		{"none_false", `p(Nums.new([1, 2, 3]).none? { |x| x > 2 })`, "false\n"},
		{"each_with_index", "r = []\nNums.new([7, 8]).each_with_index { |x, i| r << [i, x] }\np r", "[[0, 7], [1, 8]]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, enumClass+tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}

// User-defined operator methods, including the index methods [] and []=.
func TestOperatorMethodDefs(t *testing.T) {
	src := `
class Box
  def initialize
    @v = 0
  end
  def [](k)
    @v
  end
  def []=(k, val)
    @v = val
  end
  def +(other)
    Box.new
  end
end
b = Box.new
b[3] = 42
puts b[3]
`
	if got := eval(t, src); got != "42\n" {
		t.Errorf("got %q want %q", got, "42\n")
	}
}
