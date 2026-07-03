// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// splitBenchSubject builds a ~100KB whitespace-separated blob of ~20k words to
// exercise the alloc-heavy split path the way String#split does on real input.
func splitBenchSubject() string {
	var b strings.Builder
	for i := 0; i < 20000; i++ {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString("word") // 4-byte words -> ~100KB total
	}
	return b.String()
}

// splitWhitespaceCopy replays the pre-CoW behaviour (object.NewString copies the
// substring bytes) so BenchmarkSplitWhitespaceCopy vs BenchmarkSplitWhitespace
// measures the copy-on-write win on the identical scan.
func splitWhitespaceCopy(subject string) object.Value {
	var out []object.Value
	i := 0
	n := len(subject)
	for i < n {
		for i < n && isASCIISpace(subject[i]) {
			i++
		}
		if i >= n {
			break
		}
		start := i
		for i < n && !isASCIISpace(subject[i]) {
			i++
		}
		out = append(out, object.NewString(subject[start:i]))
	}
	return &object.Array{Elems: out}
}

// BenchmarkSplitWhitespaceCopy is the "before" number: each field copies its
// bytes into a fresh owned slice.
func BenchmarkSplitWhitespaceCopy(b *testing.B) {
	subject := splitBenchSubject()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		splitWhitespaceCopy(subject)
	}
}

// BenchmarkSplitWhitespace is the "after" number: each field is a copy-on-write
// view sharing the subject's immutable bytes.
func BenchmarkSplitWhitespace(b *testing.B) {
	subject := splitBenchSubject()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		splitWhitespace(subject, 0)
	}
}
