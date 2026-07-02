// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMailConstants covers the Mail module and its value classes (require "mail").
func TestMailConstants(t *testing.T) {
	cases := []struct{ src, want string }{
		{`require "mail"; p Mail.is_a?(Module)`, "true\n"},
		{`p require "mail"`, "true\n"},
		{`require "mail"; p require "mail"`, "false\n"},
		{`require "mail"; p Mail::Message.is_a?(Class)`, "true\n"},
		{`require "mail"; p Mail::Body.is_a?(Class)`, "true\n"},
		{`require "mail"; p Mail::Field.is_a?(Class)`, "true\n"},
		{`require "mail"; p Mail::Part == Mail::Message`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// mailRaw is a single-recipient message used across the parse tests.
const mailRaw = `From: alice@example.com
To: bob@example.com
Subject: Hi there
Date: Mon, 01 Jul 2024 10:00:00 +0000
Message-ID: <abc@example.com>

Hello body
`

// TestMailParse covers Mail.new(raw) and the message accessors.
func TestMailParse(t *testing.T) {
	pre := "require \"mail\"; m = Mail.new(" + rubyStr(mailRaw) + "); "
	cases := []struct{ src, want string }{
		// A single-address field returns the bare String.
		{pre + `p m.from`, "\"alice@example.com\"\n"},
		{pre + `p m.to`, "\"bob@example.com\"\n"},
		{pre + `p m.subject`, "\"Hi there\"\n"},
		{pre + `p m.message_id`, "\"abc@example.com\"\n"},
		{pre + `p m.body.decoded.strip`, "\"Hello body\"\n"},
		{pre + `p m.body.class.name`, "\"Mail::Body\"\n"},
		{pre + `p m.multipart?`, "false\n"},
		{pre + `p m.class.name`, "\"Mail::Message\"\n"},
		{pre + `p m.inspect`, "\"#<Mail::Message>\"\n"},
		// header[name] returns the raw field value; an absent field is nil.
		{pre + `p m["Subject"]`, "\"Hi there\"\n"},
		{pre + `p m["X-Absent"]`, "nil\n"},
		{pre + `p m.header["Subject"]`, "\"Hi there\"\n"},
		// date reads the Date: header as a Time.
		{pre + `p m.date.year`, "2024\n"},
		// An absent single-value field reads as nil.
		{pre + `p m.cc`, "nil\n"},
		{pre + `p m.bcc`, "nil\n"},
		{pre + `p m.reply_to`, "nil\n"},
		{pre + `p m.in_reply_to`, "nil\n"},
		{pre + `p m.mime_type`, "nil\n"},
		{pre + `p m.content_transfer_encoding`, "nil\n"},
		{pre + `p m.content_description`, "nil\n"},
		{pre + `p m.content_disposition`, "nil\n"},
		{pre + `p m.content_id`, "nil\n"},
		{pre + `p m.charset`, "nil\n"},
		{pre + `p m.filename`, "nil\n"},
		{pre + `p m.content_type`, "nil\n"},
		// date_string reads the raw Date: header.
		{pre + `p m.date_string.include?("2024")`, "true\n"},
		// decoded returns the message's decoded body content.
		{pre + `p m.decoded.include?("Hello body")`, "true\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMailMultiAddress covers the Array-return path for a multi-recipient field.
func TestMailMultiAddress(t *testing.T) {
	raw := "To: bob@example.com, carol@example.com\nSubject: x\n\nbody\n"
	pre := "require \"mail\"; m = Mail.new(" + rubyStr(raw) + "); "
	cases := []struct{ src, want string }{
		{pre + `p m.to`, "[\"bob@example.com\", \"carol@example.com\"]\n"},
		{pre + `p m.from`, "nil\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMailBuilder covers the Mail.new { … } block DSL and the setters.
func TestMailBuilder(t *testing.T) {
	src := `require "mail"
m = Mail.new do
  from "x@y.com"
  to "a@b.com"
  subject "Greetings"
  body "The body"
end
puts m.from
puts m.to
puts m.subject
puts m.body.decoded
puts m.encoded.include?("Subject: Greetings")`
	want := "x@y.com\na@b.com\nGreetings\nThe body\ntrue\n"
	if got := eval(t, src); got != want {
		t.Errorf("builder: got %q want %q", got, want)
	}
	// The name= assignment setters are directly callable, and to_s == encoded.
	src2 := `require "mail"
m = Mail.new
m.from = "p@q.com"
m.to = "r@s.com"
m.cc = "c@d.com"
m.bcc = "b@d.com"
m.reply_to = "rt@d.com"
m.subject = "S"
m.body = "B"
m.message_id = "<id@d.com>"
m.content_type = "text/plain"
m.date = "Mon, 01 Jul 2024 10:00:00 +0000"
puts m.from
puts m.cc
puts m.bcc
puts m.reply_to
puts m.message_id
puts m.to_s.include?("From: p@q.com")`
	want2 := "p@q.com\nc@d.com\nb@d.com\nrt@d.com\nid@d.com\ntrue\n"
	if got := eval(t, src2); got != want2 {
		t.Errorf("setters: got %q want %q", got, want2)
	}
	// The DSL dual form sets cc / bcc / reply_to when called with an argument.
	src3 := `require "mail"
m = Mail.new do
  from "a@b.com"
  cc "c@d.com"
  bcc "e@f.com"
  reply_to "g@h.com"
  subject "S"
end
puts m.cc
puts m.bcc
puts m.reply_to
puts m.subject`
	if got := eval(t, src3); got != "c@d.com\ne@f.com\ng@h.com\nS\n" {
		t.Errorf("dsl cc/bcc: got %q", got)
	}
	// Mail.new with no args yields an empty message (no from).
	if got := eval(t, `require "mail"; p Mail.new.from`); got != "nil\n" {
		t.Errorf("empty new: got %q", got)
	}
	// Mail.new(nil) is treated as an empty message.
	if got := eval(t, `require "mail"; p Mail.new(nil).subject`); got != "nil\n" {
		t.Errorf("nil new: got %q", got)
	}
}

// TestMailMultipart covers multipart?, parts, attachments and the part readers.
func TestMailMultipart(t *testing.T) {
	raw := "MIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=BOUND\n\n" +
		"--BOUND\nContent-Type: text/plain\n\ntext part\n" +
		"--BOUND\nContent-Type: application/octet-stream\nContent-Disposition: attachment; filename=\"a.bin\"\n\ndata\n" +
		"--BOUND--\n"
	pre := "require \"mail\"; m = Mail.new(" + rubyStr(raw) + "); "
	cases := []struct{ src, want string }{
		{pre + `p m.multipart?`, "true\n"},
		{pre + `p m.parts.length`, "2\n"},
		{pre + `p m.parts.first.class.name`, "\"Mail::Message\"\n"},
		{pre + `p m.attachments.length`, "1\n"},
		{pre + `p m.attachments.first.filename`, "\"a.bin\"\n"},
		{pre + `p m.attachments.first.attachment?`, "true\n"},
		{pre + `p m.text_part.nil?`, "false\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
	// A single-part message has no text/html sub-part.
	if got := eval(t, `require "mail"; m = Mail.new("Subject: x\n\nbody\n"); p m.html_part`); got != "nil\n" {
		t.Errorf("html_part nil: got %q", got)
	}
	// A message with no Date: header reads date as nil (mailDateValue nil arm).
	if got := eval(t, `require "mail"; m = Mail.new("Subject: x\n\nbody\n"); p m.date`); got != "nil\n" {
		t.Errorf("date nil: got %q", got)
	}
}

// TestMailBodyField covers the Mail::Body and Mail::Field readers directly.
func TestMailBodyField(t *testing.T) {
	pre := "require \"mail\"; m = Mail.new(" + rubyStr(mailRaw) + "); "
	cases := []struct{ src, want string }{
		{pre + `p m.body.to_s.strip`, "\"Hello body\"\n"},
		{pre + `p m.body.raw_source.is_a?(String)`, "true\n"},
		{pre + `p m.body.encoding.is_a?(String)`, "true\n"},
		// header_fields exposes Mail::Field value objects.
		{pre + `p m.header_fields.first.class.name`, "\"Mail::Field\"\n"},
		{pre + `f = m.header_fields.find { |x| x.name == "Subject" }; p f.value`, "\"Hi there\"\n"},
		{pre + `f = m.header_fields.find { |x| x.name == "Subject" }; p f.to_s`, "\"Hi there\"\n"},
		{pre + `f = m.header_fields.find { |x| x.name == "Subject" }; p f.decoded`, "\"Hi there\"\n"},
	}
	for _, c := range cases {
		if got := eval(t, c.src); got != c.want {
			t.Errorf("src=%q got=%q want=%q", c.src, got, c.want)
		}
	}
}

// TestMailRead covers Mail.read over a real file and the ENOENT path.
func TestMailRead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "msg.eml")
	if err := os.WriteFile(p, []byte(mailRaw), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `require "mail"; m = Mail.read(` + rubyStr(p) + `); puts m.subject`
	if got := eval(t, src); got != "Hi there\n" {
		t.Errorf("read: got %q", got)
	}
	// A missing file raises Errno::ENOENT.
	src2 := `require "mail"
begin
  Mail.read(` + rubyStr(filepath.Join(dir, "nope.eml")) + `)
rescue Errno::ENOENT
  puts "enoent"
end`
	if got := eval(t, src2); !strings.Contains(got, "enoent") {
		t.Errorf("read missing: got %q", got)
	}
}

// TestMailErrors covers the arity paths on the setters and module methods.
func TestMailErrors(t *testing.T) {
	for _, call := range []string{
		`Mail.read`,
		`Mail.new("Subject: x\n\ny\n")[]`,
		`Mail.new.send(:from=)`,
	} {
		src := `require "mail"
begin
  ` + call + `
rescue ArgumentError
  puts "arity"
end`
		if got := eval(t, src); !strings.Contains(got, "arity") {
			t.Errorf("%s: got %q", call, got)
		}
	}
}
