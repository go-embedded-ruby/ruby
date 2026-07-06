// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"github.com/go-embedded-ruby/ruby/internal/object"
)

// registerActionMailer installs the Action Mailer surface (require
// "action_mailer"), backed by the pure-Go engine of
// github.com/go-ruby-actionmailer/actionmailer:
//
//   - ActionMailer::Base, the class every `class MyMailer < ActionMailer::Base`
//     subclasses: the class-level DSL (default, delivery_method=,
//     perform_deliveries=, raise_delivery_errors=, register_interceptor /
//     register_observer), the shared deliveries Array, and the render/enqueue
//     injection seams (renderer= / enqueuer=). Each mailer action method
//     (`def welcome`) becomes callable as a class method via the method_added
//     hook, returning an ActionMailer::MessageDelivery;
//   - ActionMailer::MessageDelivery, the lazy proxy `MyMailer.welcome(user)`
//     returns: deliver_now / deliver_later / message;
//   - the mailer instance surface a `def welcome` body runs against: mail(...),
//     attachments (attachments[name] = data / attachments.inline[name] = data)
//     and headers(hash).
//
// The message composition, the multipart MIME assembly, the delivery-method
// registry and the interceptor/observer hooks live in the library; this file is
// the class and method wiring, and actionmailer_bind.go holds the four Ruby
// seams (Action, RenderBody, DeliveryMethod, EnqueueJob) and the Ruby <->
// actionmailer value conversions. Every seam runs INLINE on the VM goroutine
// under the GVL, so a mailer action composes synchronously and (with the default
// :test delivery) deliver_now appends to ActionMailer::Base.deliveries.
func (vm *VM) registerActionMailer() {
	mod := newClass("ActionMailer", nil)
	mod.isModule = true
	vm.consts["ActionMailer"] = mod

	vm.amDeliveries = object.NewArrayFromSlice(nil)

	base := newClass("ActionMailer::Base", vm.cObject)
	mod.consts["Base"] = base
	vm.consts["ActionMailer::Base"] = base
	vm.cActionMailerBase = base

	del := newClass("ActionMailer::MessageDelivery", vm.cObject)
	mod.consts["MessageDelivery"] = del
	vm.consts["ActionMailer::MessageDelivery"] = del
	vm.cActionMailerDelivery = del
	vm.registerActionMailerDelivery(del)

	// An internal proxy class for the `attachments` accessor (mirroring
	// Mail::AttachmentsList); not a documented constant, but carried on the value
	// so classOf reports it.
	att := newClass("ActionMailer::Base::Attachments", vm.cObject)
	base.consts["Attachments"] = att
	vm.cActionMailerAttachments = att
	vm.registerActionMailerAttachments(att)

	vm.registerActionMailerDSL(base)
	vm.registerActionMailerInstance(base)
}

// registerActionMailerDSL installs the class-level Action Mailer DSL on
// ActionMailer::Base (inherited by every mailer subclass): the method_added hook
// that turns each mailer action method into a class method, the default /
// delivery / hook declarations, the shared deliveries accessor and the
// render/enqueue injection seams.
func (vm *VM) registerActionMailerDSL(base *RClass) {
	sm := func(name string, fn NativeFn) {
		base.smethods[name] = &Method{name: name, owner: base, native: fn}
	}

	// method_added(:name) fires for every instance method a mailer subclass
	// defines; each becomes a class method that runs the action and returns a
	// MessageDelivery (the analogue of Rails resolving action_methods lazily).
	sm("method_added", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		if cls, ok := self.(*RClass); ok && len(args) > 0 {
			vm.amDefineAction(cls, amKeyStr(args[0]))
		}
		return object.NilV
	})

	// default(key: value, …) records the class's default headers/params, merged
	// down the ancestor chain at delivery time.
	sm("default", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		d := vm.amDefOf(self.(*RClass))
		if h := amTrailingHash(args); h != nil {
			for _, k := range h.Keys {
				v, _ := h.Get(k)
				d.defaults[amKeyStr(k)] = v.ToS()
			}
		}
		return object.NilV
	})

	// delivery_method = :test | a #deliver object.
	sm("delivery_method=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		choice := amArg(args)
		vm.amValidateDelivery(choice)
		vm.amDefOf(self.(*RClass)).delivery = choice
		return choice
	})
	sm("delivery_method", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if d := vm.amDefs[self.(*RClass)]; d != nil && d.delivery != nil {
			return d.delivery
		}
		return object.Symbol("test")
	})

	// perform_deliveries= / raise_delivery_errors= gate delivery.
	sm("perform_deliveries=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := amArg(args).Truthy()
		vm.amDefOf(self.(*RClass)).perform = &b
		return amArg(args)
	})
	sm("perform_deliveries", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if d := vm.amDefs[self.(*RClass)]; d != nil && d.perform != nil {
			return object.Bool(*d.perform)
		}
		return object.Bool(true)
	})
	sm("raise_delivery_errors=", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := amArg(args).Truthy()
		vm.amDefOf(self.(*RClass)).raiseErrors = &b
		return amArg(args)
	})
	sm("raise_delivery_errors", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		if d := vm.amDefs[self.(*RClass)]; d != nil && d.raiseErrors != nil {
			return object.Bool(*d.raiseErrors)
		}
		return object.Bool(true)
	})

	// register_interceptor / register_observer add a delivery hook.
	sm("register_interceptor", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		d := vm.amDefOf(self.(*RClass))
		d.interceptors = append(d.interceptors, amArg(args))
		return object.NilV
	})
	sm("register_observer", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		d := vm.amDefOf(self.(*RClass))
		d.observers = append(d.observers, amArg(args))
		return object.NilV
	})

	// deliveries / deliveries= expose the shared test-delivery sink.
	sm("deliveries", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return vm.amDeliveries
	})
	sm("deliveries=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if arr, ok := amArg(args).(*object.Array); ok {
			vm.amDeliveries = arr
		} else {
			vm.amDeliveries = object.NewArrayFromSlice(nil)
		}
		return vm.amDeliveries
	})

	// renderer= / enqueuer= inject the Action View and Active Job seams (wired to
	// the bound go-ruby-actionview / go-ruby-activejob in the consolidated binary,
	// or a stub in tests).
	sm("renderer=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.amRenderer = amArg(args)
		return vm.amRenderer
	})
	sm("renderer", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if vm.amRenderer == nil {
			return object.NilV
		}
		return vm.amRenderer
	})
	sm("enqueuer=", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		vm.amEnqueuer = amArg(args)
		return vm.amEnqueuer
	})
	sm("enqueuer", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if vm.amEnqueuer == nil {
			return object.NilV
		}
		return vm.amEnqueuer
	})
}

// amDefineAction installs the class method that runs a mailer action: called on a
// mailer class it processes the action with the given arguments and returns the
// ActionMailer::MessageDelivery proxy. Defined once per name (a redefinition of
// the same action keeps the first, which dispatches to the current instance
// method anyway).
func (vm *VM) amDefineAction(cls *RClass, name string) {
	if _, exists := cls.smethods[name]; exists {
		return
	}
	cls.smethods[name] = &Method{name: name, owner: cls, native: func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.amProcess(self.(*RClass), name, args)
	}}
}

// registerActionMailerInstance installs the mailer instance surface a `def
// welcome` body runs against: mail(...), attachments and headers(hash). Each
// resolves the library *Mailer bound to the running action.
func (vm *VM) registerActionMailerInstance(base *RClass) {
	base.define("mail", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := vm.amMailerFor(self)
		if err := m.Mail(vm.amMailOptions(args)); err != nil {
			return vm.amRaise(err)
		}
		return object.NilV
	})
	base.define("attachments", func(vm *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return &ActionMailerAttachments{a: vm.amMailerFor(self).Attachments(), cls: vm.cActionMailerAttachments}
	})
	base.define("headers", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		m := vm.amMailerFor(self)
		if h := amTrailingHash(args); h != nil {
			hdr := map[string]string{}
			for _, k := range h.Keys {
				v, _ := h.Get(k)
				hdr[amKeyStr(k)] = v.ToS()
			}
			m.Headers(hdr)
		}
		return object.NilV
	})
}

// registerActionMailerDelivery installs the ActionMailer::MessageDelivery proxy
// surface: deliver_now / deliver (immediate), deliver_later (through the enqueue
// seam) and message (the composed Mail::Message). A composition or delivery error
// captured on the delivery re-raises here as its Ruby exception.
func (vm *VM) registerActionMailerDelivery(cls *RClass) {
	self := func(v object.Value) *ActionMailerDelivery { return v.(*ActionMailerDelivery) }

	deliverNow := func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		d := self(v).d
		if err := d.DeliverNow(); err != nil {
			return vm.amRaise(err)
		}
		msg, _ := d.Message()
		return amMessageValue(msg)
	}
	cls.define("deliver_now", deliverNow)
	cls.define("deliver", deliverNow)

	cls.define("deliver_later", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		if err := self(v).d.DeliverLater(); err != nil {
			return vm.amRaise(err)
		}
		return object.NilV
	})
	cls.define("message", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		msg, err := self(v).d.Message()
		if err != nil {
			return vm.amRaise(err)
		}
		return amMessageValue(msg)
	})
}

// registerActionMailerAttachments installs the `attachments` proxy surface:
// attachments[name] = data (a regular attachment), attachments.inline (the inline
// sub-proxy) and attachments[name] (the stored bytes, or nil).
func (vm *VM) registerActionMailerAttachments(cls *RClass) {
	self := func(v object.Value) *ActionMailerAttachments { return v.(*ActionMailerAttachments) }

	cls.define("[]=", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		a := self(v)
		name := args[0].ToS()
		data, ct := amAttachmentData(args[1])
		var at = a.a.Set(name, data)
		if a.inline {
			at = a.a.SetInline(name, data)
		}
		if ct != "" {
			at.ContentType = ct
		}
		return args[1]
	})
	cls.define("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) == 0 {
			raise("ArgumentError", "wrong number of arguments (given 0, expected 1)")
		}
		at := self(v).a.Get(args[0].ToS())
		if at == nil {
			return object.NilV
		}
		return object.NewStringBytes(at.Data)
	})
	cls.define("inline", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		a := self(v)
		return &ActionMailerAttachments{a: a.a, inline: true, cls: a.cls}
	})
}

// amArg returns the first argument, or Ruby nil when none was given.
func amArg(args []object.Value) object.Value {
	if len(args) == 0 {
		return object.NilV
	}
	return args[0]
}
