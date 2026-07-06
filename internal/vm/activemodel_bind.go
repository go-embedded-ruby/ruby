// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"fmt"
	"regexp"
	"strings"

	activemodel "github.com/go-ruby-activemodel/activemodel"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// amModel adapts a Ruby object to activemodel.Model — the two seams the library
// leaves to the host. Attribute reads go through the Ruby reader method
// (ActiveModel's read_attribute_for_validation is `send(attr)`), falling back to
// the matching instance variable; writes set the instance variable; Call sends a
// Ruby method (for symbol if:/unless: conditions and numericality method bounds);
// RespondTo is Ruby's respond_to?. Every seam runs inline on the VM goroutine
// under the GVL — no goroutines — so a raise inside a validator block unwinds as
// a normal Ruby exception.
type amModel struct {
	vm   *VM
	inst object.Value
}

// Get reads an attribute: the reader method when the object responds to it, else
// the @attr instance variable.
func (m *amModel) Get(name string) any {
	if m.vm.respondsTo(m.inst, name) {
		return goOfRuby(m.vm.send(m.inst, name, nil, nil))
	}
	return goOfRuby(getIvar(m.inst, "@"+name))
}

// Set writes the @attr instance variable (the Attr write half; ActiveModel's
// validation core never calls it, but the Model interface requires it).
func (m *amModel) Set(name string, val any) {
	setIvar(m.inst, "@"+name, rubyOfGo(val))
}

// Call sends a Ruby method with no arguments and returns its Go-mapped result.
func (m *amModel) Call(method string) any {
	return goOfRuby(m.vm.send(m.inst, method, nil, nil))
}

// RespondTo reports Ruby respond_to?.
func (m *amModel) RespondTo(method string) bool { return m.vm.respondsTo(m.inst, method) }

// amThunk replays one DSL declaration onto a freshly built Validations.
type amThunk func(v *activemodel.Validations)

// amAddThunk appends a validation declaration for a class.
func (vm *VM) amAddThunk(cls *RClass, t amThunk) {
	if vm.amThunks == nil {
		vm.amThunks = map[*RClass][]amThunk{}
	}
	vm.amThunks[cls] = append(vm.amThunks[cls], t)
}

// amNameFor builds the ActiveModel::Name for a class from its name.
func (vm *VM) amNameFor(cls *RClass) activemodel.Name {
	return activemodel.NewName(cls.name)
}

// amBuildValidations assembles the Validations for an instance's class: it replays
// every ancestor's thunks parent-first, so a subclass inherits the validators
// declared on its superclasses (ActiveModel's class-inheritable validators).
func (vm *VM) amBuildValidations(cls *RClass) *activemodel.Validations {
	v := activemodel.New(vm.amNameFor(cls))
	var chain []*RClass
	for c := cls; c != nil; c = c.super {
		chain = append(chain, c)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		for _, t := range vm.amThunks[chain[i]] {
			t(v)
		}
	}
	return v
}

// amValidate runs validation for an instance in the given context, stashing the
// resulting Errors on @__am_errors (so #errors returns the last run's errors) and
// reporting validity.
func (vm *VM) amValidate(inst object.Value, context string) (bool, *ASModelErrors) {
	cls := vm.classOf(inst)
	v := vm.amBuildValidations(cls)
	model := &amModel{vm: vm, inst: inst}
	ok, e := v.ValidContext(model, context)
	h := &ASModelErrors{e: e, model: model}
	setIvar(inst, "@__am_errors", h)
	return ok, h
}

// amErrorsFor returns the instance's memoized ActiveModel::Errors, creating an
// empty one bound to the instance on first access (Rails memoizes #errors).
func (vm *VM) amErrorsFor(inst object.Value) *ASModelErrors {
	if e, ok := getIvar(inst, "@__am_errors").(*ASModelErrors); ok {
		return e
	}
	model := &amModel{vm: vm, inst: inst}
	e := &ASModelErrors{e: activemodel.NewErrors(model, vm.amNameFor(vm.classOf(inst))), model: model}
	setIvar(inst, "@__am_errors", e)
	return e
}

// goOfRuby maps a Ruby value into the Go value model ActiveModel's validators and
// blank?/present? checks consume (nil / bool / int64 / float64 / string / []any /
// map[any]any), with a Symbol surfaced as its name and any other value as its
// to_s.
func goOfRuby(v object.Value) any {
	switch t := v.(type) {
	case nil, object.Nil:
		return nil
	case object.Bool:
		return bool(t)
	case object.Integer:
		return int64(t)
	case object.Float:
		return float64(t)
	case *object.String:
		return t.Str()
	case object.Symbol:
		return string(t)
	case *object.Array:
		out := make([]any, len(t.Elems))
		for i, e := range t.Elems {
			out[i] = goOfRuby(e)
		}
		return out
	case *object.Hash:
		m := make(map[any]any, len(t.Keys))
		for _, k := range t.Keys {
			val, _ := t.Get(k)
			m[goOfRuby(k)] = goOfRuby(val)
		}
		return m
	}
	return v.ToS()
}

// rubyOfGo maps a Go value the library hands back (an error type, option value or
// interpolation binding) into a Ruby value: an ActiveModel Symbol becomes a Ruby
// Symbol, and int / int64 / float / string / bool / slice / map map to their Ruby
// counterparts; anything else renders through fmt.Sprint.
func rubyOfGo(v any) object.Value {
	switch t := v.(type) {
	case nil:
		return object.NilV
	case bool:
		return object.Bool(t)
	case int:
		return object.IntValue(int64(t))
	case int64:
		return object.IntValue(t)
	case float64:
		return object.Float(t)
	case string:
		return object.NewString(t)
	case activemodel.Symbol:
		return object.Symbol(string(t))
	case []any:
		arr := object.NewArrayFromSlice(make([]object.Value, len(t)))
		for i, e := range t {
			arr.Elems[i] = rubyOfGo(e)
		}
		return arr
	case map[any]any:
		h := object.NewHash()
		for k, val := range t {
			h.Set(rubyOfGo(k), rubyOfGo(val))
		}
		return h
	}
	return object.NewString(fmt.Sprint(v))
}

// amHashGet fetches a symbol-keyed option (validates :x, presence: true stores
// the option under the Symbol :presence).
func amHashGet(h *object.Hash, key string) (object.Value, bool) {
	return h.Get(object.Symbol(key))
}

// amAttrsAndOpts splits a DSL argument list into the attribute (or method) names
// and the trailing options Hash.
func amAttrsAndOpts(args []object.Value) ([]string, *object.Hash) {
	var attrs []string
	var opts *object.Hash
	for _, a := range args {
		if h, ok := a.(*object.Hash); ok {
			opts = h
			continue
		}
		attrs = append(attrs, arStr(a))
	}
	return attrs, opts
}

// amMessage converts a :message option: a Ruby String is a literal message, a
// Symbol names a message key, a Proc is a lambda message, and nil means none.
func amMessage(v object.Value) any {
	switch m := v.(type) {
	case *object.String:
		return m.Str()
	case object.Symbol:
		return activemodel.Symbol(string(m))
	case *Proc:
		p := m
		return func(mod activemodel.Model) string {
			rm := mod.(*amModel)
			return rm.vm.callBlockSelf(p, rm.inst, nil).ToS()
		}
	}
	return nil
}

// amMessageOpt reads the :message option from an options Hash, or nil.
func amMessageOpt(h *object.Hash) any {
	if v, ok := amHashGet(h, "message"); ok {
		return amMessage(v)
	}
	return nil
}

// amCondition builds a single if:/unless: predicate: a Symbol/String names a model
// method, a Proc is evaluated against the model instance.
func (vm *VM) amCondition(v object.Value) activemodel.Condition {
	switch t := v.(type) {
	case object.Symbol:
		return activemodel.MethodCond(string(t))
	case *object.String:
		return activemodel.MethodCond(t.Str())
	case *Proc:
		p := t
		return activemodel.ProcCond(func(m activemodel.Model) any {
			rm := m.(*amModel)
			return goOfRuby(rm.vm.callBlockSelf(p, rm.inst, nil))
		})
	}
	raise("ArgumentError", "if/unless must be a symbol, string or proc")
	return activemodel.Condition{}
}

// amConditions builds the list of predicates for an if:/unless: option (a single
// value or an Array of them).
func (vm *VM) amConditions(v object.Value) []activemodel.Condition {
	if arr, ok := v.(*object.Array); ok {
		out := make([]activemodel.Condition, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = vm.amCondition(e)
		}
		return out
	}
	return []activemodel.Condition{vm.amCondition(v)}
}

// amOn builds the on: context list (a single context or an Array of them).
func amOn(v object.Value) []string {
	if arr, ok := v.(*object.Array); ok {
		out := make([]string, len(arr.Elems))
		for i, e := range arr.Elems {
			out[i] = arStr(e)
		}
		return out
	}
	return []string{arStr(v)}
}

// amBuildConditions reads the shared control keys (if/unless/on/allow_nil/
// allow_blank) from an options Hash into a Conditions block.
func (vm *VM) amBuildConditions(h *object.Hash) activemodel.Conditions {
	c := activemodel.Conditions{}
	if h == nil {
		return c
	}
	if v, ok := amHashGet(h, "if"); ok {
		c.If = vm.amConditions(v)
	}
	if v, ok := amHashGet(h, "unless"); ok {
		c.Unless = vm.amConditions(v)
	}
	if v, ok := amHashGet(h, "on"); ok {
		c.On = amOn(v)
	}
	if v, ok := amHashGet(h, "allow_nil"); ok {
		c.AllowNil = v.Truthy()
	}
	if v, ok := amHashGet(h, "allow_blank"); ok {
		c.AllowBlank = v.Truthy()
	}
	return c
}

// amBuildOptions reads a full `validates` options Hash into activemodel.Options:
// the shared control keys plus each configured standard validator.
func (vm *VM) amBuildOptions(h *object.Hash) activemodel.Options {
	o := activemodel.Options{}
	if h == nil {
		return o
	}
	c := vm.amBuildConditions(h)
	o.If, o.Unless, o.On, o.AllowNil, o.AllowBlank = c.If, c.Unless, c.On, c.AllowNil, c.AllowBlank
	o.Message = amMessageOpt(h)

	if v, ok := amHashGet(h, "presence"); ok {
		o.Presence = v.Truthy()
	}
	if v, ok := amHashGet(h, "absence"); ok {
		o.Absence = v.Truthy()
	}
	if v, ok := amHashGet(h, "length"); ok {
		o.Length = vm.amLengthOptions(v)
	}
	if v, ok := amHashGet(h, "format"); ok {
		o.Format = vm.amFormatOptions(v)
	}
	if v, ok := amHashGet(h, "inclusion"); ok {
		o.Inclusion = vm.amMembershipOptions(v)
	}
	if v, ok := amHashGet(h, "exclusion"); ok {
		o.Exclusion = vm.amMembershipOptions(v)
	}
	if v, ok := amHashGet(h, "numericality"); ok {
		o.Numericality = vm.amNumericalityOptions(v)
	}
	if v, ok := amHashGet(h, "confirmation"); ok {
		o.Confirmation = amConfirmationOptions(v)
	}
	if v, ok := amHashGet(h, "acceptance"); ok {
		o.Acceptance = amAcceptanceOptions(v)
	}
	return o
}

// amIntPtr reads an Integer option into a *int, or nil.
func amIntPtr(v object.Value) *int {
	if n, ok := v.(object.Integer); ok {
		i := int(n)
		return &i
	}
	return nil
}

// amIntRange converts a Ruby Range of Integers into an inclusive length Range.
func amIntRange(v object.Value) *activemodel.Range {
	r, ok := v.(*object.Range)
	if !ok {
		raise("ArgumentError", "length in:/within: must be a range")
	}
	lo, _ := r.Lo.(object.Integer)
	hi, _ := r.Hi.(object.Integer)
	max := int(hi)
	if r.Exclusive {
		max--
	}
	return &activemodel.Range{Min: int(lo), Max: max}
}

// amLengthOptions reads a length: options Hash into LengthOptions.
func (vm *VM) amLengthOptions(v object.Value) *activemodel.LengthOptions {
	h, ok := v.(*object.Hash)
	if !ok {
		raise("ArgumentError", "length: must be given a hash of options")
	}
	o := &activemodel.LengthOptions{Message: amMessageOpt(h)}
	if x, ok := amHashGet(h, "minimum"); ok {
		o.Minimum = amIntPtr(x)
	}
	if x, ok := amHashGet(h, "maximum"); ok {
		o.Maximum = amIntPtr(x)
	}
	if x, ok := amHashGet(h, "is"); ok {
		o.Is = amIntPtr(x)
	}
	if x, ok := amHashGet(h, "in"); ok {
		o.In = amIntRange(x)
	}
	if x, ok := amHashGet(h, "within"); ok {
		o.In = amIntRange(x)
	}
	if x, ok := amHashGet(h, "too_long"); ok {
		o.TooLong = amMessage(x)
	}
	if x, ok := amHashGet(h, "too_short"); ok {
		o.TooShort = amMessage(x)
	}
	if x, ok := amHashGet(h, "wrong_length"); ok {
		o.WrongLength = amMessage(x)
	}
	return o
}

// amGoRegexp compiles a Ruby Regexp value into a Go *regexp.Regexp, translating
// Ruby's m (dot-all) / i / x flags. It raises when the value is not a Regexp or
// the pattern cannot be compiled by Go's engine.
func amGoRegexp(v object.Value) *regexp.Regexp {
	re, ok := v.(*Regexp)
	if !ok {
		raise("ArgumentError", "format with:/without: must be a regexp")
	}
	var goFlags string
	extended := false
	for _, f := range re.flags {
		switch f {
		case 'i':
			goFlags += "i"
		case 'm':
			goFlags += "s" // Ruby /m is dot-all, which Go spells (?s)
		case 'x':
			extended = true // Go's RE2 has no free-spacing flag; strip it out below.
		}
	}
	src := re.source
	if extended {
		src = amStripExtended(src)
	}
	if goFlags != "" {
		src = "(?" + goFlags + ")" + src
	}
	compiled, err := regexp.Compile(src)
	if err != nil {
		raise("RegexpError", "%s", err.Error())
	}
	return compiled
}

// amStripExtended lowers a Ruby /x (extended / free-spacing) pattern to a plain
// one Go's RE2 accepts: unescaped whitespace and #-to-end-of-line comments are
// removed, while escaped whitespace and whitespace inside a character class are
// preserved.
func amStripExtended(src string) string {
	var b strings.Builder
	inClass, escaped, inComment := false, false, false
	for _, r := range src {
		switch {
		case inComment:
			if r == '\n' {
				inComment = false
			}
		case escaped:
			b.WriteByte('\\')
			b.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case inClass:
			if r == ']' {
				inClass = false
			}
			b.WriteRune(r)
		case r == '[':
			inClass = true
			b.WriteRune(r)
		case r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v':
			// free-spacing: drop unescaped whitespace
		case r == '#':
			inComment = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// amFormatOptions reads a format: option, accepting either a bare Regexp (the
// with: shorthand) or a Hash of with:/without:/message:.
func (vm *VM) amFormatOptions(v object.Value) *activemodel.FormatOptions {
	o := &activemodel.FormatOptions{}
	if h, ok := v.(*object.Hash); ok {
		if x, ok := amHashGet(h, "with"); ok {
			o.With = amGoRegexp(x)
		}
		if x, ok := amHashGet(h, "without"); ok {
			o.Without = amGoRegexp(x)
		}
		o.Message = amMessageOpt(h)
		return o
	}
	o.With = amGoRegexp(v)
	return o
}

// amSlice maps a Ruby Array into a []any allowed/forbidden set.
func amSlice(arr *object.Array) []any {
	out := make([]any, len(arr.Elems))
	for i, e := range arr.Elems {
		out[i] = goOfRuby(e)
	}
	return out
}

// amNumRange converts a Ruby Range into a numeric NumRange for inclusion/exclusion.
func amNumRange(r *object.Range) *activemodel.NumRange {
	lo, _ := goFloat(r.Lo)
	hi, _ := goFloat(r.Hi)
	return &activemodel.NumRange{Min: lo, Max: hi, ExcludeEnd: r.Exclusive}
}

// goFloat coerces a Ruby numeric to float64.
func goFloat(v object.Value) (float64, bool) {
	switch n := v.(type) {
	case object.Integer:
		return float64(n), true
	case object.Float:
		return float64(n), true
	}
	return 0, false
}

// amMembershipOptions reads an inclusion:/exclusion: Hash into MembershipOptions,
// whose in: is either a discrete Array set or a numeric Range.
func (vm *VM) amMembershipOptions(v object.Value) *activemodel.MembershipOptions {
	h, ok := v.(*object.Hash)
	if !ok {
		raise("ArgumentError", "inclusion/exclusion must be given a hash of options")
	}
	o := &activemodel.MembershipOptions{Message: amMessageOpt(h)}
	in, ok := amHashGet(h, "in")
	if !ok {
		in, ok = amHashGet(h, "within")
	}
	if ok {
		switch t := in.(type) {
		case *object.Array:
			o.In = amSlice(t)
		case *object.Range:
			o.Range = amNumRange(t)
		}
	}
	return o
}

// amBound reads a numericality comparison bound: a number is a literal, a Symbol a
// method name, a Proc a lambda, all resolved against the model at check time.
func (vm *VM) amBound(v object.Value) any {
	switch t := v.(type) {
	case object.Integer:
		return int64(t)
	case object.Float:
		return float64(t)
	case object.Symbol:
		return activemodel.Symbol(string(t))
	case *Proc:
		p := t
		return func(m activemodel.Model) any {
			rm := m.(*amModel)
			return goOfRuby(rm.vm.callBlockSelf(p, rm.inst, nil))
		}
	}
	return goOfRuby(v)
}

// amNumericalityOptions reads a numericality: option (true, or a Hash of the
// comparison / parity keys) into NumericalityOptions.
func (vm *VM) amNumericalityOptions(v object.Value) *activemodel.NumericalityOptions {
	o := &activemodel.NumericalityOptions{}
	h, ok := v.(*object.Hash)
	if !ok {
		return o
	}
	o.Message = amMessageOpt(h)
	if x, ok := amHashGet(h, "only_integer"); ok {
		o.OnlyInteger = x.Truthy()
	}
	if x, ok := amHashGet(h, "odd"); ok {
		o.Odd = x.Truthy()
	}
	if x, ok := amHashGet(h, "even"); ok {
		o.Even = x.Truthy()
	}
	for _, b := range []struct {
		key string
		set func(any)
	}{
		{"greater_than", func(a any) { o.GreaterThan = a }},
		{"greater_than_or_equal_to", func(a any) { o.GreaterThanOrEqualTo = a }},
		{"equal_to", func(a any) { o.EqualTo = a }},
		{"less_than", func(a any) { o.LessThan = a }},
		{"less_than_or_equal_to", func(a any) { o.LessThanOrEqualTo = a }},
		{"other_than", func(a any) { o.OtherThan = a }},
	} {
		if x, ok := amHashGet(h, b.key); ok {
			b.set(vm.amBound(x))
		}
	}
	return o
}

// amConfirmationOptions reads a confirmation: option into ConfirmationOptions.
func amConfirmationOptions(v object.Value) *activemodel.ConfirmationOptions {
	o := &activemodel.ConfirmationOptions{}
	if h, ok := v.(*object.Hash); ok {
		if x, ok := amHashGet(h, "case_sensitive"); ok {
			b := x.Truthy()
			o.CaseSensitive = &b
		}
		o.Message = amMessageOpt(h)
	}
	return o
}

// amAcceptanceOptions reads an acceptance: option into AcceptanceOptions.
func amAcceptanceOptions(v object.Value) *activemodel.AcceptanceOptions {
	o := &activemodel.AcceptanceOptions{}
	if h, ok := v.(*object.Hash); ok {
		if x, ok := amHashGet(h, "accept"); ok {
			if arr, ok := x.(*object.Array); ok {
				o.Accept = amSlice(arr)
			} else {
				o.Accept = []any{goOfRuby(x)}
			}
		}
		o.Message = amMessageOpt(h)
	}
	return o
}

// amErrorType converts an Errors#add / #added? type argument: a Symbol is a
// message-table key, a String a literal message, a Proc a lambda, nil the default
// (:invalid).
func (vm *VM) amErrorType(v object.Value) any {
	switch t := v.(type) {
	case nil, object.Nil:
		return nil
	case object.Symbol:
		return activemodel.Symbol(string(t))
	case *object.String:
		return t.Str()
	case *Proc:
		p := t
		return func(m activemodel.Model) any {
			rm := m.(*amModel)
			// Re-map so a proc that returns a Symbol/String keeps its type.
			return rm.vm.amErrorType(rm.vm.callBlockSelf(p, rm.inst, nil))
		}
	}
	return goOfRuby(v)
}

// amTail returns args[n:], or nil when there are fewer than n arguments (so a
// trailing-options scan never slices out of range).
func amTail(args []object.Value, n int) []object.Value {
	if len(args) < n {
		return nil
	}
	return args[n:]
}

// amOptsMap converts an Errors#add / #where / #added? options Hash into the Go
// option map the library expects, keeping :message as a literal/symbol message and
// mapping every other value through goOfRuby.
func amOptsMap(args []object.Value) map[string]any {
	if len(args) == 0 {
		return nil
	}
	h, ok := args[len(args)-1].(*object.Hash)
	if !ok {
		return nil
	}
	out := map[string]any{}
	for _, k := range h.Keys {
		v, _ := h.Get(k)
		key := arStr(k)
		if key == "message" {
			out[key] = amMessage(v)
			continue
		}
		out[key] = goOfRuby(v)
	}
	return out
}

// amSymArray renders a []string as a Ruby Array of Symbols (attribute_names).
func amSymArray(ss []string) *object.Array {
	arr := object.NewArrayFromSlice(make([]object.Value, len(ss)))
	for i, s := range ss {
		arr.Elems[i] = object.Symbol(s)
	}
	return arr
}

// amClassName renders a Class-or-name argument (a Class, String or Symbol) as a
// class-name string, used by ActiveModel::Name / Naming.
func (vm *VM) amClassName(v object.Value) string {
	if c, ok := v.(*RClass); ok {
		return c.name
	}
	return arStr(v)
}

// amNameOf derives the ActiveModel::Name for a record or class: its #model_name
// when it responds to one, else a Name built from the class (or given) name.
func (vm *VM) amNameOf(v object.Value) activemodel.Name {
	if vm.respondsTo(v, "model_name") {
		if mn, ok := vm.send(v, "model_name", nil, nil).(*ASModelName); ok {
			return mn.n
		}
	}
	if c, ok := v.(*RClass); ok {
		return activemodel.NewName(c.name)
	}
	return activemodel.NewName(vm.classOf(v).name)
}

// amRubyValidator adapts a Ruby ActiveModel::Validator subclass to the library's
// whole-record Validator: on each run it instantiates the validator class with its
// options and sends it #validate(record), the record's #errors wired to the live
// Errors so the Ruby validator's `record.errors.add …` lands in this run.
type amRubyValidator struct {
	vm   *VM
	cls  *RClass
	opts object.Value
}

func (rv *amRubyValidator) Validate(m activemodel.Model, e *activemodel.Errors) {
	rm := m.(*amModel)
	setIvar(rm.inst, "@__am_errors", &ASModelErrors{e: e, model: rm})
	var newArgs []object.Value
	if !object.IsNil(rv.opts) {
		newArgs = []object.Value{rv.opts}
	}
	inst := rv.vm.send(rv.cls, "new", newArgs, nil)
	rv.vm.send(inst, "validate", []object.Value{rm.inst}, nil)
}

// amExposeErrors points the instance's #errors at the live Errors for the duration
// of a Ruby validator block, so `errors.add` inside it appends to this run.
func (vm *VM) amExposeErrors(rm *amModel, e *activemodel.Errors) {
	setIvar(rm.inst, "@__am_errors", &ASModelErrors{e: e, model: rm})
}
