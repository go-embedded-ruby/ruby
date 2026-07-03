// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	commonmark "github.com/go-ruby-commonmark/commonmark"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file is the thin binding between rbgo's Ruby object graph and the
// interpreter-independent github.com/go-ruby-commonmark/commonmark renderer. The
// parser and HTML renderer live in that library; rbgo only maps the Markdown
// source String and an options value to a single commonmark.ToHTML call, so the
// commonmarker-faithful HTML the Commonmark module relies on is preserved by
// construction.

// commonmarkRender renders Markdown source to an HTML fragment by mapping the
// options value into a *commonmark.Options and calling commonmark.ToHTML.
func commonmarkRender(vm *VM, src string, opt object.Value) string {
	return commonmark.ToHTML(src, commonmarkOptions(opt))
}

// commonmarkOptions maps a Ruby options value to a *commonmark.Options. A nil /
// Ruby nil value selects strict CommonMark (the library's safe default). A Hash
// maps recognised keys (as Symbol or String) to the corresponding option, a truthy
// value enabling it; the commonmarker extension names (:table, :strikethrough,
// :autolink, :tasklist) are accepted alongside the library's own flag names. An
// Array is read as a list of extension symbols to enable (the commonmarker
// `extensions:` form). Unrecognised keys are ignored.
func commonmarkOptions(opt object.Value) *commonmark.Options {
	o := &commonmark.Options{}
	{
		__sw38 := opt
		switch {
		case __sw38 == nil:
			v := __sw38
			_ = v
			return nil
		case object.IsNilObj(__sw38):
			v := object.NilObj()
			_ = v
			return nil
		case object.IsKind[*object.Hash](__sw38):
			v := object.Kind[*object.Hash](__sw38)
			_ = v
			for _, k := range v.Keys {
				val, _ := v.Get(k)
				commonmarkSetOption(o, commonmarkKey(k), val.Truthy())
			}
		case object.IsKind[*object.Array](__sw38):
			v := object.Kind[*object.Array](__sw38)
			_ = v
			for _, el := range v.Elems {
				commonmarkSetOption(o, commonmarkKey(el), true)
			}
		default:
			v := __sw38
			_ = v
			return nil
		}
	}
	return o
}

// commonmarkSetOption sets the option named key on o to on. The commonmarker
// extension spellings and the library's own Options field names are both accepted
// so a caller can use either vocabulary.
func commonmarkSetOption(o *commonmark.Options, key string, on bool) {
	switch key {
	case "table", "tables":
		o.Tables = on
	case "strikethrough", "strikethroughs":
		o.Strikethrough = on
	case "autolink", "autolinks":
		o.Autolink = on
	case "tasklist", "task_list", "tasklists":
		o.TaskList = on
	case "github_pre_lang", "githubpre_lang":
		o.GitHubPreLang = on
	case "unsafe", "unsafe_":
		o.Unsafe = on
	case "hardbreaks", "hard_breaks":
		o.HardBreaks = on
	}
}

// commonmarkKey renders an option key (a Symbol or String) as its bare name; any
// other value falls back to its to_s.
func commonmarkKey(k object.Value) string {
	{
		__sw39 := k
		switch {
		case object.IsKind[object.Symbol](__sw39):
			n := object.Kind[object.Symbol](__sw39)
			_ = n
			return string(n)
		case object.IsKind[*object.String](__sw39):
			n := object.Kind[*object.String](__sw39)
			_ = n
			return n.Str()
		}
	}
	return k.ToS()
}
