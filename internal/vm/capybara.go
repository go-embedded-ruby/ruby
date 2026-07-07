// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	capybara "github.com/go-ruby-capybara/capybara"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// capybaraVersion is the Capybara::VERSION the binding advertises — the gem
// generation whose deterministic rack_test core go-ruby-capybara reproduces,
// not the go-ruby-capybara module version.
const capybaraVersion = "3.40.0"

// CapybaraSession is the Ruby wrapper around a *capybara.Session — the acceptance
// -test driver (Capybara::Session with the default :rack_test driver). The HTML
// parsing, selector engine, form handling and redirect following all live in
// github.com/go-ruby-capybara/capybara; this shell exposes the navigation /
// form / matcher surface to Ruby and wires the App seam to a real Ruby Rack app
// (see capybara_bind.go). The wrapper carries its own class so classOf reports
// Capybara::Session.
type CapybaraSession struct {
	sess *capybara.Session
	cls  *RClass
}

func (s *CapybaraSession) ToS() string     { return "#<Capybara::Session>" }
func (s *CapybaraSession) Inspect() string { return s.ToS() }
func (s *CapybaraSession) Truthy() bool    { return true }

// CapybaraNode is the Ruby wrapper around a *capybara.Node — a single element in
// the current document (Capybara::Node::Element). Interactions on it
// (click/set/…) issue new requests through its owning Session. The wrapper
// carries its own class so classOf reports Capybara::Node::Element.
type CapybaraNode struct {
	node *capybara.Node
	cls  *RClass
}

func (n *CapybaraNode) ToS() string     { return "#<Capybara::Node::Element>" }
func (n *CapybaraNode) Inspect() string { return n.ToS() }
func (n *CapybaraNode) Truthy() bool    { return true }

// registerCapybara installs the Capybara acceptance-test surface (require
// "capybara"), backed by github.com/go-ruby-capybara/capybara: Capybara.app=
// registers the default Rack app; Capybara::Session.new(:rack_test, app) drives
// it in-process — #visit / #click_* / #fill_in / #find / #has_content? and the
// assert_* / within helpers all go through the library's rack_test driver, whose
// App seam sends #call(env) to the Ruby Rack app and converts the [status,
// headers, body] triple back (see capybara_bind.go). The whole flow is
// deterministic Go run inline under the GVL — no browser, no socket.
func (vm *VM) registerCapybara() {
	mod := newClass("Capybara", nil)
	mod.isModule = true
	vm.consts["Capybara"] = mod
	mod.consts["VERSION"] = object.NewString(capybaraVersion)

	vm.registerCapybaraErrors(mod)
	vm.registerCapybaraModuleMethods(mod)
	cls := vm.registerCapybaraSession(mod)
	vm.registerCapybaraNode(mod)
	vm.registerCapybaraDSL(mod, cls)
}

// registerCapybaraErrors installs the Capybara error tree under a common
// Capybara::CapybaraError < StandardError base, mirroring the gem's hierarchy.
// capErrClass (capybara_bind.go) maps each Go error the library returns onto one
// of these class names.
func (vm *VM) registerCapybaraErrors(mod *RClass) {
	std := vm.consts["StandardError"].(*RClass)
	base := newClass("Capybara::CapybaraError", std)
	mod.consts["CapybaraError"] = base
	vm.consts["Capybara::CapybaraError"] = base
	for _, name := range []string{"ElementNotFound", "Ambiguous", "ExpectationNotMet", "UnselectableError", "InfiniteRedirectError"} {
		c := newClass("Capybara::"+name, base)
		mod.consts[name] = c
		vm.consts["Capybara::"+name] = c
	}
}

// registerCapybaraModuleMethods installs the module-level accessors:
// Capybara.app= stores the default Rack app (used by Session.new and the DSL) and
// Capybara.app reads it back (nil when unset).
func (vm *VM) registerCapybaraModuleMethods(mod *RClass) {
	mod.smethods["app="] = &Method{name: "app=", owner: mod, native: func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		app := rackArg(args)
		mod.ivars["app"] = app
		// Changing the app invalidates the memoised DSL session so the next
		// Capybara.current_session drives the new app (matching the gem, which
		// keys its session pool by app).
		delete(mod.ivars, "current_session")
		return app
	}}
	mod.smethods["app"] = &Method{name: "app", owner: mod, native: func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return capybaraDefaultApp(mod)
	}}
}

// capybaraDefaultApp returns the app registered via Capybara.app=, or nil when
// none has been set.
func capybaraDefaultApp(mod *RClass) object.Value {
	if app, ok := mod.ivars["app"]; ok {
		return app
	}
	return object.NilV
}

// registerCapybaraSession installs Capybara::Session and its navigation / form /
// query / matcher methods, returning the class so the DSL can build sessions over
// it.
func (vm *VM) registerCapybaraSession(mod *RClass) *RClass {
	cls := newClass("Capybara::Session", vm.cObject)
	mod.consts["Session"] = cls
	vm.consts["Capybara::Session"] = cls

	cls.smethods["new"] = &Method{name: "new", owner: cls, native: func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.newCapybaraSession(mod, cls, args)
	}}

	// Navigation.
	cls.define("visit", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		capyRaise(capSession(v).Visit(capStr(rackArg(args))))
		return v
	})
	cls.define("current_path", capSessionStr((*capybara.Session).CurrentPath))
	cls.define("current_url", capSessionStr((*capybara.Session).CurrentURL))
	cls.define("body", capSessionStr((*capybara.Session).Body))
	cls.define("html", capSessionStr((*capybara.Session).Body))
	cls.define("status_code", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.IntValue(int64(capSession(v).StatusCode()))
	})

	// Click actions — each is (locator) -> nil, raising on a miss.
	cls.define("click_link", capSessionAct((*capybara.Session).ClickLink))
	cls.define("click_button", capSessionAct((*capybara.Session).ClickButton))
	cls.define("click_on", capSessionAct((*capybara.Session).ClickOn))

	// Form filling.
	cls.define("fill_in", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		capyRaise(capSession(v).FillIn(capStr(rackArg(args)), capKwarg(args, "with")))
		return v
	})
	cls.define("choose", capSessionAct((*capybara.Session).Choose))
	cls.define("check", capSessionAct((*capybara.Session).Check))
	cls.define("uncheck", capSessionAct((*capybara.Session).Uncheck))
	cls.define("select", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		capyRaise(capSession(v).Select(capStr(rackArg(args)), capKwarg(args, "from")))
		return v
	})
	cls.define("attach_file", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		if len(args) < 2 {
			raise("ArgumentError", "wrong number of arguments (given %d, expected 2)", len(args))
		}
		capyRaise(capSession(v).AttachFile(capStr(args[0]), capStr(args[1])))
		return v
	})

	// Finders — (selector) -> Node, raising on a miss / ambiguity.
	cls.define("find", capSessionFind((*capybara.Session).Find))
	cls.define("find_field", capSessionFind((*capybara.Session).FindField))
	cls.define("find_link", capSessionFind((*capybara.Session).FindLink))
	cls.define("find_button", capSessionFind((*capybara.Session).FindButton))
	cls.define("first", capSessionFind((*capybara.Session).First))
	cls.define("all", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		nodes, err := capSession(v).All(capStr(rackArg(args)))
		capyRaise(err)
		return vm.capNodeArray(nodes)
	})

	// Positive matchers.
	cls.define("has_selector?", capSessionHas((*capybara.Session).HasSelector))
	cls.define("has_css?", capSessionHas((*capybara.Session).HasSelector))
	cls.define("has_xpath?", capSessionHas((*capybara.Session).HasXPath))
	cls.define("has_content?", capSessionHas((*capybara.Session).HasContent))
	cls.define("has_text?", capSessionHas((*capybara.Session).HasText))
	cls.define("has_link?", capSessionHas((*capybara.Session).HasLink))
	cls.define("has_button?", capSessionHas((*capybara.Session).HasButton))
	cls.define("has_field?", capSessionHas((*capybara.Session).HasField))

	// Negative matchers.
	cls.define("has_no_selector?", capSessionHas((*capybara.Session).HasNoSelector))
	cls.define("has_no_css?", capSessionHas((*capybara.Session).HasNoSelector))
	cls.define("has_no_xpath?", capSessionHas((*capybara.Session).HasNoXPath))
	cls.define("has_no_content?", capSessionHas((*capybara.Session).HasNoContent))
	cls.define("has_no_text?", capSessionHas((*capybara.Session).HasNoContent))
	cls.define("has_no_link?", capSessionHas((*capybara.Session).HasNoLink))
	cls.define("has_no_button?", capSessionHas((*capybara.Session).HasNoButton))
	cls.define("has_no_field?", capSessionHas((*capybara.Session).HasNoField))

	// Assertions — true on success, raising Capybara::ExpectationNotMet on failure.
	cls.define("assert_selector", capSessionAssert((*capybara.Session).AssertSelector))
	cls.define("assert_text", capSessionAssert((*capybara.Session).AssertText))
	cls.define("assert_no_selector", capSessionAssert((*capybara.Session).AssertNoSelector))
	cls.define("assert_no_text", capSessionAssert((*capybara.Session).AssertNoText))

	// within(scope) { … } scopes every finder/matcher inside the block to the
	// first element matching scope; the block's value is returned.
	cls.define("within", func(vm *VM, v object.Value, args []object.Value, blk *Proc) object.Value {
		if blk == nil {
			raise("LocalJumpError", "no block given (yield)")
		}
		result := object.Value(object.NilV)
		err := capSession(v).Within(capStr(rackArg(args)), func() error {
			result = vm.callBlock(blk, nil)
			return nil
		})
		capyRaise(err)
		return result
	})

	return cls
}

// newCapybaraSession builds a Capybara::Session from Session.new(mode, app): the
// mode must be :rack_test (the only driver this binding provides) and the app
// defaults to Capybara.app. The App seam wraps the resolved Ruby Rack app.
func (vm *VM) newCapybaraSession(mod, cls *RClass, args []object.Value) object.Value {
	if len(args) > 0 {
		if mode := capStr(args[0]); mode != "" && mode != "rack_test" {
			raise("ArgumentError", "unsupported Capybara driver %q (only :rack_test is available)", mode)
		}
	}
	app := object.Value(object.NilV)
	if len(args) > 1 {
		app = args[1]
	}
	if capNil(app) {
		app = capybaraDefaultApp(mod)
	}
	if capNil(app) {
		raise("ArgumentError", "no Rack app given (pass one to Session.new or set Capybara.app)")
	}
	sess := capybara.New(vm.capybaraApp(app))
	return &CapybaraSession{sess: sess, cls: cls}
}

// registerCapybaraNode installs Capybara::Node::Element and its per-element
// methods (Capybara::Node is a namespace module holding the Element class).
func (vm *VM) registerCapybaraNode(mod *RClass) {
	ns := newClass("Capybara::Node", nil)
	ns.isModule = true
	mod.consts["Node"] = ns
	vm.consts["Capybara::Node"] = ns

	cls := newClass("Capybara::Node::Element", vm.cObject)
	ns.consts["Element"] = cls
	vm.consts["Capybara::Node::Element"] = cls

	cls.define("text", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(capNode(v).Text())
	})
	cls.define("value", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(capNode(v).Value())
	})
	cls.define("tag_name", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(capNode(v).TagName())
	})
	cls.define("[]", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		val, ok := capNode(v).Attr(capStr(rackArg(args)))
		if !ok {
			return object.NilV
		}
		return object.NewString(val)
	})
	cls.define("checked?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(capNode(v).Checked())
	})
	cls.define("selected?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(capNode(v).Selected())
	})
	cls.define("visible?", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.Bool(capNode(v).Visible())
	})
	cls.define("click", func(vm *VM, v object.Value, _ []object.Value, _ *Proc) object.Value {
		capyRaise(capNode(v).Click())
		return v
	})
	cls.define("set", func(vm *VM, v object.Value, args []object.Value, _ *Proc) object.Value {
		capyRaise(capNode(v).Set(capStr(rackArg(args))))
		return v
	})
}

// registerCapybaraDSL installs the Capybara::DSL mixin and Capybara.current_session
// (require "capybara/dsl"): a class that `include Capybara::DSL` gets a `page`
// returning the shared session over Capybara.app, and every Session method
// (visit/fill_in/click_button/has_content?/…) forwards to it through
// method_missing — the gem's page-delegating DSL.
func (vm *VM) registerCapybaraDSL(mod, sessionCls *RClass) {
	mod.smethods["current_session"] = &Method{name: "current_session", owner: mod, native: func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if s, ok := mod.ivars["current_session"]; ok {
			return s
		}
		app := capybaraDefaultApp(mod)
		if capNil(app) {
			raise("ArgumentError", "no Rack app given (set Capybara.app before using the DSL)")
		}
		s := &CapybaraSession{sess: capybara.New(vm.capybaraApp(app)), cls: sessionCls}
		mod.ivars["current_session"] = s
		return s
	}}

	dsl := newClass("Capybara::DSL", nil)
	dsl.isModule = true
	mod.consts["DSL"] = dsl
	vm.consts["Capybara::DSL"] = dsl

	page := func(vm *VM) object.Value {
		return vm.send(mod, "current_session", nil, nil)
	}
	dsl.define("page", func(vm *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		return page(vm)
	})
	// Every un-owned message goes to the current session, so `visit "/"` /
	// `fill_in ...` / `has_content? ...` work directly on the includer.
	dsl.define("method_missing", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		// method_missing always receives the message name as args[0], followed by
		// the original call arguments; forward the whole thing to the session.
		name := capStr(rackArg(args))
		return vm.send(page(vm), name, args[1:], blk)
	})
	dsl.define("respond_to_missing?", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return object.Bool(vm.respondsTo(page(vm), capStr(rackArg(args))))
	})
}
