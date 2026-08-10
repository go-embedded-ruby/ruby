package vm

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// FNM_* are the flag bits accepted by File.fnmatch? and Dir.glob. Their numeric
// values match MRI's File::FNM_* constants exactly (verified against ruby 4.0.5),
// so patterns and flags round-trip between Ruby code and this matcher.
const (
	fnmNoEscape = 0x01 // FNM_NOESCAPE: treat '\' as a literal character
	fnmPathname = 0x02 // FNM_PATHNAME: '/' is matched only by an explicit '/'
	fnmDotMatch = 0x04 // FNM_DOTMATCH: a leading '.' is matched by wildcards
	fnmCaseFold = 0x08 // FNM_CASEFOLD: case-insensitive matching
	fnmExtGlob  = 0x10 // FNM_EXTGLOB: enable {a,b} brace alternation
	fnmSysCase  = 0x00 // FNM_SYSCASE: 0 on case-sensitive (POSIX) platforms
)

// fnmatch reports whether name matches pattern under the given FNM_* flags,
// implementing MRI's File.fnmatch semantics on top of a self-contained matcher
// (Go's path/filepath.Match supports neither '**', '{}', nor these flags). It is
// the shared core behind File.fnmatch? and the per-segment tests Dir.glob runs.
func fnmatch(pattern, name string, flags int) bool {
	if flags&fnmExtGlob != 0 {
		for _, alt := range braceExpand(pattern, flags&fnmNoEscape == 0) {
			if fnmatchNoBrace(alt, name, flags) {
				return true
			}
		}
		return false
	}
	return fnmatchNoBrace(pattern, name, flags)
}

// fnmatchNoBrace matches a single (already brace-expanded) pattern. Under
// FNM_PATHNAME it splits both sides on '/' and matches segment lists so that a
// '**' segment can span directories and the leading-'.' rule applies per
// segment; otherwise the whole string is one segment in which '*', '?' and
// '[...]' freely match '/'.
func fnmatchNoBrace(pattern, name string, flags int) bool {
	escape := flags&fnmNoEscape == 0
	nocase := flags&fnmCaseFold != 0
	period := flags&fnmDotMatch == 0
	if flags&fnmPathname != 0 {
		return matchSegList(strings.Split(pattern, "/"), strings.Split(name, "/"), escape, nocase, period)
	}
	return matchSegment(pattern, name, escape, nocase, period)
}

// matchSegList matches a list of pattern segments against a list of path
// segments (FNM_PATHNAME). A '**' segment that is not the final pattern segment
// matches zero or more path segments (recursing), but will not descend past a
// hidden segment while the leading-'.' rule is in force; a trailing '**' behaves
// like a single '*'.
func matchSegList(psegs, ssegs []string, escape, nocase, period bool) bool {
	for len(psegs) > 0 {
		ps := psegs[0]
		if ps == "**" && len(psegs) > 1 {
			for k := 0; k <= len(ssegs); k++ {
				if matchSegList(psegs[1:], ssegs[k:], escape, nocase, period) {
					return true
				}
				if k < len(ssegs) && period && strings.HasPrefix(ssegs[k], ".") {
					break // '**' may not traverse a hidden directory without DOTMATCH
				}
			}
			return false
		}
		seg := ps
		if seg == "**" { // a trailing '**' is just '*'
			seg = "*"
		}
		if len(ssegs) == 0 {
			return false
		}
		if !matchSegment(seg, ssegs[0], escape, nocase, period) {
			return false
		}
		psegs, ssegs = psegs[1:], ssegs[1:]
	}
	return len(ssegs) == 0
}

// matchSegment matches a slash-free pattern segment against a string, anchored
// at both ends. When leadingDot is set and the string begins with '.', the match
// fails unless the pattern's first token is a literal '.', implementing MRI's
// rule that '*', '?' and '[...]' do not match a leading period.
func matchSegment(p, s string, escape, nocase, leadingDot bool) bool {
	if leadingDot && strings.HasPrefix(s, ".") && !patternStartsWithDot(p, escape) {
		return false
	}
	return matchSeg(p, s, escape, nocase)
}

// patternStartsWithDot reports whether the pattern begins with a literal '.'
// (either a bare '.' or, when escaping is on, a '\.').
func patternStartsWithDot(p string, escape bool) bool {
	if strings.HasPrefix(p, ".") {
		return true
	}
	return escape && strings.HasPrefix(p, "\\.")
}

// matchSeg is the anchored wildcard matcher for one segment: '*' matches a run of
// characters, '?' one character, '[...]' a bracket expression, '\' escapes the
// next character (unless escape is off), and everything else is literal. It
// backtracks on '*'.
func matchSeg(p, s string, escape, nocase bool) bool {
	for len(p) > 0 {
		switch p[0] {
		case '*':
			for len(p) > 0 && p[0] == '*' {
				p = p[1:]
			}
			if len(p) == 0 {
				return true // trailing '*' absorbs the rest of the segment
			}
			for i := 0; i <= len(s); {
				if matchSeg(p, s[i:], escape, nocase) {
					return true
				}
				if i == len(s) {
					break
				}
				_, sz := utf8.DecodeRuneInString(s[i:])
				i += sz
			}
			return false
		case '?':
			if len(s) == 0 {
				return false
			}
			_, sz := utf8.DecodeRuneInString(s)
			p, s = p[1:], s[sz:]
		case '[':
			if len(s) == 0 {
				return false
			}
			sr, sz := utf8.DecodeRuneInString(s)
			rest, ok, term := matchBracket(p, sr, escape, nocase)
			if !term || !ok {
				return false // unterminated bracket or non-match: this branch fails
			}
			p, s = rest, s[sz:]
		default:
			pr, psz := decodeLiteral(p, escape)
			if len(s) == 0 {
				return false
			}
			sr, ssz := utf8.DecodeRuneInString(s)
			if !runeEqual(pr, sr, nocase) {
				return false
			}
			p, s = p[psz:], s[ssz:]
		}
	}
	return len(s) == 0
}

// decodeLiteral decodes the next literal character of a pattern, honouring a
// leading '\' escape when escaping is enabled, and returns the rune plus the
// number of pattern bytes it consumed.
func decodeLiteral(p string, escape bool) (rune, int) {
	if escape && p[0] == '\\' && len(p) > 1 {
		r, sz := utf8.DecodeRuneInString(p[1:])
		return r, 1 + sz
	}
	r, sz := utf8.DecodeRuneInString(p)
	return r, sz
}

// matchBracket evaluates a bracket expression beginning at p[0]=='[' against the
// rune c. It returns the pattern remainder after the closing ']', whether c is a
// member (respecting a leading '!' or '^' negation), and whether the bracket was
// terminated at all — an unterminated '[' can never match, so callers fail on
// term==false. A ']' immediately after '[' closes an empty class (MRI semantics),
// and ranges honour case folding and match their endpoints even when reversed.
func matchBracket(p string, c rune, escape, nocase bool) (rest string, ok bool, term bool) {
	i := 1 // skip '['
	neg := false
	if i < len(p) && (p[i] == '!' || p[i] == '^') {
		neg = true
		i++
	}
	matched := false
	for {
		if i >= len(p) {
			return "", false, false // unterminated
		}
		if p[i] == ']' {
			i++
			break
		}
		lo, adv := decodeLiteral(p[i:], escape)
		i += adv
		if i+1 < len(p) && p[i] == '-' && p[i+1] != ']' {
			i++ // consume '-'
			hi, adv2 := decodeLiteral(p[i:], escape)
			i += adv2
			if rangeMatch(c, lo, hi, nocase) {
				matched = true
			}
		} else if runeEqual(c, lo, nocase) {
			matched = true
		}
	}
	return p[i:], matched != neg, true
}

// rangeMatch reports whether c lies in the inclusive range lo..hi (case-folded
// when nocase). Either endpoint always matches, so a reversed range such as
// [c-a] still matches 'a' and 'c' exactly, as MRI does.
func rangeMatch(c, lo, hi rune, nocase bool) bool {
	if nocase {
		c, lo, hi = foldRune(c), foldRune(lo), foldRune(hi)
	}
	return c == lo || c == hi || (lo <= c && c <= hi)
}

// runeEqual reports rune equality, case-folded when nocase is set.
func runeEqual(a, b rune, nocase bool) bool {
	if nocase {
		return foldRune(a) == foldRune(b)
	}
	return a == b
}

// foldRune lower-cases an ASCII letter for case-insensitive matching, leaving
// other runes unchanged.
func foldRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

// braceExpand expands EXTGLOB '{a,b,c}' alternations (including nested and empty
// alternatives) into the list of concrete patterns they denote, leaving escaped
// braces and unmatched braces literal. A pattern with no expandable brace yields
// itself unchanged.
func braceExpand(pattern string, escape bool) []string {
	open, close := findBrace(pattern, escape)
	if open < 0 {
		return []string{pattern}
	}
	prefix := pattern[:open]
	inner := pattern[open+1 : close]
	suffix := pattern[close+1:]
	var out []string
	for _, alt := range splitAlternatives(inner, escape) {
		for _, tail := range braceExpand(alt+suffix, escape) {
			out = append(out, prefix+tail)
		}
	}
	return out
}

// findBrace locates the first top-level '{' whose matching '}' encloses at least
// one ',' at brace depth one, returning its open and close indices; it returns
// (-1,-1) when no such expandable brace exists. Escaped braces are skipped, and a
// comma-free brace is left literal (scanning continues past it).
func findBrace(s string, escape bool) (open, close int) {
	for i := 0; i < len(s); i++ {
		if escape && s[i] == '\\' {
			i++ // skip the escaped character
			continue
		}
		if s[i] != '{' {
			continue
		}
		depth, firstComma := 0, -1
		for j := i; j < len(s); j++ {
			if escape && s[j] == '\\' {
				j++
				continue
			}
			if s[j] == '{' {
				depth++
			} else if s[j] == ',' && depth == 1 && firstComma < 0 {
				firstComma = j
			} else if s[j] == '}' {
				depth--
				if depth == 0 {
					if firstComma >= 0 {
						return i, j
					}
					break // comma-free brace: leave literal, resume outer scan
				}
			}
		}
	}
	return -1, -1
}

// splitAlternatives splits a brace body on top-level commas (respecting nesting
// and escapes), returning each alternative — an empty alternative yields "".
func splitAlternatives(s string, escape bool) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		if escape && s[i] == '\\' {
			i++
			continue
		}
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}

// sortedUnique returns the input with duplicates removed and, when sortResults is
// set, sorted ascending — the shape Dir.glob returns (sorted by default, dupes
// coalesced across brace/`**` overlap).
func sortedUnique(in []string, sortResults bool) []string {
	seen := make(map[string]struct{}, len(in))
	out := in[:0:0]
	for _, s := range in {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if sortResults {
		sort.Strings(out)
	}
	return out
}
