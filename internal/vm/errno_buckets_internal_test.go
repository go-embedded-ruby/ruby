// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"sort"
	"testing"
)

// TestErrnoBucketsPartitionKnownNames is the guard that the js/wasm break of
// issue #682 needed: it asserts, on whatever target it is compiled for, that the
// three buckets of errno.go's rule PARTITION MRI's known_errors list — every name
// in exactly one bucket, and errnoKnownNameCount names in total.
//
// The count is compared against a LITERAL (errnoKnownNameCount), not against the
// tables' own lengths. TestErrnoClassRegistration compares the registered
// constants to len(errnoNumbers)+len(errnoUndefinedNames)+len(errnoAliases),
// which is an assertion the subject can satisfy by shrinking: move a name out of
// errnoNumbers and forget to add it anywhere and both sides drop by one, so the
// test stays green while Errno.constants loses an entry. This one cannot.
func TestErrnoBucketsPartitionKnownNames(t *testing.T) {
	bucket := map[string]string{}
	add := func(name, which string) {
		if prev, dup := bucket[name]; dup {
			t.Errorf("%s is in two buckets: %s and %s", name, prev, which)
			return
		}
		bucket[name] = which
	}
	for name := range errnoNumbers {
		add(name, "errnoNumbers")
	}
	for name := range errnoAliases {
		add(name, "errnoAliases")
	}
	for _, name := range errnoUndefinedNames {
		add(name, "errnoUndefinedNames")
	}
	if got := len(bucket); got != errnoKnownNameCount {
		t.Errorf("the buckets hold %d distinct names, want MRI's known_errors count %d", got, errnoKnownNameCount)
	}

	// Every per-target name must have landed in a bucket on this target, and in
	// the bucket the per-target files chose. This is what a new name added to
	// errnoPlatformNames but only to one of errno_native.go / errno_wasm.go
	// trips: it would be missing from both platform vars on the other target and
	// so from the buckets entirely.
	for _, name := range errnoPlatformNames {
		_, hasNumber := errnoPlatformNumbers[name]
		switch got := bucket[name]; {
		case hasNumber && got != "errnoNumbers":
			t.Errorf("%s has a platform number but is in %q, want errnoNumbers", name, got)
		case !hasNumber && got != "errnoUndefinedNames":
			t.Errorf("%s has no platform number but is in %q, want errnoUndefinedNames", name, got)
		}
	}

	// The two per-target vars must cover errnoPlatformNames exactly: no name in
	// neither (it would vanish from Errno.constants) and none in both (it would
	// be a number AND a NOERROR constant, which registerErrnoClasses would bind
	// twice).
	covered := make([]string, 0, len(errnoPlatformNames))
	for name := range errnoPlatformNumbers {
		covered = append(covered, name)
	}
	for _, name := range errnoPlatformUndefined {
		if _, both := errnoPlatformNumbers[name]; both {
			t.Errorf("%s is both a platform number and a platform undefined name", name)
		}
		covered = append(covered, name)
	}
	want := append([]string{}, errnoPlatformNames...)
	sort.Strings(covered)
	sort.Strings(want)
	if len(covered) != len(want) {
		t.Fatalf("the per-target files cover %v, want exactly errnoPlatformNames %v", covered, want)
	}
	for i := range want {
		if covered[i] != want[i] {
			t.Fatalf("the per-target files cover %v, want exactly errnoPlatformNames %v", covered, want)
		}
	}
}

// TestMergeErrnoNumbers covers the merge on both shapes it has across rbgo's
// targets: a populated platform table (every target but js/wasm) and an empty one
// (js/wasm). A POSIX lane only ever builds the first, so the second is supplied
// here rather than left to the js lane, which cannot run this package's tests.
func TestMergeErrnoNumbers(t *testing.T) {
	universal := map[string]int64{"EINVAL": 22, "ENOENT": 2}

	full := mergeErrnoNumbers(universal, map[string]int64{"ETXTBSY": 26})
	if len(full) != 3 || full["EINVAL"] != 22 || full["ETXTBSY"] != 26 {
		t.Errorf("merge with a platform table = %v, want the union of both", full)
	}

	none := mergeErrnoNumbers(universal, map[string]int64{})
	if len(none) != 2 || none["ENOENT"] != 2 {
		t.Errorf("merge with an empty platform table = %v, want the universal set", none)
	}
	if _, ok := none["ETXTBSY"]; ok {
		t.Error("merge with an empty platform table must not carry a name from an earlier call")
	}

	// The merge must not alias its inputs: errnoUniversalNumbers is package state
	// several tests read directly.
	if len(universal) != 2 {
		t.Errorf("mergeErrnoNumbers mutated its universal argument: %v", universal)
	}
}
