// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"testing"

	mail "github.com/go-ruby-mail/mail"
)

// TestMailShells covers the Go-only arms of the Mail value shells — ToS /
// Inspect / Truthy on Mail::Message, Mail::Body and Mail::Field — plus the
// Mail::Field readers, which the Ruby-level surface does not reach directly (the
// binding exposes fields through the header Hash rather than as Field objects).
func TestMailShells(t *testing.T) {
	raw := "From: a@b.com\r\nSubject: Hi\r\n\r\nBody\r\n"
	m := mail.New(raw)

	msg := &MailMessage{m: mailMsg{m: m}}
	if !strings.Contains(msg.ToS(), "Subject: Hi") {
		t.Errorf("Message.ToS() = %q", msg.ToS())
	}
	if msg.Inspect() != "#<Mail::Message>" {
		t.Errorf("Message.Inspect() = %q", msg.Inspect())
	}
	if !msg.Truthy() {
		t.Error("a Mail::Message must be truthy")
	}

	body := &MailBody{b: mailBody{b: m.Body()}}
	if !strings.Contains(body.ToS(), "Body") {
		t.Errorf("Body.ToS() = %q", body.ToS())
	}
	if body.Inspect() != "#<Mail::Body>" {
		t.Errorf("Body.Inspect() = %q", body.Inspect())
	}
	if !body.Truthy() {
		t.Error("a Mail::Body must be truthy")
	}

	f := m.Header().Get("Subject")
	if f == nil {
		t.Fatal("Subject field missing")
	}
	field := &MailField{f: mailField{f: f}}
	if field.ToS() != "Hi" {
		t.Errorf("Field.ToS() = %q", field.ToS())
	}
	if !strings.Contains(field.Inspect(), "Subject") {
		t.Errorf("Field.Inspect() = %q", field.Inspect())
	}
	if !field.Truthy() {
		t.Error("a Mail::Field must be truthy")
	}
}
