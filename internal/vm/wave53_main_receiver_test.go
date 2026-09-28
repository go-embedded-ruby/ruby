// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// TestTopLevelSelfRendersAsMain covers MRI's one exception inside the
// singleton-receiver arm of name_err_mesg_to_str (error.c ruby_4_0:2679-2689):
// an object carrying a singleton class renders as its identity string, EXCEPT
// the top-level self, which renders as "main".
//
// Every want below is the byte-for-byte output of the local MRI 4.0.5 on that
// exact source.
func TestTopLevelSelfRendersAsMain(t *testing.T) {
	t.Run("top-level self", func(t *testing.T) {
		src := "begin; self.nope_xyz; rescue NoMethodError => e; puts e.message; end\n"
		const want = "undefined method 'nope_xyz' for main\n"
		if got := eval(t, src); got != want {
			t.Errorf("got %q want %q", got, want)
		}
	})

	// The rest of the arm must not move. An ordinary object that carries a
	// singleton still renders as its identity, and — this is the part worth
	// pinning — MRI does NOT dispatch #inspect or #to_s to build it. Overriding
	// both changes nothing on ruby 4.0.5, which is why this message is
	// constructed here rather than by sending to the receiver: an error path
	// that runs user code can recurse or raise again.
	t.Run("a singleton-carrying object still renders as its identity", func(t *testing.T) {
		src := `o = Object.new
def o.inspect; "CUSTOM-INSPECT"; end
def o.to_s; "CUSTOM-TO-S"; end
begin; o.nope; rescue NoMethodError => e; puts e.message.sub(/0x\h+/, "0xX"); end
`
		const want = "undefined method 'nope' for #<Object:0xX>\n"
		if got := eval(t, src); got != want {
			t.Errorf("got %q want %q — the override must NOT reach the message", got, want)
		}
	})

	// And an object with no singleton takes the other arm entirely.
	t.Run("no singleton", func(t *testing.T) {
		src := "begin; Object.new.nope; rescue NoMethodError => e; puts e.message; end\n"
		const want = "undefined method 'nope' for an instance of Object\n"
		if got := eval(t, src); got != want {
			t.Errorf("got %q want %q", got, want)
		}
	})
}

// TestRemoveMethodNameErrorCarriesItsReceiver: Module#remove_method on a name
// that is not there raises through rb_name_err_raise (vm_method.c
// rb_mod_remove_method), whose first argument IS the exception's receiver. rbgo
// built the same message through the bare raise() form and recorded none.
func TestRemoveMethodNameErrorCarriesItsReceiver(t *testing.T) {
	src := "class K; end\nbegin; K.send(:remove_method, :nope); rescue NameError => e; p [e.message, e.name, e.receiver]; end\n"
	const want = "[\"method 'nope' not defined in K\", :nope, K]\n"
	if got := eval(t, src); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
