// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ⛔⛔ THE DOORS ARE DERIVED HERE, NOT LISTED ANYWHERE.
//
// `Klass.open { |x| … }` in Ruby yields, closes however the block leaves, and
// answers with the BLOCK's value. Writing `smethods["open"] = smethods["new"]`
// gives none of that: `new` has no block parameter, so the block is discarded in
// silence and the call still looks like it worked. That one line cost #916 its
// 27 hung `library/net-http` files — the fixture shuts its server down from
// inside `TCPSocket.open { … }`, and with the socket still open the server's
// `gets` never returns.
//
// ⛔ AND THE LIST OF DOORS IS WHAT KEPT DRIFTING. #931 wrote the socket doors out
// in prose and the prose had already lost `UNIXServer`; #939 re-derived them from
// the source, fixed three, and found a FOURTH nobody had written down
// (`Resolv::DNS`, outside the socket family altogether). A hand-written list of
// doors is the same artefact as any other hand-written list — see
// a-hand-written-container-list-drifts-from-the-parser — so this test does not
// hold one. It parses the package and finds every `smethods["open"]` assignment
// there is, on EVERY platform (go/parser ignores build tags, which is how the
// windows-only UNIX doors get counted), and then requires each to be accounted
// for.
//
// What it refuses outright is the ALIAS form, because no configuration of it can
// be correct. Everything else must appear in testdata/open-doors.txt with a
// reason, so a NEW `open` goes red until somebody says which kind it is.
func TestEveryOpenDoorIsAccountedFor(t *testing.T) {
	doors := deriveOpenDoors(t)
	if len(doors) < 15 {
		// A derivation that silently matches nothing reports a clean package. The
		// floor is the population measured on 2026-11-15 (18) minus room to remove
		// a few; a real drop below it means the matcher stopped matching.
		t.Fatalf("only %d `open` doors derived from the package: the matcher is broken, not the package", len(doors))
	}

	accounted := readOpenDoorTable(t)
	var unlisted, aliases []string
	seen := map[string]bool{}
	for _, d := range doors {
		seen[d.key] = true
		if d.kind == openKindAlias {
			aliases = append(aliases, fmt.Sprintf("%s (aliased to %s)", d.key, d.detail))
			continue
		}
		if d.kind == openKindBlockHelper || d.kind == openKindUnsupported {
			continue // correct by construction; see the kind's own comment below
		}
		if _, ok := accounted[d.key]; !ok {
			unlisted = append(unlisted, fmt.Sprintf("%s (%s)", d.key, d.detail))
		}
	}

	if len(aliases) > 0 {
		t.Errorf(`⛔ %d `+"`open`"+` door(s) are an ALIAS for another method:

    %s

An alias cannot honour a block: the aliased method has no block parameter, so
`+"`Klass.open { |x| … }`"+` discards the block and closes nothing. Route it
through socketOpenWithBlock(cls, <the new fn>) instead — and check that the
class's own `+"`close`"+` does not raise, because that helper closes through it.`,
			len(aliases), strings.Join(aliases, "\n    "))
	}

	if len(unlisted) > 0 {
		t.Errorf(`⛔ %d `+"`open`"+` door(s) are neither socketOpenWithBlock nor listed in
testdata/open-doors.txt:

    %s

Each inline handler has to say what it does with its block. Add a line
`+"`<file>:<receiver> <reason>`"+` to that file — and the reason has to name the
test that proves it, or say plainly that none does, because an unverified door
that is merely WRITTEN DOWN is the artefact this test exists to replace.`,
			len(unlisted), strings.Join(unlisted, "\n    "))
	}

	// The table may not keep rows for doors that no longer exist: a stale row is
	// how a list stops describing the code.
	var stale []string
	for key := range accounted {
		if !seen[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("testdata/open-doors.txt accounts for %d door(s) that are no longer in the package: %s",
			len(stale), strings.Join(stale, ", "))
	}
}

type openKind int

const (
	// openKindBlockHelper: the native is socketOpenWithBlock(cls, newFn), which
	// yields, closes through the class's own #close and answers the block's
	// value. Correct by construction, and the behaviour is pinned separately by
	// TestUnixAndUDPOpenHonourTheirBlock and the net-http fixtures of #916.
	openKindBlockHelper openKind = iota
	// openKindUnsupported: the native is unsupported("…"), which raises. There is
	// no block semantics to get wrong, and the platform that has the class is
	// covered by openKindBlockHelper in its own file.
	openKindUnsupported
	// openKindAlias: `smethods["open"] = <something>.smethods[…]`. Refused.
	openKindAlias
	// openKindInline: a func literal, or any other expression. Must be listed.
	openKindInline
)

type openDoor struct {
	key    string // "<file>:<receiver>", stable across edits in a way a line number is not
	kind   openKind
	detail string
}

// deriveOpenDoors parses every non-test Go file of this package and returns one
// entry per `….smethods["open"] = …` assignment.
func deriveOpenDoors(t *testing.T) []openDoor {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	var doors []openDoor
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// ParseDir/ParseFile ignores build constraints, which is the point: the
		// windows-only UNIX doors are doors too, and the prose list that missed
		// UNIXServer missed it in exactly such a file.
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, lhs := range as.Lhs {
				recv, isOpen := openSMethodTarget(lhs)
				if !isOpen || i >= len(as.Rhs) {
					continue
				}
				kind, detail := classifyOpenRHS(as.Rhs[i])
				doors = append(doors, openDoor{key: name + ":" + recv, kind: kind, detail: detail})
			}
			return true
		})
	}
	sort.Slice(doors, func(i, j int) bool { return doors[i].key < doors[j].key })
	return doors
}

// openSMethodTarget recognises `<recv>.smethods["open"]` and answers the
// receiver's source text.
func openSMethodTarget(e ast.Expr) (string, bool) {
	ix, ok := e.(*ast.IndexExpr)
	if !ok {
		return "", false
	}
	lit, ok := ix.Index.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	if s, err := strconv.Unquote(lit.Value); err != nil || s != "open" {
		return "", false
	}
	sel, ok := ix.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "smethods" {
		return "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}

func classifyOpenRHS(e ast.Expr) (openKind, string) {
	// `x.smethods["open"] = y.smethods["new"]` — the shape that cost #916 its 27 files.
	if ix, ok := e.(*ast.IndexExpr); ok {
		if sel, ok := ix.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "smethods" {
			return openKindAlias, exprText(ix)
		}
	}
	native := methodNative(e)
	if native == nil {
		return openKindInline, "no `native:` field found"
	}
	if call, ok := native.(*ast.CallExpr); ok {
		if id, ok := call.Fun.(*ast.Ident); ok {
			switch id.Name {
			case "socketOpenWithBlock":
				return openKindBlockHelper, "socketOpenWithBlock"
			case "unsupported":
				return openKindUnsupported, "unsupported(…)"
			}
		}
	}
	if _, ok := native.(*ast.FuncLit); ok {
		return openKindInline, "inline func literal"
	}
	return openKindInline, exprText(native)
}

// methodNative digs the `native:` value out of `&Method{…}`.
func methodNative(e ast.Expr) ast.Expr {
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "native" {
			return kv.Value
		}
	}
	return nil
}

func exprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprText(v.X) + "." + v.Sel.Name
	case *ast.IndexExpr:
		return exprText(v.X) + "[…]"
	case *ast.CallExpr:
		return exprText(v.Fun) + "(…)"
	default:
		return fmt.Sprintf("%T", e)
	}
}

// readOpenDoorTable reads testdata/open-doors.txt: blank lines and `#` comments
// ignored, otherwise `<key> <reason>`.
func readOpenDoorTable(t *testing.T) map[string]string {
	t.Helper()
	p := filepath.Join("testdata", "open-doors.txt")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	out := map[string]string{}
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, reason, ok := strings.Cut(line, " ")
		if !ok || strings.TrimSpace(reason) == "" {
			t.Fatalf("%s:%d: %q has no reason after the key; a row without one accounts for nothing", p, i+1, line)
		}
		if _, dup := out[key]; dup {
			t.Fatalf("%s:%d: %q is listed twice", p, i+1, key)
		}
		out[key] = strings.TrimSpace(reason)
	}
	return out
}

// ⛔ THE CONTROL. Without it, every assertion above would pass against a
// classifier that answers "block helper" for everything: the test would be green
// and the gate would be a decoration. This feeds the classifier the four shapes
// directly and checks it tells them apart — the alias one above all, since that
// is the only verdict that fails a build.
func TestTheOpenDoorClassifierTellsTheShapesApart(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want openKind
	}{
		{"alias", `package p
func f() { a.smethods["open"] = a.smethods["new"] }`, openKindAlias},
		{"helper", `package p
func f() { a.smethods["open"] = &Method{name: "open", owner: a, native: socketOpenWithBlock(a, newFn)} }`, openKindBlockHelper},
		{"unsupported", `package p
func f() { a.smethods["open"] = &Method{name: "open", owner: a, native: unsupported("X")} }`, openKindUnsupported},
		{"inline", `package p
func f() { a.smethods["open"] = &Method{name: "open", owner: a, native: func() {}} }`, openKindInline},
		{"bare_ident", `package p
func f() { a.smethods["open"] = &Method{name: "open", owner: a, native: open} }`, openKindInline},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := parser.ParseFile(token.NewFileSet(), "x.go", tc.src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			var got []openKind
			ast.Inspect(f, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				if _, isOpen := openSMethodTarget(as.Lhs[0]); isOpen {
					k, _ := classifyOpenRHS(as.Rhs[0])
					got = append(got, k)
				}
				return true
			})
			if len(got) != 1 {
				t.Fatalf("matched %d assignments, want 1", len(got))
			}
			if got[0] != tc.want {
				t.Errorf("classified as %d, want %d", got[0], tc.want)
			}
		})
	}

	// And the matcher must NOT fire on a different method name, or the "derived
	// at least 15" floor could be met by noise.
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", `package p
func f() { a.smethods["new"] = b; a.methods["open"] = c }`, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			if recv, isOpen := openSMethodTarget(lhs); isOpen {
				t.Errorf("matched %s, which is not an smethods[\"open\"] assignment", recv)
			}
		}
		return true
	})
}

// Resolv::DNS was the fourth door — found by #939's re-derivation, outside the
// socket family, and filed rather than fixed. Measured against MRI 4.0.7:
//
//	Resolv::DNS.open { |dns| :from_block }
//	  MRI   yields, returns :from_block
//	  rbgo  did NOT yield, returned #<Resolv::DNS…>
//	Resolv::DNS#close
//	  MRI   nil, and a second close is fine
//	  rbgo  NotImplementedError, both times
//
// ⛔ The two halves are one fix: socketOpenWithBlock closes through the class's
// own `close`, so leaving `close` on the NotImplementedError list would have
// turned every `Resolv::DNS.open { … }` into a NotImplementedError raised from
// the ensure — a block that now runs, and a program that now fails.
func TestResolvDNSOpenYieldsAndCloses(t *testing.T) {
	got := runSrc(t, `
require "resolv"
r = Resolv::DNS.open { |dns| puts "yielded #{dns.class}"; :from_block }
p r
d = Resolv::DNS.open
p d.class
p d.close
d.close
puts "second close ok"
`)
	want := strings.Join([]string{
		"yielded Resolv::DNS",
		":from_block",
		"Resolv::DNS",
		"nil",
		"second close ok",
	}, "\n")
	if got != want {
		t.Errorf("Resolv::DNS.open:\ngot\n%s\nwant\n%s", got, want)
	}

	// ⛔ `close` takes NO arguments, and this is the half the pre-existing
	// TestResolvDNS could not see. It asserted that `close("x")` raises, and it
	// did — with NotImplementedError, because the stub raised for EVERY call. The
	// right verdict for the wrong reason: the test passed identically whether
	// `close` rejected an argument or did nothing at all. Measured against MRI
	// 4.0.7 and asserted here on the MESSAGE and the arity, not merely on "an
	// error happened".
	got = runSrc(t, `
require "resolv"
d = Resolv::DNS.new
begin
  d.close("x")
  puts "no raise"
rescue ArgumentError => e
  puts e.message
end
p d.method(:close).arity
`)
	wantArgs := "wrong number of arguments (given 1, expected 0)\n0"
	if got != wantArgs {
		t.Errorf("Resolv::DNS#close arity:\ngot\n%s\nwant\n%s", got, wantArgs)
	}

	// The query methods must still raise: `close` came off that list, and nothing
	// else may have followed it.
	for _, m := range []string{"getaddress", "getresource", "each_name"} {
		out := runSrc(t, fmt.Sprintf(`
require "resolv"
begin
  Resolv::DNS.new.%s("example.com")
  puts "no raise"
rescue NotImplementedError => e
  puts e.class
end
`, m))
		if out != "NotImplementedError" {
			t.Errorf("Resolv::DNS#%s should still raise NotImplementedError, got %q", m, out)
		}
	}
}

// guard against a future edit quietly dropping the socket doors from the helper:
// the six of them are what #932 and #939 landed, and the count is derived.
func TestTheSocketOpenDoorsAllUseTheBlockHelper(t *testing.T) {
	var helper, other []string
	for _, d := range deriveOpenDoors(t) {
		if !strings.HasPrefix(d.key, "socket") {
			continue
		}
		if d.kind == openKindBlockHelper {
			helper = append(helper, d.key)
		} else if d.kind != openKindUnsupported {
			other = append(other, d.key+" ("+d.detail+")")
		}
	}
	if len(other) > 0 {
		t.Errorf("socket `open` doors not using socketOpenWithBlock: %s", strings.Join(other, ", "))
	}
	if len(helper) < 6 {
		t.Errorf("only %d socket `open` doors go through the helper (%s); #932 and #939 landed six",
			len(helper), strings.Join(helper, ", "))
	}
	if slices.Contains(helper, "socket_unix.go:srv") == false {
		t.Error("UNIXServer's door is missing from the helper — the one #931's prose list had already lost")
	}
}
