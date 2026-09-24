// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"sort"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// Refinement#import_methods — copying Ruby-defined instance methods of one or
// more plain modules INTO a refinement, so they behave as if they had been
// written in the refine block itself.
//
// MRI (ruby/ruby v3_4_0 eval.c refinement_import_methods):
//
//	rb_check_arity(argc, 1, UNLIMITED_ARGUMENTS);
//	for (i = 0; i < argc; i++) {
//	    Check_Type(argv[i], T_MODULE);
//	    if (RCLASS_SUPER(argv[i])) {
//	        rb_warn("%"PRIsVALUE" has ancestors, but Refinement#import_methods "
//	                "doesn't import their methods", rb_class_path(argv[i]));
//	    }
//	}
//	arg.cref = rb_vm_cref_replace_with_duplicated_cref();
//	arg.refinement = refinement;
//	for (i = 0; i < argc; i++) { … rb_id_table_foreach(m_tbl, …); }
//	return refinement;
//
// and refinement_import_methods_i, which does the copying:
//
//	if (me->def->type != VM_METHOD_TYPE_ISEQ) {
//	    rb_raise(rb_eArgError, "Can't import method which is not defined with "
//	             "Ruby code: %"PRIsVALUE"#%"PRIsVALUE, …);
//	}
//	rb_cref_t *new_cref = rb_vm_cref_dup_without_refinements(me->def->body.iseq.cref);
//	CREF_REFINEMENTS_SET(new_cref, CREF_REFINEMENTS(arg->cref));
//	rb_add_method_iseq(arg->refinement, key, me->def->body.iseq.iseqptr,
//	                   new_cref, METHOD_ENTRY_VISI(me));
//
// Four consequences the ruby/spec suite pins, each reproduced below:
//
//   - Every argument is type-checked BEFORE anything is copied, so a bad
//     argument late in the list imports nothing at all; but the
//     "not defined with Ruby code" check happens DURING the copy, so the
//     modules listed before the offending one keep their imports.
//   - Only the module's OWN instance method table is walked: neither its
//     included/prepended modules (hence the warning) nor its class methods.
//   - The copy keeps its source cref for constants but takes the refinement's
//     refinements, so an imported method sees the other imported methods and
//     the rest of the refine block. Here the copy keeps the source method's
//     lexScope (rbgo's cref for constant lookup) and takes the refinement as
//     its owner, which is what this VM's refinement dispatch keys on.
//   - Visibility is carried over with the method.

// installRefinementImport defines Refinement#import_methods. It is private, as
// MRI defines it, so it is reached as a bare call with the refinement as self —
// which is exactly how a refine block body invokes it.
func (vm *VM) installRefinementImport() {
	m := &Method{
		name:   "import_methods",
		native: refinementImportMethods,
		owner:  vm.cRefinement,
		vis:    visPrivate,
	}
	vm.cRefinement.methods["import_methods"] = m
}

// refinementImportMethods is the Refinement#import_methods body.
func refinementImportMethods(vm *VM, self object.Value, args []object.Value, _ *Proc) object.Value {
	r, ok := self.(*RClass)
	if !ok || !r.isRefinement {
		raise("TypeError", "wrong argument type %s (expected Refinement)", vm.classOf(self).name)
	}
	if len(args) == 0 {
		raise("ArgumentError", "wrong number of arguments (given 0, expected 1+)")
	}
	// Pass 1: Check_Type every argument first, so one bad argument leaves the
	// refinement untouched, then warn for each module that has ancestors
	// (RCLASS_SUPER non-NULL for a module means it includes or prepends one).
	mods := make([]*RClass, len(args))
	for i, a := range args {
		mod, isMod := a.(*RClass)
		if !isMod || !mod.isModule {
			raise("TypeError", "wrong argument type %s (expected Module)", vm.classOf(a).name)
		}
		mods[i] = mod
	}
	for _, mod := range mods {
		if len(mod.includes) > 0 || len(mod.prepends) > 0 {
			vm.rbWarn("warning: %s has ancestors, but Refinement#import_methods doesn't import their methods",
				vm.moduleToSStr(mod))
		}
	}
	// Pass 2: copy. Modules are walked in argument order, so a name defined in
	// several of them ends up holding the LAST one's definition.
	for _, mod := range mods {
		for _, name := range sortedMethodNames(mod.methods) {
			src := mod.methods[name]
			if src.undefined {
				continue
			}
			if !definedWithRubyCode(src) {
				raise("ArgumentError", "Can't import method which is not defined with Ruby code: %s#%s",
					vm.moduleToSStr(mod), name)
			}
			cp := *src
			// The refinement owns the copy: this VM reads a frame's refinement
			// scope off the running method's owner, so an imported method that
			// calls another imported method (or anything else the refine block
			// defines) resolves it through the refinement, as MRI's
			// CREF_REFINEMENTS_SET arranges.
			cp.owner = r
			// Constants keep resolving where the method was WRITTEN
			// (rb_vm_cref_dup_without_refinements preserves the source cref), so
			// pin lexScope to the source module unless the source already carried
			// an explicit one.
			if cp.lexScope == nil {
				cp.lexScope = src.owner
			}
			r.methods[name] = &cp
		}
	}
	return r
}

// definedWithRubyCode reports whether m is VM_METHOD_TYPE_ISEQ — a method
// written as Ruby source with `def`. A native (C-function) method, an
// attr_reader/writer and a define_method block body are all other types, which
// refinement_import_methods_i refuses.
func definedWithRubyCode(m *Method) bool {
	return m.iseq != nil && m.native == nil && m.proc == nil && m.attrKind == 0
}

// sortedMethodNames returns tbl's keys in a stable order. MRI's
// rb_id_table_foreach has no defined order; sorting makes which method of a
// module is reported in the "not defined with Ruby code" ArgumentError — and
// how far the import got before raising — reproducible run to run.
func sortedMethodNames(tbl map[string]*Method) []string {
	out := make([]string, 0, len(tbl))
	for k := range tbl {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
