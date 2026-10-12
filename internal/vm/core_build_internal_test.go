// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build rbgo_core

package vm

import (
	"bytes"
	"testing"
)

// TestCoreBuild runs only with -tags rbgo_core: the gems are not there, their
// requires raise the core LoadError, nothing of them is half-defined, and the
// numeric stack the playground needs is.
func TestCoreBuild(t *testing.T) {
	if !coreBuild {
		t.Fatal("coreBuild is false under -tags rbgo_core")
	}
	var out bytes.Buffer
	vm := New(&out)
	err := vm.loadSource(`
%w[rails rails/all nokogiri yaml erb optparse uri openssl socket net/http sass slim haml images widgets concurrent].each do |g|
  begin
    require g
    puts "#{g}: loaded"
  rescue LoadError => e
    puts e.message
  end
end
p [defined?(Rails), defined?(Nokogiri), defined?(ERB), defined?(Slim), defined?(Haml), defined?(OptionParser), defined?(Image), defined?(Net), defined?(Concurrent)]
require "json"; require "date"; require "bigdecimal"; require "matrix"; require "prime"
a = NDArray.arange(6).reshape(2, 3)
p a.sum, Set[1, 1, 2].size, Prime.first(3), Matrix[[1, 2], [3, 4]].det
`)
	if err != nil {
		t.Fatal(err)
	}
	want := ""
	for _, g := range []string{"rails", "rails/all", "nokogiri", "yaml", "erb", "optparse", "uri", "openssl", "socket", "net/http", "sass", "slim", "haml", "images", "widgets", "concurrent"} {
		want += "cannot load such file -- " + g + " (not available in the rbgo_core build)\n"
	}
	want += "[nil, nil, nil, nil, nil, nil, nil, nil, nil]\n15.0\n2\n[2, 3, 5]\n-2\n"
	if got := out.String(); got != want {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}
}
