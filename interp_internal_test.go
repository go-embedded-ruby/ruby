package ruby

import (
	"errors"
	"testing"
)

func TestSplitLocation(t *testing.T) {
	for _, c := range []struct {
		in, file, rest string
		line           int
		ok             bool
	}{
		{"a.rb:12:in 'm'", "a.rb", "in 'm'", 12, true},
		{"a.rb:3: oops", "a.rb", "oops", 3, true},
		{"c:/x.rb:4", "c:/x.rb", "", 4, true},
		{"no location", "", "", 0, false},
		{"a.rb:x:", "", "", 0, false},
	} {
		f, l, r, ok := splitLocation(c.in)
		if f != c.file || l != c.line || r != c.rest || ok != c.ok {
			t.Errorf("splitLocation(%q) = %q %d %q %v", c.in, f, l, r, ok)
		}
	}
}

func TestLocate(t *testing.T) {
	bt := []string{"<internal:x>", "lib.rb:5:in 'f'", "cell.rb:9:in '<main>'"}
	if f, l := locate(bt, "cell.rb"); f != "cell.rb" || l != 9 {
		t.Fatalf("locate in file = %s:%d", f, l)
	}
	if f, l := locate(bt, "other.rb"); f != "lib.rb" || l != 5 {
		t.Fatalf("locate fallback = %s:%d", f, l)
	}
}

func TestNewErrorPassesThroughAHostError(t *testing.T) {
	in := New()
	host := errors.New("disk full")
	if err := in.newError(host, ""); err != host {
		t.Fatalf("newError = %v", err)
	}
	if in.wrap(nil).IsNil() != true {
		t.Fatal("wrap(nil) is not nil")
	}
}
