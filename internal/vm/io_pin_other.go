// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows || wasm || plan9

package vm

import "os"

// pinFile holds nothing here. On Windows an open handle would refuse the
// file's deletion and renaming, which a stream must not do, and NTFS file IDs
// carry a sequence number, so a recreated file never takes the old one's ID.
// wasm and Plan 9 keep the plain stat identity.
func pinFile(string) *os.File { return nil }

// dupPin has no pin to duplicate here.
func dupPin(*os.File) *os.File { return nil }
