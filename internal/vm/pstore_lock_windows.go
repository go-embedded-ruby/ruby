// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package vm

// flockFile is a pure no-op on Windows: it opens NOTHING. Windows has no advisory
// flock(2), so there is no lock to take; and, crucially, holding the store file
// open would make the atomic-rename Store fail with "Access is denied" (Windows
// forbids renaming over an open file). PStore still works correctly single-process
// there — the lock is only advisory cross-process serialisation, which Windows
// callers do without here.
func flockFile(path string, readOnly bool) (func(), error) {
	return func() {}, nil
}
