// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"strings"
	"unicode/utf8"

	"github.com/go-embedded-ruby/ruby/internal/object"
	reline "github.com/go-ruby-reline/reline"
)

// registerReline installs the Reline standard library (require "reline"): the
// Reline module of line-editing entry points (readline / readmultiline plus the
// completion-proc, editing-mode and input/output seams) and the Reline::HISTORY
// object.
//
// The deterministic editing state machine — the line buffer, keymap dispatch,
// history, kill-ring, completion and the pure rendering computation — lives
// entirely in the pure-Go github.com/go-ruby-reline/reline library (a faithful
// port of MRI's Reline::LineEditor). Only the terminal I/O is bound here: it is
// the host seam the library keeps abstract. rbgo wires it to its own IO objects
// (StringIO / $stdin / $stdout) through Reline.input=/output=, so the read loop
// reads scripted key bytes from a Ruby IO and writes the rendered line back to
// one — no real tty, and every completion / termination proc runs inline under
// the GVL.
func (vm *VM) registerReline() {
	st := &relineState{history: reline.NewHistory(-1)}
	vm.relineState = st

	mod := newClass("Reline", nil)
	mod.isModule = true
	vm.consts["Reline"] = mod
	mdef := func(name string, fn NativeFn) { mod.smethods[name] = &Method{name: name, owner: mod, native: fn} }

	mdef("readline", func(vm *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		return vm.relineReadline(relinePrompt(args), relineArg(args, 1).Truthy(), false, nil)
	})
	mdef("readmultiline", func(vm *VM, _ object.Value, args []object.Value, blk *Proc) object.Value {
		return vm.relineReadline(relinePrompt(args), relineArg(args, 1).Truthy(), true, blk)
	})
	mdef("completion_proc=", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if p, ok := args[0].(*Proc); ok {
			st.completionProc = p
		} else {
			st.completionProc = nil
		}
		return args[0]
	})
	mdef("completion_proc", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if st.completionProc == nil {
			return object.NilV
		}
		return st.completionProc
	})
	mdef("completion_append_character=", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		if _, isNil := args[0].(object.Nil); isNil {
			st.completionAppendChar = ""
		} else {
			st.completionAppendChar = strArg(args[0])
		}
		return args[0]
	})
	mdef("completion_append_character", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		if st.completionAppendChar == "" {
			return object.NilV
		}
		return object.NewString(st.completionAppendChar)
	})
	mdef("input=", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		st.input = relineIOArg(args[0])
		return args[0]
	})
	mdef("output=", func(_ *VM, _ object.Value, args []object.Value, _ *Proc) object.Value {
		st.output = relineIOArg(args[0])
		return args[0]
	})
	mdef("vi_editing_mode", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		st.viMode = true
		return object.NilV
	})
	mdef("emacs_editing_mode", func(_ *VM, _ object.Value, _ []object.Value, _ *Proc) object.Value {
		st.viMode = false
		return object.NilV
	})

	mod.consts["HISTORY"] = vm.newRelineHistory(mod, st.history)
}

// relineState holds the module-level Reline configuration mutated by the setters
// and read by the readline loop.
type relineState struct {
	history              *reline.History
	input, output        *IOObj // injected Reline.input=/output= streams (nil ⇒ $stdin/$stdout)
	completionProc       *Proc  // Reline.completion_proc (a Ruby callable) or nil
	completionAppendChar string // appended after a unique completion (Reline.completion_append_character)
	viMode               bool   // vi editing mode selected (default emacs)
}

// relinePrompt returns the first argument as the prompt string ("" when absent
// or nil).
func relinePrompt(args []object.Value) string {
	p := relineArg(args, 0)
	if _, isNil := p.(object.Nil); isNil {
		return ""
	}
	return strArg(p)
}

// relineArg returns args[i], or nil when the argument was not supplied.
func relineArg(args []object.Value, i int) object.Value {
	if i < len(args) {
		return args[i]
	}
	return object.NilV
}

// relineIOArg coerces an input=/output= argument to an *IOObj, raising TypeError
// for anything else (Reline drives concrete IO streams).
func relineIOArg(v object.Value) *IOObj {
	o, ok := v.(*IOObj)
	if !ok {
		raise("TypeError", "no implicit conversion of %s into IO", classNameOf(v))
	}
	return o
}

// relineReadline runs one Reline read: it builds a LineEditor over the injected
// (or default) input/output streams, feeds the scripted key bytes decoded from
// the input through the editor, and returns the submitted line as a String — or
// nil at end of input (MRI Reline.readline returns nil on EOF). add_hist appends
// a non-empty submitted line to Reline::HISTORY. In multiline mode termProc (the
// block passed to readmultiline) decides when Enter submits the buffer.
func (vm *VM) relineReadline(prompt string, addHist, multiline bool, termProc *Proc) object.Value {
	st := vm.relineState
	rio := &relineIO{in: st.inputIO(vm), out: st.outputIO(vm), rows: 24, cols: 80}

	cfg := reline.NewConfig()
	if st.viMode {
		cfg.SetEditingMode(reline.ModeViInsert)
	}
	le := reline.NewLineEditor(cfg, rio, st.history)
	// Reset clears the per-read editor state (including the multiline flag and
	// completion-append character), so configure the read AFTER it.
	le.Reset(prompt)
	if multiline {
		le.MultilineOn()
	}
	if st.completionProc != nil {
		p := st.completionProc
		le.SetCompletionProc(func(target, pre, post string) []string {
			return vm.relineCompletion(p, target, pre, post)
		})
	}
	if st.completionAppendChar != "" {
		le.SetCompletionAppendCharacter(st.completionAppendChar)
	}
	if termProc != nil {
		tp := termProc
		le.SetConfirmMultilineTermination(func(buffer string) bool {
			return vm.callBlock(tp, []object.Value{object.NewString(buffer)}).Truthy()
		})
	}

	rio.Write(prompt)
	for !le.Finished() {
		c := rio.GetC(-1)
		if c < 0 {
			le.Update(reline.Key{EOF: true})
			break
		}
		le.Update(relineDecode(rio, c))
	}

	rio.refresh(le)
	if le.EOF() {
		return object.NilV
	}
	line := le.WholeBuffer()
	if addHist && line != "" {
		st.history.Append(line)
	}
	return object.NewString(line)
}

// inputIO / outputIO return the streams the read loop reads keys from and writes
// the rendered line to: the injected Reline.input=/output= object, else the
// current $stdin/$stdout.
func (st *relineState) inputIO(vm *VM) *IOObj {
	if st.input != nil {
		return st.input
	}
	if o, ok := vm.globals["$stdin"].(*IOObj); ok {
		return o
	}
	return vm.consts["STDIN"].(*IOObj)
}

func (st *relineState) outputIO(vm *VM) *IOObj {
	if st.output != nil {
		return st.output
	}
	return vm.curStdout()
}

// relineCompletion bridges the library's CompletionProc to the Ruby completion
// proc, invoking it under the GVL and normalising its result to the candidate
// list. A one-arity proc receives just the target word (MRI's common form); any
// other arity receives (target, preposing, postposing). A non-Array result means
// "no completion" (nil).
func (vm *VM) relineCompletion(p *Proc, target, pre, post string) []string {
	var args []object.Value
	if p.arityVal() == 1 {
		args = []object.Value{object.NewString(target)}
	} else {
		args = []object.Value{object.NewString(target), object.NewString(pre), object.NewString(post)}
	}
	res := vm.callBlock(p, args)
	arr, ok := res.(*object.Array)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr.Elems))
	for _, e := range arr.Elems {
		out = append(out, e.ToS())
	}
	return out
}

// relineIO is the concrete terminal seam (reline.IO) wired to rbgo IO objects:
// keys are read byte-by-byte from the input stream, rendered output is written to
// the output stream. It is the only tty-touching part of the binding; the
// editing core stays pure. In tests both streams are in-memory StringIOs, so a
// full session runs deterministically with no real terminal and no goroutines.
type relineIO struct {
	in, out    *IOObj
	unget      []int
	rows, cols int
}

// GetC returns the next input byte (an ungot byte first), or -1 at end of input.
// timeoutMs is ignored: the scripted input is already fully buffered.
func (r *relineIO) GetC(int) int {
	if n := len(r.unget); n > 0 {
		c := r.unget[n-1]
		r.unget = r.unget[:n-1]
		return c
	}
	r.in.pipeRefresh()
	if r.in.pos >= len(r.in.buf) {
		return -1
	}
	c := int(r.in.buf[r.in.pos])
	r.in.pos++
	return c
}

// UngetC pushes a byte back to be returned by the next GetC.
func (r *relineIO) UngetC(c int) { r.unget = append(r.unget, c) }

// GetScreenSize reports the terminal size the renderer wraps against.
func (r *relineIO) GetScreenSize() (int, int) { return r.rows, r.cols }

// Write emits rendered output to the output stream.
func (r *relineIO) Write(s string) { r.out.writeStr(s) }

// MoveCursorColumn / EraseAfterCursor / ClearScreen are the direct cursor/screen
// operations; they emit the corresponding ANSI control sequences.
func (r *relineIO) MoveCursorColumn(col int) { r.out.writeStr("\x1b[" + itoa(col+1) + "G") }
func (r *relineIO) EraseAfterCursor()        { r.out.writeStr("\x1b[K") }
func (r *relineIO) ClearScreen()             { r.out.writeStr("\x1b[2J") }

// refresh redraws the current line: it lays the buffer out for the terminal
// width (the pure Render computation) and writes the wrapped rows after homing
// the cursor and clearing the old line — the tty half of the editing loop.
func (r *relineIO) refresh(le *reline.LineEditor) {
	rs := le.Render(r.cols, nil)
	r.MoveCursorColumn(0)
	r.EraseAfterCursor()
	var b strings.Builder
	for i, row := range rs.Lines {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(row[0] + row[1])
	}
	r.Write(b.String())
}

// relineCtrl maps the control bytes the loop understands to their MRI emacs
// command symbols (from the library's EMACS_MAPPING). Bytes outside this set are
// dispatched as ed_insert (printable) or ed_ignore.
var relineCtrl = map[byte]string{
	1: "ed_move_to_beg", 2: "ed_prev_char", 4: "em_delete", 5: "ed_move_to_end",
	6: "ed_next_char", 8: "em_delete_prev_char", 9: "complete", 10: "ed_newline",
	11: "ed_kill_line", 12: "ed_clear_screen", 13: "ed_newline",
	14: "ed_next_history", 16: "ed_prev_history", 18: "vi_search_prev",
	20: "ed_transpose_chars", 21: "unix_line_discard", 23: "em_kill_region",
	25: "em_yank", 127: "em_delete_prev_char",
}

// relineCSI maps the final byte of an ESC-[ / ESC-O (CSI / SS3) sequence — the
// arrow and Home/End keys — to its emacs command symbol.
var relineCSI = map[byte]string{
	'A': "ed_prev_history", 'B': "ed_next_history", 'C': "ed_next_char",
	'D': "ed_prev_char", 'H': "ed_move_to_beg", 'F': "ed_move_to_end",
}

// relineDecode turns one input byte (reading any UTF-8 continuation or escape
// bytes that follow) into a decoded key event for the LineEditor.
func relineDecode(r *relineIO, c int) reline.Key {
	switch {
	case c == 27: // ESC: a CSI/SS3 cursor sequence, else ignored
		return r.decodeEscape()
	case c < 0x80:
		if sym, ok := relineCtrl[byte(c)]; ok {
			return reline.Key{Char: string(rune(c)), MethodSymbol: sym}
		}
		if c >= 0x20 && c != 0x7f {
			return reline.Key{Char: string(rune(c)), MethodSymbol: "ed_insert"}
		}
		return reline.Key{Char: string(rune(c)), MethodSymbol: "ed_ignore"}
	default: // 0x80..0xff: a multibyte UTF-8 lead byte
		return r.decodeUTF8(c)
	}
}

// decodeEscape decodes an ESC-prefixed cursor sequence. ESC-[ / ESC-O followed
// by a known final byte becomes the corresponding command; anything else is
// ignored (the peeked byte is pushed back so it is read normally next).
func (r *relineIO) decodeEscape() reline.Key {
	c := r.GetC(-1)
	if c == '[' || c == 'O' {
		f := r.GetC(-1)
		if sym, ok := relineCSI[byte(f)]; ok {
			return reline.Key{Char: "\x1b" + string(rune(c)) + string(rune(f)), MethodSymbol: sym}
		}
		return reline.Key{Char: "\x1b", MethodSymbol: "ed_ignore"}
	}
	if c >= 0 {
		r.UngetC(c)
	}
	return reline.Key{Char: "\x1b", MethodSymbol: "ed_ignore"}
}

// decodeUTF8 reads the continuation bytes of a multibyte rune whose lead byte is
// c and returns it as an ed_insert key; an invalid/truncated sequence is ignored.
func (r *relineIO) decodeUTF8(c int) reline.Key {
	n := utf8ContinuationCount(byte(c))
	b := []byte{byte(c)}
	for i := 0; i < n; i++ {
		nc := r.GetC(-1)
		if nc < 0 {
			break
		}
		b = append(b, byte(nc))
	}
	if utf8.Valid(b) {
		return reline.Key{Char: string(b), MethodSymbol: "ed_insert"}
	}
	return reline.Key{Char: string(b), MethodSymbol: "ed_ignore"}
}

// utf8ContinuationCount returns how many continuation bytes follow the UTF-8 lead
// byte b (0 for an invalid lead).
func utf8ContinuationCount(b byte) int {
	switch {
	case b>>5 == 0b110:
		return 1
	case b>>4 == 0b1110:
		return 2
	case b>>3 == 0b11110:
		return 3
	default:
		return 0
	}
}
