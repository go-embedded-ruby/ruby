// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestIOAdvise covers IO#advise (rb_io_advise): the six recognized advice types
// as validated no-ops returning nil, the optional offset/len (present, nil, and
// coercion errors), and the error branches — a non-Symbol advice (TypeError), an
// unrecognized one (NotImplementedError "Unsupported advice: :sym"), wrong arity
// (ArgumentError), a non-Integer or too-large offset/len (TypeError / RangeError),
// and a closed stream (IOError). Every line verified byte-for-byte against ruby
// 4.0.5 on darwin. File-backed via a per-test temp dir so no machine file is
// touched; the asserted classes/messages are platform-independent.
func TestIOAdvise(t *testing.T) {
	dir := t.TempDir()
	path := filepath.ToSlash(filepath.Join(dir, "advise.txt"))
	src := fmt.Sprintf(`
f = File.open(%q, "w+")
p f.advise(:normal)
p f.advise(:sequential)
p f.advise(:random)
p f.advise(:willneed)
p f.advise(:dontneed)
p f.advise(:noreuse)
p f.advise(:normal, 0)
p f.advise(:normal, nil)
p f.advise(:normal, 0, 0)
p f.advise(:normal, 0, nil)
begin; f.advise("normal"); rescue Exception => e; p e.class; end
begin; f.advise(:foo); rescue Exception => e; p [e.class, e.message]; end
begin; f.advise; rescue Exception => e; p e.class; end
begin; f.advise(:normal, 0, 0, 0); rescue Exception => e; p e.class; end
begin; f.advise(:normal, "wat"); rescue Exception => e; p e.class; end
begin; f.advise(:normal, 0, "wat"); rescue Exception => e; p e.class; end
begin; f.advise(:normal, 10 ** 32); rescue Exception => e; p e.class; end
begin; f.advise(:normal, 0, 10 ** 32); rescue Exception => e; p e.class; end
f.close
begin; f.advise(:normal); rescue Exception => e; p [e.class, e.message]; end
`, path)
	want := "nil\nnil\nnil\nnil\nnil\nnil\n" +
		"nil\nnil\nnil\nnil\n" +
		"TypeError\n" +
		"[NotImplementedError, \"Unsupported advice: :foo\"]\n" +
		"ArgumentError\n" +
		"ArgumentError\n" +
		"TypeError\n" +
		"TypeError\n" +
		"RangeError\n" +
		"RangeError\n" +
		"[IOError, \"closed stream\"]\n"
	if got := eval(t, src); got != want {
		t.Errorf("src=%q\n got=%q\nwant=%q", src, got, want)
	}
}
