//go:build rbgo_closed

// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

// actionViewInlineRender is unavailable in a closed-world binary: rendering an
// inline ERB template evaluates its compiled Ruby source, which needs the
// front-end (parser + CompileWithLocals) a closed-world build drops from the link
// — the same reason Binding#eval and the Sinatra erb helper raise there. Every
// other ActionView helper (the pure tag/url/form/text/number helpers and the
// SafeBuffer) still works, and a host that wires a render_template callable seam
// (e.g. actionpack) renders without this default path; only the built-in inline
// ERB default raises.
func (vm *VM) actionViewInlineRender(b *ActionViewBase, tmpl string, locals map[string]any) string {
	_, _, _ = b, tmpl, locals
	raise("NotImplementedError", "ActionView inline template rendering is unavailable in a closed-world binary (built with rbgo build --closed, without the front-end)")
	return "" // unreachable: raise panics
}
