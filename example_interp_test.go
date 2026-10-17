package ruby_test

import (
	"fmt"
	"os"

	ruby "github.com/go-embedded-ruby/ruby"
)

// An interpreter keeps its top-level locals between evaluations, takes Go
// values in, and calls Go methods that call Ruby blocks back.
func ExampleInterp() {
	in := ruby.New(ruby.WithStdout(os.Stdout))
	in.Set("xs", []float64{1, 2, 3})

	m, _ := in.DefineModule("Calc")
	m.DefineSingletonMethod("sum_by", func(c *ruby.Call) (any, error) {
		total := 0.0
		for _, x := range c.Args[0].Go().([]any) {
			y, err := c.Block.Call(x)
			if err != nil {
				return nil, err
			}
			total += y.Go().(float64)
		}
		return total, nil
	})

	in.Eval("squares = Calc.sum_by(xs) { |x| x * x }", "example.rb", 1)
	v, _ := in.Eval(`puts "sum of squares: #{squares}"; squares / 2`, "example.rb", 2)
	got, _ := in.Get("squares")
	fmt.Println(v.Inspect(), got)

	_, err := in.Eval("Calc.nope", "example.rb", 3)
	fmt.Println(err)
	// Output:
	// sum of squares: 14.0
	// 7.0 14
	// example.rb:3: undefined method 'nope' for module Calc (NoMethodError)
}
