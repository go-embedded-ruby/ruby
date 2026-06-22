package vm

import (
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
	"github.com/go-ruby-marshal/marshal"
)

// stubMarshalValue is a marshal.Value the converter does not handle, used to
// exercise fromMarshalValue's defensive default (the marshal engine itself
// never produces such a value).
type stubMarshalValue struct{}

func (stubMarshalValue) RubyClass() string { return "Stub" }

func TestFromMarshalValueDefault(t *testing.T) {
	defer func() {
		r := recover()
		re, ok := r.(RubyError)
		if !ok || re.Class != "ArgumentError" {
			t.Fatalf("expected ArgumentError panic, got %v", r)
		}
	}()
	fromMarshalValue(stubMarshalValue{}, map[marshal.Value]object.Value{})
}
