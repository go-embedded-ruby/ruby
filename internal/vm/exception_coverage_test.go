package vm_test

import "testing"

// TestExceptionMessageCoverage exercises the receiver-formatting and
// receiver-keyword branches of the exception subsystem — undefinedMethodReceiver,
// classDisplayName, anonClassOrModuleRepr and stampReceiverKwarg — across nil, a
// class, a named/anonymous instance, an anonymous module, and NameError's
// `receiver:` keyword in its present / absent / message-only forms. Checked
// against MRI Ruby 4.0.6 (identity hex addresses are matched by prefix).
func TestExceptionMessageCoverage(t *testing.T) {
	ok := []struct{ src, want string }{
		// undefinedMethodReceiver: nil, true/false, a class, a named instance, and
		// an object carrying a per-object singleton class (rendered by identity).
		{`begin; nil.zzz_undef; rescue NoMethodError => e; puts e.message; end`,
			"undefined method 'zzz_undef' for nil\n"},
		{`begin; true.zzz_undef; rescue NoMethodError => e; puts e.message; end`,
			"undefined method 'zzz_undef' for true\n"},
		{`begin; false.zzz_undef; rescue NoMethodError => e; puts e.message; end`,
			"undefined method 'zzz_undef' for false\n"},
		{`begin; String.zzz_undef; rescue NoMethodError => e; puts e.message; end`,
			"undefined method 'zzz_undef' for class String\n"},
		{`begin; Object.new.zzz_undef; rescue NoMethodError => e; puts e.message; end`,
			"undefined method 'zzz_undef' for an instance of Object\n"},
		{`o = Object.new; def o.sing; end
begin; o.zzz_undef; rescue NoMethodError => e
  puts e.message.start_with?("undefined method 'zzz_undef' for #<Object:0x")
end`, "true\n"},
		// classDisplayName anonymous branch + anonClassOrModuleRepr (Class kind):
		// an instance of an anonymous class renders as "#<Class:0x…>".
		{`begin; Class.new.new.zzz_undef; rescue NoMethodError => e
  puts e.message.start_with?("undefined method 'zzz_undef' for an instance of #<Class:0x")
end`, "true\n"},
		// anonClassOrModuleRepr Module kind: a method missing on an anonymous module.
		{`begin; Module.new.zzz_undef; rescue NoMethodError => e
  puts e.message.include?("#<Module:0x")
end`, "true\n"},
		// stampReceiverKwarg: the receiver: keyword is peeled off and stored.
		{`p NameError.new("x", receiver: 42).receiver`, "42\n"},
		// stampReceiverKwarg: a String last arg is left untouched (no keyword hash).
		{`p NameError.new("x").message`, "\"x\"\n"},
		// stampReceiverKwarg: empty args returns unchanged (default message).
		{`p NameError.new.is_a?(NameError)`, "true\n"},
		// stampReceiverKwarg: a Symbol name arg is not a keyword hash (left as-is).
		{`p NameError.new("x", :some_name).name`, ":some_name\n"},
		// stampReceiverKwarg: a trailing Hash without a :receiver key is left as-is.
		{`p NameError.new("x", {"a" => 1}).message`, "\"x\"\n"},
		// NoMethodError.new(message, name, args): the positional message/name/args
		// branches of its initialize.
		{`p NoMethodError.new("boom", :meth, [1, 2]).name`, ":meth\n"},
		{`p NoMethodError.new("boom", :meth, [1, 2]).args`, "[1, 2]\n"},
		{`p NoMethodError.new("boom", :meth, [1, 2]).message`, "\"boom\"\n"},
	}
	for _, c := range ok {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q\n got=%q\nwant=%q", c.src, got, c.want)
		}
	}
}
