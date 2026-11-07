// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import "testing"

const causePre = `def show(label)
  yield
rescue Exception => e
  puts "#{label}: #{e.class} cause=#{e.cause.inspect}"
end
`

// An exception raised while another is being handled carries that other one as
// its #cause. MRI takes it from get_ec_errinfo(ec) in exc_setup_message when no
// cause was given, and exc_setup_cause declines a self-cause (eval.c at v4.0.9).
//
// ⛔ Kernel#raise already did this here (applyRaiseCause). An INTERNAL raise did
// not, and there are TWO doors: raiseWithIvars builds its object up front to
// carry @name/@receiver/@args and is stamped at the raise; the objectless
// raise() helper panics with a RubyError struct and is stamped when the object
// is built in exceptionObject. NameError and NoMethodError from the dispatcher
// come through the first, which is why a fix in the second alone changed
// nothing — measured, not reasoned.
//
// Every expectation was measured on
// `ruby 4.0.7 (2026-09-15 revision 229531a6cf) +PRISM [arm64-darwin25]`
// through scripts/conformance/rubyspec/cmd/oracle; the control (main at
// ac87a757) differs on exactly the three positive rows.
func TestAnInternalRaiseCarriesTheHandledExceptionAsItsCause(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// ⛔ The two rows language/send_spec.rb asserts: "raises NameError with
		// $! as a cause" and "raises NoMethodError with $! as a cause".
		{"a NoMethodError raised inside a rescue names the handled exception",
			causePre + `show("x") do
  begin
    raise "orig"
  rescue
    nil.no_such_method
  end
end`, `x: NoMethodError cause=#<RuntimeError: orig>`},

		{"a NameError from a vcall miss names it too",
			causePre + `show("x") do
  begin
    raise "orig"
  rescue
    no_such_bare_name
  end
end`, `x: NameError cause=#<RuntimeError: orig>`},

		{"the cause is the innermost handled exception, not the outermost",
			causePre + `show("x") do
  begin
    begin
      raise "first"
    rescue
      raise "second"
    end
  rescue
    nil.no_such_method
  end
end`, `x: NoMethodError cause=#<RuntimeError: second>`},

		{"an internal raise inside an ensure sees the exception in flight",
			causePre + `show("x") do
  begin
    begin
      raise "orig-8"
    ensure
      nil.no_such_method
    end
  rescue Exception
    raise
  end
end`, `x: NoMethodError cause=#<RuntimeError: orig-8>`},

		// ⛔ THE NEGATIVE ROWS. Stamping a cause on every internal raise is the
		// way this change could be wrong, and nothing above would notice.
		{"no enclosing rescue means no cause",
			causePre + `show("x") { nil.no_such_method }`,
			`x: NoMethodError cause=nil`},

		{"after the rescue has finished there is nothing to be caused by",
			causePre + `begin
  raise "orig"
rescue
  nil
end
show("x") { nil.no_such_method }`, `x: NoMethodError cause=nil`},

		{"a bare re-raise is the same exception and is not its own cause",
			causePre + `show("x") do
  begin
    raise "orig"
  rescue
    raise
  end
end`, `x: RuntimeError cause=nil`},

		{"an explicit cause: wins over the handled exception",
			causePre + `show("x") do
  begin
    raise "orig"
  rescue
    raise RuntimeError, "chosen", cause: ArgumentError.new("picked")
  end
end`, `x: RuntimeError cause=#<ArgumentError: picked>`},

		{"an explicit nil cause suppresses the automatic one",
			causePre + `show("x") do
  begin
    raise "orig"
  rescue
    raise RuntimeError, "no-cause", cause: nil
  end
end`, `x: RuntimeError cause=nil`},

		// ⛔ The OTHER door, and the reason exceptionObject is stamped as well as
		// raiseWithIvars: these two reach the objectless raise() helper, which
		// panics with a RubyError struct carrying no exception object.
		{"an objectless internal raise is stamped too",
			causePre + `show("x") do
  begin
    raise "orig"
  rescue
    Object.new.instance_variable_get("bad name")
  end
end`, `x: NameError cause=#<RuntimeError: orig>`},

		{"a conversion failure carries the cause as well",
			causePre + `show("x") do
  begin
    raise "orig"
  rescue
    Integer("zz")
  end
end`, `x: ArgumentError cause=#<RuntimeError: orig>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runSrc(t, tc.src); got != tc.want {
				t.Errorf("%s\n got %q\nwant %q", tc.src, got, tc.want)
			}
		})
	}
}
