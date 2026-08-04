// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build wasm

package vm

// flockFile is a pure no-op under js/wasm, for the same reason as on Windows:
// there is no flock(2) (syscall.Flock is undefined on wasm), and the browser
// runtime is single-process, so the advisory cross-process lock has nothing to
// serialise against. PStore still works correctly; only the cross-process lock —
// meaningless here — is skipped.
func flockFile(path string, readOnly bool) (func(), error) {
	return func() {}, nil
}
