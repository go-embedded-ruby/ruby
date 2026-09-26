// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// TestMatchDataInheritsSubjectEncoding pins re.c's rule at tag v3_4_0 that
// every substring a match yields is cut out of the stored subject with
// rb_str_subseq — rb_reg_nth_match, rb_reg_match_pre and rb_reg_match_post all
// do — so $&, $`, $', $+, $1..N and the MatchData accessors carry the
// SUBJECT's encoding. rbgo built them with object.NewString and stamped
// UTF-8 on every one, which language/predefined_spec.rb caught seven times.
func TestMatchDataInheritsSubjectEncoding(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"dollar_amp", `
s = "abc".dup.force_encoding(Encoding::EUC_JP)
s =~ /(b)/
p $&.encoding`, "#<Encoding:EUC-JP>\n"},
		{"dollar_pre", `
s = "abc".dup.force_encoding(Encoding::EUC_JP)
s =~ /(b)/
p $` + "`" + `.encoding`, "#<Encoding:EUC-JP>\n"},
		{"dollar_post", `
s = "abc".dup.force_encoding(Encoding::EUC_JP)
s =~ /(b)/
p $'.encoding`, "#<Encoding:EUC-JP>\n"},
		{"dollar_plus", `
s = "abc".dup.force_encoding(Encoding::EUC_JP)
s =~ /(b)/
p $+.encoding`, "#<Encoding:EUC-JP>\n"},
		{"dollar_one", `
s = "abc".dup.force_encoding(Encoding::EUC_JP)
s =~ /(b)/
p $1.encoding`, "#<Encoding:EUC-JP>\n"},
		// An EMPTY pre_match/post_match is tagged too — the spec asserts this
		// separately because a zero-length result is the easy one to get wrong.
		{"empty_pre", `
s = "abc".dup.force_encoding(Encoding::ISO_8859_1)
s =~ /a/
p [$` + "`" + `.empty?, $` + "`" + `.encoding]`, "[true, #<Encoding:ISO-8859-1>]\n"},
		{"empty_post", `
s = "abc".dup.force_encoding(Encoding::ISO_8859_1)
s =~ /c/
p [$'.empty?, $'.encoding]`, "[true, #<Encoding:ISO-8859-1>]\n"},
		{"matchdata_accessors", `
u = "abc".dup.force_encoding(Encoding::EUC_JP)
m = /(b)/.match(u)
p [m[0].encoding, m[1].encoding, m.pre_match.encoding, m.post_match.encoding,
   m.to_s.encoding, m.string.encoding]`,
			"[#<Encoding:EUC-JP>, #<Encoding:EUC-JP>, #<Encoding:EUC-JP>, #<Encoding:EUC-JP>, #<Encoding:EUC-JP>, #<Encoding:EUC-JP>]\n"},
		{"match_with_pos", `
u = "abcabc".dup.force_encoding(Encoding::EUC_JP)
m = /(b)/.match(u, 2)
p [m[0].encoding, m.pre_match.encoding, m.post_match.encoding]`,
			"[#<Encoding:EUC-JP>, #<Encoding:EUC-JP>, #<Encoding:EUC-JP>]\n"},
		{"string_index_regexp", `
u = "abc".dup.force_encoding(Encoding::EUC_JP)
u.index(/b/)
p $&.encoding`, "#<Encoding:EUC-JP>\n"},
		{"string_rindex_regexp", `
u = "abcb".dup.force_encoding(Encoding::EUC_JP)
u.rindex(/b/)
p $&.encoding`, "#<Encoding:EUC-JP>\n"},
		{"string_start_with_regexp", `
u = "abc".dup.force_encoding(Encoding::EUC_JP)
u.start_with?(/a/)
p $&.encoding`, "#<Encoding:EUC-JP>\n"},
		{"string_rpartition_regexp", `
u = "abcb".dup.force_encoding(Encoding::EUC_JP)
u.rpartition(/b/)
p $&.encoding`, "#<Encoding:EUC-JP>\n"},
		{"regexp_triple_equal", `
u = "abc".dup.force_encoding(Encoding::EUC_JP)
/b/ === u
p $&.encoding`, "#<Encoding:EUC-JP>\n"},
		{"string_scan", `
u = "abcb".dup.force_encoding(Encoding::EUC_JP)
u.scan(/b/)
p $&.encoding`, "#<Encoding:EUC-JP>\n"},
		{"string_gsub_block", `
u = "abcb".dup.force_encoding(Encoding::EUC_JP)
u.gsub(/b/) { $&.encoding }
p $&.encoding`, "#<Encoding:EUC-JP>\n"},
		{"string_sub_hash", `
u = "abcb".dup.force_encoding(Encoding::EUC_JP)
u.gsub(/b/, "b" => "B")
p $&.encoding`, "#<Encoding:EUC-JP>\n"},
		{"string_slice_bang", `
u = "abc".dup.force_encoding(Encoding::EUC_JP)
u.slice!(/b/)
p $&.encoding`, "#<Encoding:EUC-JP>\n"},
		{"string_element_set", `
u = "abc".dup.force_encoding(Encoding::EUC_JP)
u[/b/] = "x"
p $&.encoding`, "#<Encoding:EUC-JP>\n"},
		{"byteindex_regexp", `
u = "abc".dup.force_encoding(Encoding::EUC_JP)
u.byteindex(/b/)
p $&.encoding`, "#<Encoding:EUC-JP>\n"},
		{"byterindex_regexp", `
u = "abcb".dup.force_encoding(Encoding::EUC_JP)
u.byterindex(/b/)
p $&.encoding`, "#<Encoding:EUC-JP>\n"},
		// A Symbol subject is not a String, so the match carries the default
		// encoding (matchEnc's fallback arm).
		{"symbol_subject", `
m = /b/.match(:abc)
p [m[0], m[0].encoding]`, "[\"b\", #<Encoding:UTF-8>]\n"},
		// The default stays UTF-8 for an untagged subject.
		{"plain_subject", `
"abc" =~ /b/
p $&.encoding`, "#<Encoding:UTF-8>\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := eval(t, tc.src); got != tc.want {
				t.Errorf("src=%q got=%q want=%q", tc.src, got, tc.want)
			}
		})
	}
}
