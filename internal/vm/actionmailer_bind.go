// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"errors"

	actionmailer "github.com/go-ruby-actionmailer/actionmailer"
	mail "github.com/go-ruby-mail/mail"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the binding between rbgo's Ruby object graph and the pure-Go
// Action Mailer engine of github.com/go-ruby-actionmailer/actionmailer. The
// library owns message composition (headers, the multipart/alternative ->
// related -> mixed MIME assembly), the delivery-method registry and the
// interceptor/observer hooks; rbgo supplies the four interpreter-dependent seams
// it documents, each dispatched at the Ruby level and run INLINE under the GVL:
//
//   - Action: a mailer method's body is Ruby, so the library's per-action closure
//     sends the mailer action (welcome, …) to a freshly allocated Ruby mailer
//     instance bound to the library *Mailer, mapping a raised Ruby exception into
//     an amError the delivery proxy re-raises faithfully (amProcess / amInvokeAction).
//   - RenderBody: body rendering is Action View, so the seam sends #render to the
//     configured Ruby renderer (ActionMailer::Base.renderer=, wired to the bound
//     go-ruby-actionview in the consolidated binary; an injected stub in tests);
//     a nil renderer or a nil result means "no template for this format" and the
//     format is skipped, exactly as a missing .text/.html template does.
//   - DeliveryMethod: the default appends the composed message to the shared
//     ActionMailer::Base.deliveries Array (the :test method); a Ruby object that
//     responds to #deliver plugs in as a custom transport (delivery_method=).
//   - EnqueueJob: deliver_later's Active Job seam sends #enqueue to the configured
//     Ruby enqueuer (wired to the bound go-ruby-activejob in the consolidated
//     binary; an injected stub in tests) with the delivery as its block; with no
//     enqueuer the delivery runs inline.

// amError carries a raised Ruby exception's class through the Go error the
// Action / RenderBody / delivery seams return, so the composed-message error the
// library captures on a MessageDelivery re-raises as the original Ruby exception
// (amRaise), mirroring how ActionMailer surfaces a composition/delivery failure
// from deliver_now / deliver_later / message.
type amError struct {
	cls *RClass
	msg string
}

func (e *amError) Error() string { return e.msg }

// ActionMailerDelivery is the lazy delivery proxy `MyMailer.welcome(user)`
// returns — a Ruby ActionMailer::MessageDelivery wrapping the library
// *MessageDelivery — responding to deliver_now / deliver_later / message.
type ActionMailerDelivery struct {
	d   *actionmailer.MessageDelivery
	cls *RClass
}

func (d *ActionMailerDelivery) ToS() string     { return "#<ActionMailer::MessageDelivery>" }
func (d *ActionMailerDelivery) Inspect() string { return d.ToS() }
func (d *ActionMailerDelivery) Truthy() bool    { return true }

// ActionMailerAttachments is the mailer's `attachments` proxy (mirroring
// Mail::AttachmentsList): attachments[name] = data adds a regular attachment,
// attachments.inline[name] = data adds an inline one. It wraps the library
// *Attachments of the action's *Mailer; inline records which sub-proxy this is.
type ActionMailerAttachments struct {
	a      *actionmailer.Attachments
	inline bool
	cls    *RClass
}

func (a *ActionMailerAttachments) ToS() string     { return "#<ActionMailer::Base::Attachments>" }
func (a *ActionMailerAttachments) Inspect() string { return a.ToS() }
func (a *ActionMailerAttachments) Truthy() bool    { return true }

// --- class-level declaration model ------------------------------------------

// amDef is the class-level Action Mailer configuration a mailer class accumulates
// as its body runs the DSL (default, delivery_method=, perform_deliveries=,
// raise_delivery_errors=, register_interceptor / register_observer). Each mailer
// class owns one; amBuildBase flattens a class and its ancestors' defs into a
// live *actionmailer.Base at delivery time, so a subclass inherits its parents'
// defaults, delivery method and hooks (ancestor first, subclass overrides).
type amDef struct {
	defaults     map[string]string
	delivery     object.Value // :test symbol or a Ruby object responding to #deliver; nil = inherit/default
	perform      *bool
	raiseErrors  *bool
	interceptors []object.Value
	observers    []object.Value
}

// amDefOf returns the amDef for a mailer class, creating it on first use.
func (vm *VM) amDefOf(cls *RClass) *amDef {
	if vm.amDefs == nil {
		vm.amDefs = map[*RClass]*amDef{}
	}
	d, ok := vm.amDefs[cls]
	if !ok {
		d = &amDef{defaults: map[string]string{}}
		vm.amDefs[cls] = d
	}
	return d
}

// amBuildBase assembles a *actionmailer.Base for a mailer class from its
// declaration chain (ActionMailer::Base-most ancestor first, the mailer last so
// it overrides), wiring the RenderBody and EnqueueJob Ruby-dispatch seams and the
// delivery method. It is built fresh per delivery, so the latest class config
// always applies; Now is left nil so composed output carries no non-deterministic
// Date header.
func (vm *VM) amBuildBase(cls *RClass) *actionmailer.Base {
	b := actionmailer.New(cls.name)
	b.Now = nil
	b.RenderBody = vm.amRenderBody()
	b.EnqueueJob = vm.amEnqueueJob()

	var chain []*RClass
	for c := cls; c != nil && c != vm.cObject; c = c.super {
		chain = append(chain, c)
	}
	var delivery object.Value
	for i := len(chain) - 1; i >= 0; i-- {
		d := vm.amDefs[chain[i]]
		if d == nil {
			continue
		}
		for k, v := range d.defaults {
			b.Default(k, v)
		}
		if d.delivery != nil {
			delivery = d.delivery
		}
		if d.perform != nil {
			b.PerformDeliveries = *d.perform
		}
		if d.raiseErrors != nil {
			b.RaiseDeliveryErrors = *d.raiseErrors
		}
		for _, in := range d.interceptors {
			b.RegisterInterceptor(&amRubyInterceptor{vm: vm, obj: in})
		}
		for _, ob := range d.observers {
			b.RegisterObserver(&amRubyObserver{vm: vm, obj: ob})
		}
	}
	b.DeliveryMethod = vm.amDeliveryMethod(delivery)
	return b
}

// --- action dispatch --------------------------------------------------------

// amProcess runs a mailer action: it builds the class's *actionmailer.Base,
// registers the Ruby action seam and processes the action with the call's
// arguments, returning the Ruby ActionMailer::MessageDelivery proxy. The library
// composes the message eagerly (running the Ruby action body inline here) and
// captures any composition error on the delivery for deliver_now/message to raise.
func (vm *VM) amProcess(cls *RClass, action string, args []object.Value) object.Value {
	b := vm.amBuildBase(cls)
	b.Register(action, func(m *actionmailer.Mailer, params ...any) error {
		inst := &RObject{class: cls, ivars: map[string]object.Value{}}
		vm.amBind(inst, m)
		rubyArgs := make([]object.Value, len(params))
		for i, p := range params {
			rubyArgs[i] = p.(object.Value)
		}
		if aerr := vm.amInvokeAction(inst, action, rubyArgs); aerr != nil {
			return aerr
		}
		return nil
	})
	goArgs := make([]any, len(args))
	for i, a := range args {
		goArgs[i] = a
	}
	return &ActionMailerDelivery{d: b.Process(action, goArgs...), cls: vm.cActionMailerDelivery}
}

// amBind associates a Ruby mailer instance with the library *Mailer of its
// running action, so the mail / attachments / headers instance methods resolve
// the composer for the current invocation.
func (vm *VM) amBind(inst *RObject, m *actionmailer.Mailer) {
	if vm.amMailerOf == nil {
		vm.amMailerOf = map[*RObject]*actionmailer.Mailer{}
	}
	vm.amMailerOf[inst] = m
}

// amMailerFor returns the library *Mailer bound to a mailer instance, raising
// when mail/attachments/headers are called outside a mailer action.
func (vm *VM) amMailerFor(self object.Value) *actionmailer.Mailer {
	if o, ok := self.(*RObject); ok {
		if m := vm.amMailerOf[o]; m != nil {
			return m
		}
	}
	raise("RuntimeError", "no mailer action in progress")
	return nil
}

// amInvokeAction sends the action to the mailer instance, recovering a raised
// Ruby exception into an amError so the library captures it on the delivery
// (mirroring an action that raises during composition). A non-Ruby Go panic
// propagates.
func (vm *VM) amInvokeAction(inst *RObject, action string, args []object.Value) (aerr *amError) {
	defer func() {
		if r := recover(); r != nil {
			e := r.(RubyError)
			aerr = &amError{cls: vm.minitestRaisedClass(e), msg: e.Message}
		}
	}()
	vm.send(inst, action, args, nil)
	return nil
}

// amProtect runs f, recovering a raised Ruby exception into an amError so a seam
// invoked deep inside the library (RenderBody, a Ruby delivery method) surfaces
// the failure through the library's error path rather than unwinding across it.
func (vm *VM) amProtect(f func() object.Value) (res object.Value, aerr *amError) {
	defer func() {
		if r := recover(); r != nil {
			e := r.(RubyError)
			aerr = &amError{cls: vm.minitestRaisedClass(e), msg: e.Message}
		}
	}()
	return f(), nil
}

// amRaise re-raises an error the library surfaced as the faithful Ruby exception:
// a seam failure carries its original Ruby class through amError; any other
// (a library sentinel such as ErrNoDeliveryMethod) raises as a RuntimeError.
func (vm *VM) amRaise(err error) object.Value {
	var ae *amError
	if errors.As(err, &ae) {
		return raise(ae.cls.name, "%s", ae.Error())
	}
	return raise("RuntimeError", "%s", err.Error())
}

// --- RenderBody seam --------------------------------------------------------

// amRenderBody builds the body-rendering seam: it sends #render to the configured
// Ruby renderer with the mailer/action/format/locals, returning the rendered
// String. A nil renderer or a nil #render result signals ErrNoTemplate, so that
// format is skipped; a raise during rendering surfaces as an amError the delivery
// proxy re-raises.
func (vm *VM) amRenderBody() actionmailer.RenderBody {
	return func(mailer, action, format string, locals map[string]any) (string, error) {
		if vm.amRenderer == nil {
			return "", actionmailer.ErrNoTemplate
		}
		h := object.NewHash()
		h.Set(object.Symbol("mailer"), object.NewString(mailer))
		h.Set(object.Symbol("action"), object.NewString(action))
		h.Set(object.Symbol("format"), object.NewString(format))
		h.Set(object.Symbol("locals"), amLocalsToRuby(locals))
		res, aerr := vm.amProtect(func() object.Value {
			return vm.send(vm.amRenderer, "render", []object.Value{h}, nil)
		})
		if aerr != nil {
			return "", aerr
		}
		if object.IsNil(res) {
			return "", actionmailer.ErrNoTemplate
		}
		return res.ToS(), nil
	}
}

// amLocalsToRuby maps the render seam's locals back into a Ruby Hash (the values
// are the Ruby Values captured from the mail(locals:) call).
func amLocalsToRuby(locals map[string]any) object.Value {
	h := object.NewHash()
	for k, v := range locals {
		h.Set(object.NewString(k), v.(object.Value))
	}
	return h
}

// --- EnqueueJob seam --------------------------------------------------------

// amEnqueueJob builds the deliver_later Active Job seam: with a configured Ruby
// enqueuer it sends #enqueue with the delivery as its block (the enqueuer may run
// it now or defer it); with no enqueuer the delivery runs inline. A delivery
// error raised by the inline path propagates.
func (vm *VM) amEnqueueJob() func(func() error) error {
	return func(job func() error) error {
		if vm.amEnqueuer == nil {
			return job()
		}
		blk := &Proc{native: func(_ *VM, _ []object.Value) object.Value {
			if err := job(); err != nil {
				return vm.amRaise(err)
			}
			return object.NilV
		}}
		vm.send(vm.amEnqueuer, "enqueue", nil, blk)
		return nil
	}
}

// --- delivery method + interceptor/observer seams ---------------------------

// amValidateDelivery checks a delivery_method= value at set time (so amDeliveryMethod
// only ever sees a valid choice): :test is the built-in test method, any other
// Symbol is unsupported, and a non-Symbol must respond to #deliver.
func (vm *VM) amValidateDelivery(choice object.Value) {
	if sym, ok := choice.(object.Symbol); ok {
		if string(sym) != "test" {
			raise("ArgumentError", "unsupported delivery_method %q (only :test or a #deliver object)", string(sym))
		}
		return
	}
	if !vm.respondsTo(choice, "deliver") {
		raise("ArgumentError", "delivery_method must be :test or respond to #deliver")
	}
}

// amDeliveryMethod resolves a (validated) delivery-method choice: nil or :test
// appends to the shared ActionMailer::Base.deliveries Array; any other value is a
// Ruby object responding to #deliver, plugged in as a custom transport.
func (vm *VM) amDeliveryMethod(choice object.Value) actionmailer.DeliveryMethod {
	if choice == nil {
		return &amArrayDelivery{vm: vm}
	}
	if sym, ok := choice.(object.Symbol); ok && string(sym) == "test" {
		return &amArrayDelivery{vm: vm}
	}
	return &amRubyDelivery{vm: vm, obj: choice}
}

// amArrayDelivery is the :test delivery method: it appends the delivered message
// to the shared ActionMailer::Base.deliveries Array (read through vm so a
// deliveries= reset is honoured).
type amArrayDelivery struct{ vm *VM }

func (d *amArrayDelivery) Deliver(m *mail.Message) error {
	d.vm.amDeliveries.Elems = append(d.vm.amDeliveries.Elems, amMessageValue(m))
	return nil
}

// amRubyDelivery wraps a Ruby delivery object (delivery_method= obj): Deliver
// sends #deliver with the composed Mail::Message, mapping a raise to an amError.
type amRubyDelivery struct {
	vm  *VM
	obj object.Value
}

func (d *amRubyDelivery) Deliver(m *mail.Message) error {
	_, aerr := d.vm.amProtect(func() object.Value {
		return d.vm.send(d.obj, "deliver", []object.Value{amMessageValue(m)}, nil)
	})
	if aerr != nil {
		return aerr
	}
	return nil
}

// amRubyInterceptor wraps a Ruby interceptor (register_interceptor obj): it sends
// #delivering_email with the message just before delivery.
type amRubyInterceptor struct {
	vm  *VM
	obj object.Value
}

func (i *amRubyInterceptor) DeliveringEmail(m *mail.Message) {
	i.vm.send(i.obj, "delivering_email", []object.Value{amMessageValue(m)}, nil)
}

// amRubyObserver wraps a Ruby observer (register_observer obj): it sends
// #delivered_email with the message just after delivery.
type amRubyObserver struct {
	vm  *VM
	obj object.Value
}

func (o *amRubyObserver) DeliveredEmail(m *mail.Message) {
	o.vm.send(o.obj, "delivered_email", []object.Value{amMessageValue(m)}, nil)
}

// amMessageValue wraps a library *mail.Message as the Ruby Mail::Message value the
// Mail module already binds, so a delivered message is inspectable in Ruby.
func amMessageValue(m *mail.Message) object.Value {
	return &MailMessage{m: mailMsg{m: m}}
}

// --- mail(...) option + attachment parsing ----------------------------------

// amMailOptions maps the keyword Hash of a Ruby mail(...) call into the library
// MailOptions: the recipient lists (to/cc/bcc/reply_to) accept a String or an
// Array, from/subject/content_type/body/formats/locals map to their fields, and
// any other key becomes an extra header.
func (vm *VM) amMailOptions(args []object.Value) actionmailer.MailOptions {
	opts := actionmailer.MailOptions{}
	h := amTrailingHash(args)
	if h == nil {
		return opts
	}
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		switch amKeyStr(k) {
		case "to":
			opts.To = amStrList(v)
		case "cc":
			opts.Cc = amStrList(v)
		case "bcc":
			opts.Bcc = amStrList(v)
		case "reply_to":
			opts.ReplyTo = amStrList(v)
		case "from":
			opts.From = v.ToS()
		case "subject":
			opts.Subject = v.ToS()
		case "content_type":
			opts.ContentType = v.ToS()
		case "body":
			opts.Body = v.ToS()
		case "formats":
			opts.Formats = amStrList(v)
		case "locals":
			opts.Locals = amLocalsFromRuby(v)
		default:
			if opts.Headers == nil {
				opts.Headers = map[string]string{}
			}
			opts.Headers[amKeyStr(k)] = v.ToS()
		}
	}
	return opts
}

// amLocalsFromRuby maps a Ruby locals Hash into the render seam's locals map,
// keying by the String form of each key and carrying the Ruby Value through.
func amLocalsFromRuby(v object.Value) map[string]any {
	h, ok := v.(*object.Hash)
	if !ok {
		return nil
	}
	out := map[string]any{}
	for _, k := range h.Keys {
		val, _ := h.Get(k)
		out[amKeyStr(k)] = val
	}
	return out
}

// amAttachmentData reads the value of an `attachments[name] = …` assignment: a
// bare String is the content; a Hash carries :content (or :data/:body) and an
// optional :content_type / :mime_type override.
func amAttachmentData(v object.Value) (data []byte, contentType string) {
	if h, ok := v.(*object.Hash); ok {
		if c, ok := amHashFetch(h, "content", "data", "body"); ok {
			data = amBytes(c)
		}
		if ct, ok := amHashFetch(h, "content_type", "mime_type"); ok {
			contentType = ct.ToS()
		}
		return data, contentType
	}
	return amBytes(v), ""
}

// amHashFetch returns the first present of the given symbol-or-string keys.
func amHashFetch(h *object.Hash, keys ...string) (object.Value, bool) {
	for _, k := range keys {
		if v, ok := h.Get(object.Symbol(k)); ok {
			return v, true
		}
		if v, ok := h.Get(object.NewString(k)); ok {
			return v, true
		}
	}
	return nil, false
}

// amTrailingHash returns the trailing keyword Hash of an argument list, or nil.
func amTrailingHash(args []object.Value) *object.Hash {
	if len(args) == 0 {
		return nil
	}
	if h, ok := args[len(args)-1].(*object.Hash); ok {
		return h
	}
	return nil
}

// amStrList coerces a recipient value to a list of Strings: an Array yields each
// element's String, any other value yields a single-element list.
func amStrList(v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = e.ToS()
		}
		return out
	}
	return []string{v.ToS()}
}

// amKeyStr renders a Hash/option key as its String form (a Symbol without the
// colon).
func amKeyStr(k object.Value) string {
	if s, ok := k.(object.Symbol); ok {
		return string(s)
	}
	return k.ToS()
}

// amBytes returns the raw bytes of a value: a String's content verbatim (so
// binary attachment data round-trips), any other value its String form.
func amBytes(v object.Value) []byte {
	if s, ok := v.(*object.String); ok {
		return s.Bytes()
	}
	return []byte(v.ToS())
}
