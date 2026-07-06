// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import "testing"

// amSrc prefixes a program with require "action_mailer".
func amSrc(body string) string { return "require \"action_mailer\"\n" + body }

// amRendererStub is a program fragment installing a renderer stub that renders a
// text and an html body from the :name local.
const amRendererStub = `
stub = Object.new
def stub.render(opts)
  n = opts[:locals]["name"]
  case opts[:format]
  when "text" then "Hi #{n}"
  when "html" then "<h1>Hi #{n}</h1>"
  end
end
ActionMailer::Base.renderer = stub
`

func amCheck(t *testing.T, src, want string) {
	t.Helper()
	if got := eval(t, amSrc(src)); got != want {
		t.Errorf("src=%q\n got=%q\nwant=%q", src, got, want)
	}
}

// TestActionMailerRequire covers feature registration and the constant tree.
func TestActionMailerRequire(t *testing.T) {
	cases := []struct{ src, want string }{
		{`p require "action_mailer"`, "true\n"},
		{`require "action_mailer"; p require "action_mailer"`, "false\n"},
		{`p require "actionmailer"`, "true\n"},
		{`require "action_mailer"; p ActionMailer.is_a?(Module)`, "true\n"},
		{`require "action_mailer"; p ActionMailer::Base.is_a?(Class)`, "true\n"},
		{`require "action_mailer"; p ActionMailer::MessageDelivery.is_a?(Class)`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestActionMailerCompose covers the end-to-end compose+deliver flow: the default
// from, the rendered multipart/alternative body, deliver_now returning the
// message, the shared deliveries Array and #message.
func TestActionMailerCompose(t *testing.T) {
	amCheck(t, amRendererStub+`
class UserMailer < ActionMailer::Base
  default from: "notifications@example.com"
  def welcome(name)
    mail(to: "ada@example.com", subject: "Welcome", locals: { name: name })
  end
end
md = UserMailer.welcome("Ada")
p md.class.name
msg = md.deliver_now
p msg.subject
p msg.to
p msg.from
p ActionMailer::Base.deliveries.length
e = md.message.encoded
puts e.include?("multipart/alternative")
puts e.include?("Hi Ada")
puts e.include?("<h1>Hi Ada</h1>")
`, "\"ActionMailer::MessageDelivery\"\n\"Welcome\"\n\"ada@example.com\"\n\"notifications@example.com\"\n1\ntrue\ntrue\ntrue\n")
}

// TestActionMailerRecipientsAndHeaders covers recipient lists (String and Array),
// an explicit from, a custom header from an unknown mail(...) key and the
// headers(hash) instance method.
func TestActionMailerRecipientsAndHeaders(t *testing.T) {
	amCheck(t, amRendererStub+`
class M < ActionMailer::Base
  def multi
    headers("X-Extra" => "e")
    mail(from: "s@x.com", to: ["a@x.com", "b@x.com"], cc: "c@x.com",
         bcc: ["d@x.com"], reply_to: "r@x.com", subject: "Hi",
         "X-Custom" => "v", locals: {})
  end
end
msg = M.multi.deliver_now
p msg.to
p msg.cc
e = msg.encoded
puts e.include?("X-Custom: v")
puts e.include?("X-Extra: e")
puts e.include?("Reply-To: r@x.com")
`, "[\"a@x.com\", \"b@x.com\"]\n\"c@x.com\"\ntrue\ntrue\ntrue\n")
}

// TestActionMailerExplicitBody covers mail(body:, content_type:) which bypasses
// the RenderBody seam, and a mailer with no renderer configured.
func TestActionMailerExplicitBody(t *testing.T) {
	amCheck(t, `
class Plain < ActionMailer::Base
  def note
    mail(to: "a@x.com", subject: "N", body: "hello", content_type: "text/plain")
  end
end
e = Plain.note.deliver_now.encoded
puts e.include?("hello")
puts e.include?("multipart") == false
`, "true\ntrue\n")
}

// TestActionMailerNoTemplate covers the RenderBody seam skipping a format the
// renderer has no template for (nil result) and rendering only the other.
func TestActionMailerNoTemplate(t *testing.T) {
	amCheck(t, `
stub = Object.new
def stub.render(opts)
  opts[:format] == "text" ? "only text" : nil
end
ActionMailer::Base.renderer = stub
class M < ActionMailer::Base
  def go; mail(to: "a@x.com", subject: "S", locals: {}); end
end
e = M.go.deliver_now.encoded
puts e.include?("only text")
puts e.include?("multipart/alternative") == false
`, "true\ntrue\n")
}

// TestActionMailerFormats covers the mail(formats:) option restricting rendering
// to a single format.
func TestActionMailerFormats(t *testing.T) {
	amCheck(t, amRendererStub+`
class M < ActionMailer::Base
  def go; mail(to: "a@x.com", subject: "S", formats: ["text"], locals: {name: "Zo"}); end
end
e = M.go.deliver_now.encoded
puts e.include?("Hi Zo")
puts e.include?("<h1>") == false
`, "true\ntrue\n")
}

// TestActionMailerAttachments covers regular attachments (multipart/mixed), inline
// attachments (multipart/related + Content-ID), the hash form with a content_type
// override and the attachments[name] reader (present and absent).
func TestActionMailerAttachments(t *testing.T) {
	amCheck(t, amRendererStub+`
class M < ActionMailer::Base
  def go
    attachments["a.txt"] = "plain"
    attachments.inline["logo.png"] = "PNGDATA"
    attachments["data.bin"] = { content: "raw", mime_type: "application/x-thing" }
    p attachments["a.txt"]
    p attachments["missing"]
    mail(to: "a@x.com", subject: "S", locals: {name: "Q"})
  end
end
e = M.go.deliver_now.encoded
puts e.include?("multipart/mixed")
puts e.include?("multipart/related")
puts e.include?("Content-ID: <logo.png>")
puts e.include?("application/x-thing")
`, "\"plain\"\nnil\ntrue\ntrue\ntrue\ntrue\n")
}

// TestActionMailerDeliverLater covers deliver_later run inline (no enqueuer), via
// an enqueuer stub that yields (runs the delivery) and via one that defers it.
func TestActionMailerDeliverLater(t *testing.T) {
	amCheck(t, amRendererStub+`
class M < ActionMailer::Base
  def go; mail(to: "a@x.com", subject: "S", locals: {name: "L"}); end
end
# inline (no enqueuer)
M.go.deliver_later
p ActionMailer::Base.deliveries.length

# enqueuer that runs immediately
runner = Object.new
def runner.enqueue; yield; end
ActionMailer::Base.enqueuer = runner
M.go.deliver_later
p ActionMailer::Base.deliveries.length

# enqueuer that defers (never yields)
defer = Object.new
def defer.enqueue; end
ActionMailer::Base.enqueuer = defer
M.go.deliver_later
p ActionMailer::Base.deliveries.length
p ActionMailer::Base.enqueuer.equal?(defer)
`, "1\n2\n2\ntrue\n")
}

// TestActionMailerInterceptorsObservers covers register_interceptor and
// register_observer hooks firing around delivery.
func TestActionMailerInterceptorsObservers(t *testing.T) {
	amCheck(t, amRendererStub+`
$log = []
icept = Object.new
def icept.delivering_email(m); $log << "before:#{m.subject}"; end
obs = Object.new
def obs.delivered_email(m); $log << "after:#{m.subject}"; end
class M < ActionMailer::Base
  def go; mail(to: "a@x.com", subject: "S", locals: {name: "I"}); end
end
ActionMailer::Base.register_interceptor(icept)
ActionMailer::Base.register_observer(obs)
M.go.deliver_now
p $log
`, "[\"before:S\", \"after:S\"]\n")
}

// TestActionMailerDeliveryMethod covers delivery_method= with :test, the reader,
// a custom #deliver object, and the unsupported-symbol / non-#deliver raises.
func TestActionMailerDeliveryMethod(t *testing.T) {
	amCheck(t, amRendererStub+`
$sent = []
custom = Object.new
def custom.deliver(m); $sent << m.subject; end
class M < ActionMailer::Base
  def go; mail(to: "a@x.com", subject: "S", locals: {name: "D"}); end
end
p M.delivery_method
M.delivery_method = :test
p M.delivery_method
M.delivery_method = custom
M.go.deliver_now
p $sent
p ActionMailer::Base.deliveries.length
begin
  M.delivery_method = :smtp
  M.go.deliver_now
rescue ArgumentError => e
  puts "bad-sym"
end
begin
  M.delivery_method = Object.new
  M.go.deliver_now
rescue ArgumentError
  puts "bad-obj"
end
`, ":test\n:test\n[\"S\"]\n0\nbad-sym\nbad-obj\n")
}

// TestActionMailerPerformAndRaiseFlags covers perform_deliveries= false (delivery
// skipped) and raise_delivery_errors gating a delivery-method error.
func TestActionMailerPerformAndRaiseFlags(t *testing.T) {
	amCheck(t, amRendererStub+`
class Quiet < ActionMailer::Base
  def go; mail(to: "a@x.com", subject: "S", locals: {name: "P"}); end
end
p Quiet.perform_deliveries
p Quiet.raise_delivery_errors
Quiet.perform_deliveries = false
Quiet.go.deliver_now
p ActionMailer::Base.deliveries.length

boom = Object.new
def boom.deliver(m); raise "smtp down"; end
class Loud < ActionMailer::Base
  def go; mail(to: "a@x.com", subject: "S", locals: {name: "R"}); end
end
Loud.delivery_method = boom
begin
  Loud.go.deliver_now
rescue RuntimeError => e
  puts "raised:#{e.message}"
end
Loud.raise_delivery_errors = false
p Loud.raise_delivery_errors
Loud.go.deliver_now
puts "swallowed"
`, "true\ntrue\n0\nraised:smtp down\nfalse\nswallowed\n")
}

// TestActionMailerDeliveriesReset covers deliveries= replacing the sink (with an
// Array and with a non-Array reset) and the renderer/enqueuer nil readers.
func TestActionMailerDeliveriesReset(t *testing.T) {
	amCheck(t, `
p ActionMailer::Base.renderer
p ActionMailer::Base.enqueuer
class M < ActionMailer::Base
  def go; mail(to: "a@x.com", subject: "S", body: "b"); end
end
M.go.deliver_now
p ActionMailer::Base.deliveries.length
ActionMailer::Base.deliveries = []
p ActionMailer::Base.deliveries.length
M.go.deliver_now
p ActionMailer::Base.deliveries.length
ActionMailer::Base.deliveries = nil
p ActionMailer::Base.deliveries.length
`, "nil\nnil\n1\n0\n1\n0\n")
}

// TestActionMailerErrors covers the composition error paths: an action that
// raises, an action that never calls mail, a renderer that raises, and calling
// mail outside a mailer action.
func TestActionMailerErrors(t *testing.T) {
	amCheck(t, `
class Boom < ActionMailer::Base
  def go(x); raise ArgumentError, "no #{x}"; end
  def empty; @x = 1; end
end
begin
  Boom.go("way").deliver_now
rescue ArgumentError => e
  puts "action:#{e.message}"
end
begin
  Boom.empty.message
rescue => e
  puts "nomail"
end
begin
  Boom.new.send(:mail, to: "a@x.com")
rescue RuntimeError
  puts "outside"
end

bad = Object.new
def bad.render(opts); raise "tmpl broke"; end
ActionMailer::Base.renderer = bad
class R < ActionMailer::Base
  def go; mail(to: "a@x.com", subject: "S", locals: {}); end
end
begin
  R.go.deliver_now
rescue RuntimeError => e
  puts "render:#{e.message}"
end
`, "action:no way\nnomail\noutside\nrender:tmpl broke\n")
}

// TestActionMailerInheritance covers a subclass inheriting the parent's default
// and a parent-defined action.
func TestActionMailerInheritance(t *testing.T) {
	amCheck(t, amRendererStub+`
class AppMailer < ActionMailer::Base
  default from: "app@x.com"
  def base_action; mail(to: "a@x.com", subject: "Base", locals: {name: "N"}); end
end
class ChildMailer < AppMailer
  def child_action; mail(to: "b@x.com", subject: "Child", locals: {name: "C"}); end
end
m1 = ChildMailer.child_action.deliver_now
p m1.from
p m1.subject
m2 = ChildMailer.base_action.deliver_now
p m2.subject
`, "\"app@x.com\"\n\"Child\"\n\"Base\"\n")
}

// TestActionMailerValueMethods covers the ToS/Inspect/Truthy of the delivery and
// attachments Ruby value wrappers.
func TestActionMailerValueMethods(t *testing.T) {
	amCheck(t, `
class M < ActionMailer::Base
  def go
    a = attachments
    puts a.inspect
    puts a.to_s
    puts(a ? "at" : "af")
    mail(to: "a@x.com", subject: "S", body: "b")
  end
end
md = M.go
p md
puts md.to_s
puts(md ? "t" : "f")
`, "#<ActionMailer::Base::Attachments>\n#<ActionMailer::Base::Attachments>\nat\n#<ActionMailer::MessageDelivery>\n#<ActionMailer::MessageDelivery>\nt\n")
}

// TestActionMailerRedefine covers the method_added hook seeing a redefined action
// name (the class method is installed once).
func TestActionMailerRedefine(t *testing.T) {
	amCheck(t, `
class M < ActionMailer::Base
  def go; mail(to: "a@x.com", subject: "One", body: "b"); end
  def go; mail(to: "a@x.com", subject: "Two", body: "b"); end
end
p M.go.deliver_now.subject
`, "\"Two\"\n")
}

// TestActionMailerZeroArgHooks covers registering a hook with no argument (the
// amArg empty path) without delivering.
func TestActionMailerZeroArgHooks(t *testing.T) {
	amCheck(t, `
p ActionMailer::Base.send(:register_interceptor)
`, "nil\n")
}

// TestActionMailerNoRendererEmptyBody covers the RenderBody seam with no renderer
// configured and no explicit body: both formats skip, yielding an empty body.
func TestActionMailerNoRendererEmptyBody(t *testing.T) {
	amCheck(t, `
class M < ActionMailer::Base
  def go; mail(to: "a@x.com", subject: "S"); end
end
e = M.go.deliver_now.encoded
puts e.include?("Subject: S")
puts e.include?("multipart") == false
`, "true\ntrue\n")
}

// TestActionMailerTwoCustomHeaders covers the second-custom-header branch of the
// mail(...) option parser (Headers map already allocated).
func TestActionMailerTwoCustomHeaders(t *testing.T) {
	amCheck(t, `
class M < ActionMailer::Base
  def go; mail(to: "a@x.com", subject: "S", body: "b", "X-A" => "1", "X-B" => "2"); end
end
e = M.go.deliver_now.encoded
puts e.include?("X-A: 1")
puts e.include?("X-B: 2")
`, "true\ntrue\n")
}

// TestActionMailerOptionEdges covers non-Hash locals, a String attachment value
// coerced from a non-String, the attachments hash form with string keys, the
// attachments[]=/[] argument-count raises, mail() with no options and a
// non-Hash headers argument.
func TestActionMailerOptionEdges(t *testing.T) {
	amCheck(t, `
class M < ActionMailer::Base
  def go
    attachments["n.bin"] = 12345
    attachments["s.dat"] = { "content" => "x", "content_type" => "application/x-str" }
    attachments["empty.dat"] = { "note" => "no content here" }
    begin; attachments.send(:[]=, "only"); rescue ArgumentError; puts "set-args"; end
    begin; attachments.send(:[]); rescue ArgumentError; puts "get-args"; end
    mail(to: "a@x.com", subject: "S", body: "b", locals: "not-a-hash")
  end
end
e = M.go.deliver_now.encoded
puts e.include?("application/x-str")
puts e.include?("n.bin")

class N < ActionMailer::Base
  def go
    headers("just-a-string")
    mail
  end
end
N.go.deliver_now
puts "ok"
`, "set-args\nget-args\ntrue\ntrue\nok\n")
}

// TestActionMailerDeliverLaterErrors covers deliver_later surfacing a delivery
// error inline and through an enqueuer that runs the deferred delivery.
func TestActionMailerDeliverLaterErrors(t *testing.T) {
	amCheck(t, `
boom = Object.new
def boom.deliver(m); raise "down"; end
class M < ActionMailer::Base
  def go; mail(to: "a@x.com", subject: "S", body: "b"); end
end
M.delivery_method = boom
begin
  M.go.deliver_later
rescue RuntimeError => e
  puts "inline:#{e.message}"
end

runner = Object.new
def runner.enqueue; yield; end
ActionMailer::Base.enqueuer = runner
begin
  M.go.deliver_later
rescue RuntimeError => e
  puts "enq:#{e.message}"
end
`, "inline:down\nenq:down\n")
}

// TestActionMailerReaderBranches covers the renderer reader when set, the :test
// delivery method reaching the composer, and the perform_deliveries reader after
// it is declared.
func TestActionMailerReaderBranches(t *testing.T) {
	amCheck(t, amRendererStub+`
class M < ActionMailer::Base
  def go; mail(to: "a@x.com", subject: "S", locals: {name: "Z"}); end
end
puts ActionMailer::Base.renderer.nil?
M.delivery_method = :test
M.go.deliver_now
p ActionMailer::Base.deliveries.length
M.perform_deliveries = false
p M.perform_deliveries
`, "false\n1\nfalse\n")
}
