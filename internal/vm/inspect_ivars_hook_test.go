// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

// Ruby 4.0 lets an object say which instance variables its #inspect may show.
// object.c's rb_obj_inspect at v4.0.9:
//
//	VALUE ivars = rb_check_funcall(obj, id_instance_variables_to_inspect, 0, 0);
//	if (UNDEF_P(ivars) || NIL_P(ivars)) { n = rb_ivar_count(obj); ivars = Qnil; }
//	else if (RB_TYPE_P(ivars, T_ARRAY)) { n = RARRAY_LEN(ivars); }
//	else rb_raise(rb_eTypeError, "Expected #instance_variables_to_inspect to "
//	              "return an Array or nil, but it returned %"PRIsVALUE, ...);
//
// Every expectation came from MRI 4.0.7 on this host through rubydiff. The two
// ruby/spec examples behind `ruby_version_is "4.0"` in core/kernel/inspect_spec.rb
// that rbgo failed are the first two rows; the other two in that block already
// passed, because showing everything was what rbgo did anyway.
func TestInspectHonoursInstanceVariablesToInspect(t *testing.T) {
	const obj = `
o = Object.new
o.instance_eval { @host = "localhost"; @user = "root"; @password = "hunter2" }
`
	norm := "\n.sub(/0x[0-9a-f]+/, '0xXX')"
	for _, tc := range []struct{ name, src, want string }{
		{"only the named variables are shown",
			obj + `o.singleton_class.class_eval("private def instance_variables_to_inspect = %i[@host @user @does_not_exist]")
p o.inspect` + norm,
			`"#<Object:0xXX @host=\"localhost\", @user=\"root\">"`},

		{"an invalid return is a TypeError",
			obj + `o.singleton_class.class_eval("private def instance_variables_to_inspect = {}")
begin; o.inspect; rescue TypeError => e; p e.message; end`,
			`"Expected #instance_variables_to_inspect to return an Array or nil, but it returned Hash"`},

		{"an empty array shows nothing",
			obj + `o.singleton_class.class_eval("private def instance_variables_to_inspect = []")
p o.inspect` + norm,
			`"#<Object:0xXX>"`},

		{"nil shows everything",
			obj + `o.singleton_class.class_eval("private def instance_variables_to_inspect = nil")
p o.inspect` + norm,
			`"#<Object:0xXX @host=\"localhost\", @user=\"root\", @password=\"hunter2\">"`},

		// ⛔ STRINGS DO NOT MATCH. inspect_i compares ID2SYM(id) against the
		// array's elements, so a String that spells the same name is not the
		// name: n is 2, the filtering branch is taken, and nothing matches.
		{"a String is not the Symbol it spells",
			obj + `o.singleton_class.class_eval('private def instance_variables_to_inspect = ["@host", "@user"]')
p o.inspect` + norm,
			`"#<Object:0xXX>"`},

		// ⛔ THE ORDER IS THE OBJECT'S, not the list's: inspect_i walks the
		// object's variables and tests each for membership, so reversing the
		// list changes nothing.
		{"the list does not reorder the output",
			obj + `o.singleton_class.class_eval("private def instance_variables_to_inspect = %i[@user @host]")
p o.inspect` + norm,
			`"#<Object:0xXX @host=\"localhost\", @user=\"root\">"`},

		{"a repeated name is still shown once",
			obj + `o.singleton_class.class_eval("private def instance_variables_to_inspect = %i[@host @host]")
p o.inspect` + norm,
			`"#<Object:0xXX @host=\"localhost\">"`},

		{"a name without the @ matches nothing",
			obj + `o.singleton_class.class_eval("private def instance_variables_to_inspect = %i[host]")
p o.inspect` + norm,
			`"#<Object:0xXX>"`},

		// rb_check_funcall ignores visibility, so a public hook works too.
		{"a public hook is honoured",
			obj + `o.singleton_class.class_eval("def instance_variables_to_inspect = %i[@host]")
p o.inspect` + norm,
			`"#<Object:0xXX @host=\"localhost\">"`},

		// ⛔ THE FAST PATH MUST NOT SWALLOW AN OVERRIDE. inspectIvarFilter skips
		// the dispatch when the resolved record IS Kernel's default, so every
		// way of overriding it has to produce a different record: on the class,
		// inherited from it, re-overridden below it, and mixed in from a module.
		{"an override on the class",
			`class C0; def initialize = (@a=1; @b=2); private def instance_variables_to_inspect = %i[@a]; end
p C0.new.inspect` + norm,
			`"#<C0:0xXX @a=1>"`},
		{"an override inherited by a subclass",
			`class C1; def initialize = (@a=1; @b=2); private def instance_variables_to_inspect = %i[@a]; end
class C2 < C1; end
p C2.new.inspect` + norm,
			`"#<C2:0xXX @a=1>"`},
		{"an override replaced below",
			`class C3; def initialize = (@a=1; @b=2); private def instance_variables_to_inspect = %i[@a]; end
class C4 < C3; private def instance_variables_to_inspect = %i[@b]; end
p C4.new.inspect` + norm,
			`"#<C4:0xXX @b=2>"`},
		{"an override mixed in from a module",
			`module M0; private def instance_variables_to_inspect = %i[@b]; end
class C5; include M0; def initialize = (@a=1; @b=2); end
p C5.new.inspect` + norm,
			`"#<C5:0xXX @b=2>"`},

		// ⛔ A BasicObject does not include Kernel, so the hook is not defined on
		// it at all — MRI's rb_check_funcall returns Qundef, which means the
		// same as nil. ruby/spec reaches this through a bound Kernel#inspect,
		// and it is the only route to inspectIvarFilter's "no method" branch:
		// the coverage gate was what showed the Go tests had never taken it.
		{"a BasicObject has no hook and shows everything",
			`o = BasicObject.new
o.instance_eval { @host = "localhost"; @user = "root" }
p Kernel.instance_method(:inspect).bind(o).call.sub(/0x[0-9a-f]+/, '0xXX')`,
			`"#<BasicObject:0xXX @host=\"localhost\", @user=\"root\">"`},

		// Controls: the ordinary paths, which must be untouched.
		{"an object with no override shows everything",
			`class C6; def initialize = (@a=1; @b=2); end
p C6.new.inspect` + norm,
			`"#<C6:0xXX @a=1, @b=2>"`},
		{"#to_s never shows variables",
			`class C7; def initialize = (@a=1); private def instance_variables_to_inspect = %i[@a]; end
p C7.new.to_s` + norm,
			`"#<C7:0xXX>"`},
		{"the filter survives nesting",
			`class C8; def initialize = (@a=1; @b=2); private def instance_variables_to_inspect = %i[@a]; end
p [C8.new].inspect` + norm,
			`"[#<C8:0xXX @a=1>]"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %s\nwant %s", tc.src, got, tc.want)
			}
		})
	}
}

// ⛔ Kernel#instance_variables_to_inspect is a PRIVATE method returning nil,
// defined by object.c as rb_obj_instance_variables_to_inspect. Its name was
// already in kernelPrivateNames, so the rehome pass was ready for it — but
// nothing defined it, and a name listed as private that does not exist reports
// the same `false` as one that was never meant to be there.
func TestKernelDefinesTheInspectHookAsPrivate(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"it exists and is private",
			`p Object.new.respond_to?(:instance_variables_to_inspect, true)`, "true"},
		{"it is not public",
			`p Object.new.respond_to?(:instance_variables_to_inspect)`, "false"},
		{"it answers nil",
			`p Object.new.send(:instance_variables_to_inspect)`, "nil"},
		{"Kernel owns it",
			`p Object.new.method(:instance_variables_to_inspect).owner`, "Kernel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %s\nwant %s", tc.src, got, tc.want)
			}
		})
	}
}
