// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// validIvarName reports whether name is a syntactically valid instance-variable
// name — an '@' followed by an identifier whose first character is a letter,
// '_' or any non-ASCII rune, and whose remaining characters may also be digits.
// This mirrors MRI's rb_is_instance_id check: '@', '@0', '@+' and '@@x' are all
// rejected, while '@x', '@_9' and unicode names like '@💙' are accepted.
func validIvarName(name string) bool {
	if len(name) < 2 || name[0] != '@' {
		return false
	}
	rest := name[1:]
	r, sz := utf8.DecodeRuneInString(rest)
	if r == utf8.RuneError && sz <= 1 {
		return false
	}
	// First character: a letter, '_' or any non-ASCII rune — but never a digit.
	if !(r >= utf8.RuneSelf || isIvarLetter(r)) {
		return false
	}
	for _, c := range rest[sz:] {
		if c < utf8.RuneSelf && !(isIvarLetter(c) || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// isIvarLetter reports whether an ASCII rune may start an identifier component.
func isIvarLetter(r rune) bool {
	return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// ivarNameArg coerces an instance-variable-name argument to its string form the
// way MRI's rb_check_id does: a Symbol or String is taken directly, any other
// object is converted through #to_str (which must yield a String), and anything
// else raises TypeError. The resulting name is then validated and a NameError is
// raised when it is not a legal instance-variable name. The returned string
// includes the leading '@'.
func (vm *VM) ivarNameArg(arg object.Value) string {
	var name string
	switch a := arg.(type) {
	case object.Symbol:
		name = string(a)
	case *object.String:
		name = a.Str()
	default:
		if vm.respondsToDynamic(arg, "to_str") {
			r := vm.send(arg, "to_str", nil, nil)
			s, ok := r.(*object.String)
			if !ok {
				raise("TypeError", "can't convert %s to String (%s#to_str gives %s)",
					classNameOf(arg), classNameOf(arg), classNameOf(r))
			}
			name = s.Str()
		} else {
			raise("TypeError", "%s is not a symbol nor a string", vm.inspectStr(arg))
		}
	}
	if !validIvarName(name) {
		raise("NameError", "`%s' is not allowed as an instance variable name", name)
	}
	return name
}
