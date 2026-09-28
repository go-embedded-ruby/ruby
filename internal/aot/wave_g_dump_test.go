package aot

import (
	"os"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/compiler"
	"github.com/go-ruby-parser/parser"
)

func TestWaveGDump(t *testing.T) {
	src, _ := os.ReadFile("/tmp/wave-g-vcall/wave-g-aot-recvonly.rb")
	p, err := parser.Parse(string(src))
	if err != nil {
		t.Fatal(err)
	}
	iseq, err := compiler.Compile(p)
	if err != nil {
		t.Fatal(err)
	}
	content, keys, ok := CompileProgram(iseq)
	t.Log("ok", ok, "keys", keys)
	os.WriteFile("/tmp/wave-g-vcall/wave-g-aot-gen.go.txt", []byte(content), 0o644)
}
