// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "github.com/go-embedded-ruby/ruby/internal/object"

// The rbgo_core build.
//
// `go build -tags rbgo_core` produces an interpreter that keeps the Ruby
// language and its numeric stack and leaves the gem bindings out. It exists for
// the browser: the default js/wasm module links ~160 gem ports and their Go
// backends (go-scss, go-rouge, go-opentype, go-widgets, kramdown, Prawn, the
// Rails stack, …), which a page that only runs NDArray programs downloads and
// never calls. Nothing changes without the tag: coreBuild is a constant, so on
// the default build every `if !coreBuild { … }` in bootstrap is the plain
// sequence of calls it was before, in the same order.
//
// Core is:
//
//   - the Ruby language and its core classes and modules: everything MRI
//     defines without a require (Object, Kernel, Comparable, Enumerable,
//     Integer/Float/Complex/Rational, String, Symbol, Array, Hash, Range, Struct,
//     Data, Proc/Method/Binding, Regexp, Encoding, Exception and its whole tree,
//     Time, Random, Marshal, File/IO/Dir/Process/Signal/ENV, Thread/Fiber,
//     ObjectSpace, GC, Warning, Enumerator, TracePoint, …);
//   - what MRI preloads: set, rubygems (the Gem module), did_you_mean, pp;
//   - the numeric stack: Math, CMath, BigDecimal, Prime, Matrix, Random,
//     SecureRandom, NDArray (go-ndarray), FFT (go-fft), Special
//     (go-numerics/special), Stats (go-statistics/stats), Integrate
//     (go-numerics/integrate) and Optimize (go-numerics/optimize);
//   - small, pure-computation standard libraries a program in a page may
//     plausibly require: json, date, csv, digest, base64, strscan, tsort,
//     abbrev, benchmark, scanf, logger, forwardable, delegate, singleton,
//     ostruct, pathname, tempfile, tmpdir, fileutils, find, etc, timeout,
//     monitor, observer, stringio, English, rbconfig, objspace, fiber, fcntl,
//     io/nonblock, unicode_normalize, prettyprint, and rbgo's own Bag.
//
// Out of core, by rule rather than one by one:
//
//   - every third-party gem (anything MRI does not ship): Rails and its
//     frameworks, Rack/Sinatra/Hanami/…, the database, queue and cloud clients,
//     template engines, Nokogiri, Prawn, Sass, Rouge, Kramdown, the go-widgets
//     bindings, Image (go-images), RSpec/Minitest, Puppet, …;
//   - the standard libraries that need a network, a terminal, a server or a
//     document toolchain: openssl, socket, net/*, resolv, ipaddr, uri, yaml,
//     erb, cgi, rexml, rss, rdoc, irb, reline, ripper, optparse, getoptlong,
//     open3, syslog, shellwords, zlib, pstore.
//
// The README's "rbgo_core" section carries the same decision as a table.
//
// coreFeatures is that definition as require sees it. On the core build, every
// name require.go provides that is NOT listed here raises LoadError (see
// hookOutOfCore): nothing outside the list can load, including gems added after
// this file was written, which then fail closed instead of half-loading. A
// listed name must be one whose classes the core build really defines;
// TestCoreFeaturesAreProvided and the rbgo_core test lane hold it to that.
var coreFeatures = map[string]bool{
	// Core classes MRI still lets you require.
	"time": true, "fiber": true, "objspace": true, "rbconfig": true,
	"English": true, "fcntl": true, "io/nonblock": true, "stringio": true,
	"etc": true, "unicode_normalize": true, "did_you_mean": true,
	"prettyprint": true,
	// The numeric stack.
	"bigdecimal": true, "cmath": true, "prime": true, "matrix": true,
	"securerandom": true, "random/formatter": true, "bag": true,
	// Small pure-computation standard libraries.
	"json": true, "date": true, "csv": true,
	"digest": true, "digest/md5": true, "digest/sha1": true, "digest/sha2": true,
	"digest/rmd160": true, "digest/bubblebabble": true,
	"base64": true, "strscan": true, "tsort": true, "abbrev": true,
	"benchmark": true, "scanf": true, "logger": true,
	"forwardable": true, "delegate": true, "singleton": true, "ostruct": true,
	"pathname": true, "tempfile": true, "tmpdir": true, "fileutils": true,
	"find": true, "timeout": true, "monitor": true, "observer": true,
}

// hookOutOfCore makes every provided feature outside coreFeatures unloadable:
// its first require raises the core build's LoadError instead of succeeding on a
// gem whose register function was never run. It reads providedFeatures at call
// time, so it must run after the last registration (some add names to the table
// as they register). Only the rbgo_core build calls it (core_on.go); it lives in
// this tag-free file so the default build's tests exercise it directly.
//
// The hook replaces whatever hook a name had. On the core build none of the
// out-of-core register functions ran, so there is nothing to replace there; a
// default-build test calling it on a full VM shows the LoadError wins either way.
//
// Raising from the hook is also what keeps a require of an excluded name from
// reaching the file system: require.go answers a provided feature before it
// searches $LOAD_PATH, so a rails.rb sitting on the load path is never run in
// place of the gem the build left out.
func (vm *VM) hookOutOfCore() {
	if vm.featureHooks == nil {
		vm.featureHooks = map[string]func(){}
	}
	for f := range providedFeatures {
		if coreFeatures[f] {
			continue
		}
		f := f
		vm.featureHooks[f] = func() { vm.raiseCoreUnavailable(f) }
	}
}

// raiseCoreUnavailable raises the LoadError the rbgo_core build produces for a
// require of a gem it leaves out.
func (vm *VM) raiseCoreUnavailable(feature string) object.Value {
	return vm.raiseBuildUnavailable(feature, "the rbgo_core build")
}

// raiseBuildUnavailable is the one LoadError every build that compiles a gem out
// raises for it: the wasm build, the closed-world build and the rbgo_core build
// differ only in the build they name. It clears the just-set loaded marker first
// (require.go marks a provided feature loaded before invoking its hook), so a
// retried require re-raises rather than silently returning false. The message
// is MRI's "cannot load such file", annotated with the reason; the feature name
// is passed as an argument, never as the format, so a name carrying a '%' is
// printed as written.
func (vm *VM) raiseBuildUnavailable(feature, build string) object.Value {
	delete(vm.loaded, "feature:"+feature)
	return raise("LoadError", "cannot load such file -- %s (not available in %s)", feature, build)
}
