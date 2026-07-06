// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	actionview "github.com/go-ruby-actionview/actionview"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// FormBuilderVal is a Ruby ActionView::Helpers::FormBuilder: the object-bound form
// field helper yielded by form_with. It wraps the library's *actionview.FormBuilder
// so each field method generates the exact user[field] name / user_field id
// conventions and the stored-value defaults the library reproduces.
type FormBuilderVal struct {
	cls *RClass
	fb  *actionview.FormBuilder
}

// ToS renders the default object form; a form builder has no string value.
func (b *FormBuilderVal) ToS() string { return "#<ActionView::Helpers::FormBuilder>" }

// Inspect matches ToS.
func (b *FormBuilderVal) Inspect() string { return b.ToS() }

// Truthy reports true.
func (b *FormBuilderVal) Truthy() bool { return true }

// registerActionViewFormBuilder installs ActionView::Helpers::FormBuilder (the
// object yielded by form_with) and ActionView::Base#form_with, which opens a
// model-bound form, yields a FormBuilder to the block for the fields, and closes
// the form. It runs after registerActionViewBase so ActionView::Base already
// exists to reopen.
func (vm *VM) registerActionViewFormBuilder(mod *RClass) {
	fbCls := newClass("ActionView::Helpers::FormBuilder", vm.cObject)
	vm.consts["ActionView::Helpers::FormBuilder"] = fbCls
	mod.consts["Helpers"] = vm.actionViewHelpersModule(mod, fbCls)

	fbCls.define("object_name", func(_ *VM, self object.Value, _ []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*FormBuilderVal).fb.ObjectName)
	})
	fbCls.define("field_name", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*FormBuilderVal).fb.FieldName(avToS(avArg(args, 0))))
	})
	fbCls.define("field_id", func(_ *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		return object.NewString(self.(*FormBuilderVal).fb.FieldID(avToS(avArg(args, 0))))
	})

	field := func(fn func(*actionview.FormBuilder, string, actionview.Attrs) actionview.SafeBuffer) NativeFn {
		return func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
			b := self.(*FormBuilderVal)
			return vm.newSafeBuffer(fn(b.fb, avToS(avArg(args, 0)), avOpts(avArg(args, 1))))
		}
	}
	fbCls.define("text_field", field((*actionview.FormBuilder).TextField))
	fbCls.define("password_field", field((*actionview.FormBuilder).PasswordField))
	fbCls.define("hidden_field", field((*actionview.FormBuilder).HiddenField))
	fbCls.define("text_area", field((*actionview.FormBuilder).TextArea))

	fbCls.define("label", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*FormBuilderVal)
		return vm.newSafeBuffer(b.fb.Label(avToS(avArg(args, 0)), avToS(avArg(args, 1)), avOpts(avArg(args, 2))))
	})
	fbCls.define("check_box", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*FormBuilderVal)
		return vm.newSafeBuffer(b.fb.CheckBox(avToS(avArg(args, 0)), avToS(avArg(args, 1)), avToS(avArg(args, 2)), avOpts(avArg(args, 3))))
	})
	fbCls.define("radio_button", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*FormBuilderVal)
		return vm.newSafeBuffer(b.fb.RadioButton(avToS(avArg(args, 0)), avToS(avArg(args, 1)), avOpts(avArg(args, 2))))
	})
	fbCls.define("select", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*FormBuilderVal)
		return vm.newSafeBuffer(b.fb.Select(avToS(avArg(args, 0)), avChoices(avArg(args, 1)), avOpts(avArg(args, 2))))
	})
	fbCls.define("submit", func(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
		b := self.(*FormBuilderVal)
		return vm.newSafeBuffer(b.fb.Submit(avToS(avArg(args, 0)), avOpts(avArg(args, 1))))
	})

	base := vm.consts["ActionView::Base"].(*RClass)
	base.define("form_with", func(vm *VM, self object.Value, args []object.Value, blk *Proc) object.Value {
		b := self.(*ActionViewBase)
		h, ok := avArg(args, 0).(*object.Hash)
		if !ok {
			raise("ArgumentError", "form_with expects an options Hash")
		}
		objectName := avHashStr(h, "scope", "")
		url := avHashStr(h, "url", "")
		persisted := avHashBool(h, "persisted", false)
		var htmlOpts actionview.Attrs
		if hv, ok := avHashVal(h, "html"); ok {
			htmlOpts = avOpts(hv)
		}
		out := b.context().FormWith(objectName, avModelMap(h), url, htmlOpts, persisted,
			func(fb *actionview.FormBuilder) actionview.SafeBuffer {
				if blk == nil {
					return actionview.SafeBuffer{}
				}
				return avToSafeBuffer(vm.callBlock(blk, []object.Value{&FormBuilderVal{cls: fbCls, fb: fb}}))
			})
		return vm.newSafeBuffer(out)
	})
}

// actionViewHelpersModule returns the ActionView::Helpers module, holding the
// FormBuilder constant so ActionView::Helpers::FormBuilder resolves by its full
// name.
func (vm *VM) actionViewHelpersModule(mod *RClass, fbCls *RClass) *RClass {
	helpers := newClass("ActionView::Helpers", nil)
	helpers.isModule = true
	helpers.consts["FormBuilder"] = fbCls
	vm.consts["ActionView::Helpers"] = helpers
	return helpers
}

// avModelMap reads the form_with :model option (a Hash of attribute values) into
// the map[string]any the FormBuilder reads field values from. A missing / non-Hash
// :model yields an empty map (a form with no seeded values).
func avModelMap(h *object.Hash) map[string]any {
	mv, ok := avHashVal(h, "model")
	if !ok {
		return map[string]any{}
	}
	mh, ok := mv.(*object.Hash)
	if !ok {
		return map[string]any{}
	}
	out := make(map[string]any, len(mh.Keys))
	for _, k := range mh.Keys {
		val, _ := mh.Get(k)
		out[avKey(k)] = avValue(val)
	}
	return out
}
