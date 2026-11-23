// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestAFailedCallRaisesMRIsClassOnEveryHost (go-embedded-ruby/ruby#980): the
// expected output is MRI 4.0.7's on darwin, and MRI raises the same classes on
// Windows because it maps each Win32 code through errmap first. These are the
// rows where the raw Win32 code named another class, or none:
//
//   - a path through a missing directory fails with ERROR_PATH_NOT_FOUND (3),
//     which is Go's syscall.ENOTDIR on Windows, so it read as Errno::ENOTDIR;
//   - rmdir of a non-empty directory fails with ERROR_DIR_NOT_EMPTY (145),
//     which errors.Is never matched to ENOTEMPTY, so it fell to Errno::ENOENT.
//
// On the POSIX lanes these pass with or without the mapping; the Windows lane
// is the one that says whether it holds.
func TestAFailedCallRaisesMRIsClassOnEveryHost(t *testing.T) {
	checkCases(t, []runCase{
		{`require "tmpdir"
Dir.mktmpdir do |d|
  begin
    Dir.chdir(File.join(d, "missing", "sub"))
  rescue SystemCallError => e
    p e.class
  end
  full = File.join(d, "full")
  Dir.mkdir(full)
  File.write(File.join(full, "f"), "x")
  begin
    Dir.rmdir(full)
  rescue SystemCallError => e
    p e.class
  end
  begin
    File.truncate(File.join(d, "missing", "sub"), 0)
  rescue SystemCallError => e
    p e.class
  end
end`, "Errno::ENOENT\nErrno::ENOTEMPTY\nErrno::ENOENT\n"},
	})
}
