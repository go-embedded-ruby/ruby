// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestApplyRegexpEncodingFlags locks the literal-regexp encoding-modifier mapping
// (go-ruby-parser >= v0.1.8 records n/u/e/s in Flags): n = NOENCODING/ASCII-8BIT,
// u = UTF-8, e = EUC-JP, s = Windows-31J (u/e/s FIXEDENCODING); i/m/x/o carry no
// encoding; the last encoding letter wins. Verified byte-for-byte against MRI
// 4.0.5 (/x/n.options == 32, /x/u.encoding == UTF-8, /a/un.options == 32, …).
func TestApplyRegexpEncodingFlags(t *testing.T) {
	cases := []struct {
		flags        string
		fixed, noEnc bool
		srcEnc       string
	}{
		{"", false, false, ""},
		{"imxo", false, false, ""}, // non-encoding letters are ignored
		{"n", false, true, ""},
		{"u", true, false, "UTF-8"},
		{"e", true, false, "EUC-JP"},
		{"s", true, false, "Windows-31J"},
		{"mn", false, true, ""},      // m ignored, n applies
		{"un", false, true, ""},      // last wins: n after u
		{"nu", true, false, "UTF-8"}, // last wins: u after n
	}
	for _, c := range cases {
		r := &Regexp{}
		applyRegexpEncodingFlags(r, c.flags)
		if r.fixedEnc != c.fixed || r.noEnc != c.noEnc || r.srcEnc != c.srcEnc {
			t.Errorf("flags %q: got fixed=%v noEnc=%v srcEnc=%q, want fixed=%v noEnc=%v srcEnc=%q",
				c.flags, r.fixedEnc, r.noEnc, r.srcEnc, c.fixed, c.noEnc, c.srcEnc)
		}
	}
}
