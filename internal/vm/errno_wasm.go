// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build js && wasm

package vm

// This is the per-target half of the errno bucket rule stated in errno.go, for
// js/wasm. Go's js/wasm syscall package (src/syscall/tables_js.go) carries an
// errno enum of its own that omits ENOTRECOVERABLE, EOWNERDEAD and ETXTBSY —
// referring to them is a compile error, which is what broke the browser build for
// three days (issue #682). wasip1 is NOT affected and is served by
// errno_native.go.
//
// The three therefore take MRI's undefined_error treatment on this target: still
// constants, naming Errno::NOERROR (errno 0), exactly as the 80 names no target
// defines do. Errno.constants keeps all errnoKnownNameCount entries here, so a
// spec that counts them holds in the browser too.
//
// Nothing is hard-coded to a number: the browser has no MRI to witness one
// against, and errno.go's rule is that a name without a number is a NOERROR
// constant, not a name with a number rbgo invented.

// errnoPlatformNumbers gives the numbers for the errnoPlatformNames this target
// defines — none of them.
var errnoPlatformNumbers = map[string]int64{}

// errnoPlatformUndefined lists the errnoPlatformNames this target has NO number
// for: all three.
var errnoPlatformUndefined = []string{"ENOTRECOVERABLE", "EOWNERDEAD", "ETXTBSY"}
