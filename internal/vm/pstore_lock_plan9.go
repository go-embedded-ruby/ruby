// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build plan9

package vm

// flockFile is a no-op on Plan 9: there is no flock(2) (syscall.Flock is
// undefined), so there is no advisory lock to take. PStore still works correctly
// single-process; only the cross-process serialisation is skipped, as on Windows
// (pstore_lock_windows.go) and wasm.
func flockFile(path string, readOnly bool) (func(), error) {
	return func() {}, nil
}
