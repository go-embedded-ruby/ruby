package vm_test

import "testing"

// The Ruby module, new in Ruby 4.0: "the Ruby module that contains portable
// information among implementations. The constants defined here are aliased in
// the toplevel with +RUBY_+ prefix" (ruby/ruby version.c, Init_version at tag
// v4.0.9 -- note the tag naming changed at 4.0.0, from v3_4_1 to v4.0.9).
//
// ⛔ THESE ARE IDENTITY TESTS, NOT EQUALITY TESTS, and that is the only reason
// they are interesting. version.c installs each value under both names in one
// step:
//
//	if (toplevel) {
//	    rb_define_global_const(name, value);   // RUBY_VERSION
//	    name += rb_strlen_lit("RUBY_");
//	}
//	rb_define_const(mod, name, value);         // Ruby::VERSION
//
// so core/builtin_constants/builtin_constants_spec.rb asserts
// `Ruby::VERSION.should.equal?(RUBY_VERSION)` for all nine. An implementation
// that built two frozen strings with the same bytes would pass `==` and fail
// every one of those nine examples -- and the failure would read as "the
// constant has the wrong value", which it does not.
//
// The nine are spelled out rather than looped over a list of names: a loop that
// silently iterated eight would print `true` eight times and look exactly like
// a pass. Nine lines, nine `true`s in the expected output, so the COUNT is part
// of what is asserted.
func TestTheRubyNamespaceAliasesTheSameObjects(t *testing.T) {
	checkCases(t, []runCase{
		{`p Ruby.instance_of?(Module)`, "true\n"},
		{
			`p Ruby::VERSION.equal?(RUBY_VERSION)
p Ruby::RELEASE_DATE.equal?(RUBY_RELEASE_DATE)
p Ruby::PLATFORM.equal?(RUBY_PLATFORM)
p Ruby::PATCHLEVEL.equal?(RUBY_PATCHLEVEL)
p Ruby::REVISION.equal?(RUBY_REVISION)
p Ruby::COPYRIGHT.equal?(RUBY_COPYRIGHT)
p Ruby::ENGINE.equal?(RUBY_ENGINE)
p Ruby::ENGINE_VERSION.equal?(RUBY_ENGINE_VERSION)
p Ruby::DESCRIPTION.equal?(RUBY_DESCRIPTION)`,
			"true\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\n",
		},
		// version.c passes the one `version` VALUE to both VERSION and
		// ENGINE_VERSION, so in MRI these are the same object too. Nothing in
		// ruby/spec requires it either way; matching it is free, and a reader who
		// checks against the reference should find no difference to explain.
		{`p RUBY_VERSION.equal?(RUBY_ENGINE_VERSION)`, "true\n"},
		// The values still have to be the values. An alias to the wrong object
		// would satisfy every identity above.
		{`p Ruby::VERSION == RUBY_VERSION && Ruby::VERSION.frozen?`, "true\n"},
	})
}

// ⛔ The control for the tests above: `equal?` must actually be identity in this
// VM. If it answered on value, every assertion above would pass whatever the
// implementation did, and the nine examples in ruby/spec would still fail.
//
// Two separately-built strings with the same bytes are `==` and not `equal?`;
// one object is both. Without this, the test file above is nine assertions
// resting on an untested assumption about the matcher.
func TestEqualIsIdentityNotEquality(t *testing.T) {
	checkCases(t, []runCase{
		{`a = "x" + "y"; b = "x" + "y"; p a == b, a.equal?(b), a.equal?(a)`, "true\nfalse\ntrue\n"},
	})
}
