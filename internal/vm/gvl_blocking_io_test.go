// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !rbgo_core

package vm

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ⛔ THE GATE FOR A MISTAKE THIS REPOSITORY MADE EIGHT TIMES IN ONE WEEK:
// a mechanism that exists and is wired at only some of its doors.
//
// rbgo emulates a GVL — one Ruby thread runs VM code at a time (vm.go), and a
// thread that blocks in a native call MUST release it, which is what ioBlock
// does. Net::HTTP has done that at twenty sites since it was written.
// Net::FTP did it at NONE, so every one of its reads held the GVL across a
// blocking read(2).
//
// The cost was not a slow program, it was a DEADLOCK. A Ruby program that
// serves itself — a spec fixture, a test that binds a socket and connects to
// it, an embedder running a listener beside its own client — has its server
// thread return from Accept and then park on a lock the client is sitting on.
// Measured by dose-response: with a 2 s client deadline the accept completed at
// 2.308 s, with a 5 s deadline at 5.307 s, and with ioBlock at 0.305 s. In
// ruby/spec that was 39 of 56 library/net-ftp files killed by the clock and
// none under MRI (#916).
//
// Asking "is ioBlock implemented?" was always the wrong question. The right one
// is "WHO ELSE should be calling it?", and that is answerable from the AST
// without running anything: a blocking socket call in a binding must sit
// lexically inside a closure handed to ioBlock (or to threadBlock, or to a
// wrapper that forwards to one).
func TestEveryBlockingSocketCallReleasesTheGVL(t *testing.T) {
	// The methods that block on a peer. Dial/Accept/Read/Write on a net.Conn or
	// net.Listener can wait indefinitely; anything here must not do so while
	// holding the GVL.
	blocking := map[string]bool{
		"Accept": true, "Dial": true, "DialTimeout": true,
		"ReadString": true, "ReadFrom": true, "ReadFromUDP": true, "ReadFull": true,
	}
	// ⛔ Read is the commonest blocking socket call of all, and the first version
	// of this gate did not look for it -- which made it blind to retrbinary,
	// nineteen lines below the retrlines it DID catch, reading the same data
	// connection through conn.Read instead of bufio's ReadString. The gate had
	// the very defect it was written to catch: wired at some of its doors.
	//
	// It is not in the list above because `Read` on a bytes.Reader, a file or a
	// protocol decoder is not a socket call, and a gate that cried wolf on those
	// would be switched off within a week. The discriminator is the FILE: one
	// that does not import "net" cannot hold a net.Conn. Measured on this tree
	// that is enough for Read -- the six non-socket Read sites (crypto/rand,
	// mail, images, openbao, grpc, sftp) are all in files importing no net, and
	// restricting to net-importing files leaves exactly two, both real.
	//
	// ⛔ WRITE IS DELIBERATELY ABSENT, and the measurement is why. Adding it
	// reported eleven sites, of which eight are NOT network writes: seven
	// `s.writer().Write(...)` on a *bufio.Writer, which only buffers (the socket
	// write happens at Flush), and one `buf.Write(chunk)` on a bytes.Buffer in a
	// file that does import net. So the file-level rule does NOT hold for Write;
	// telling those apart needs the receiver's TYPE, not its file. Three real
	// unwrapped conn writes do exist (socket_raw.go:206, net_ftp_bind.go:431,
	// nethttp_bind.go's CONNECT). They are written down as a COMMENT at the foot
	// of the backlog -- not as entries, since an entry that never reports fails
	// this gate by design -- so the known gap is on the page instead of in
	// somebody's memory.
	connOnly := map[string]bool{"Read": true}
	// The wrappers that release it. A call inside one of these closures is fine;
	// ftpIO forwards to ioBlock, and a binding may add its own as long as it is
	// named here, which keeps this list the single place the exemption lives.
	releasing := map[string]bool{
		"ioBlock": true, "threadBlock": true, "threadRelease": true, "ftpIO": true,
		// ioBlockCancel is ioBlock plus an unblock function (#933): it releases
		// the GVL exactly the same way and additionally makes the call return on
		// Thread#kill. It is listed here rather than inferred, which is the whole
		// point of keeping this map the single place an exemption lives — adding
		// the wrapper without adding it here failed this gate, as it should.
		"ioBlockCancel": true,
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	type finding struct{ where, what string }
	var bad []finding
	scanned, calls := 0, 0

	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("read %s: %v", path, rerr)
		}
		f, perr := parser.ParseFile(fset, path, src, parser.ParseComments)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		scanned++
		exempt := exemptLines(fset, f)
		netFile := importsNet(f)

		// Walk with a stack so a call can be asked which closures enclose it.
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)

			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if !blocking[sel.Sel.Name] && !(netFile && connOnly[sel.Sel.Name]) {
				return true
			}
			calls++
			if enclosedByReleaser(stack, releasing) {
				return true
			}
			if exempt[enclosingFuncLine(fset, stack)] {
				return true
			}
			bad = append(bad, finding{
				where: fset.Position(call.Pos()).String(),
				what:  sel.Sel.Name,
			})
			return true
		})
	}

	// ⛔ "N of M, never N alone": a walk that parsed nothing finds no offender
	// either, and an empty result would read exactly like a clean tree.
	if scanned == 0 || calls == 0 {
		t.Fatalf("scanned %d file(s) and found %d blocking call(s); this gate cannot "+
			"have run — a zero here is an unread tree, not a clean one", scanned, calls)
	}
	t.Logf("%d file(s), %d blocking socket call(s) checked", scanned, calls)

	sort.Slice(bad, func(i, j int) bool { return bad[i].where < bad[j].where })

	// ⛔ A RATCHET, NOT A BIG-BANG. This gate was written after the fact and
	// finds sites in several bindings at once; failing on all of them would
	// have meant either a sprawling change nobody can review or — far more
	// likely — the gate never landing at all. The backlog records what was
	// already there on the day it was written, so the gate fires on what
	// happens NEXT, and the list can only shrink: an entry that no longer
	// reports is a failure too, so a fix cannot leave its allowance behind for
	// the next regression to hide in.
	backlog, order := readBacklog(t)
	seen := map[string]bool{}
	for _, b := range bad {
		key := trimFileLine(b.where)
		seen[key] = true
		if backlog[key] {
			continue
		}
		t.Errorf("%s: %s blocks while HOLDING the GVL.\n"+
			"    Wrap it in ioBlock(vm, func(){ … }). A Ruby program that serves itself "+
			"deadlocks here: the server thread returns from Accept and then waits for a "+
			"lock this call is sitting on (#916). If its callers already release the GVL, "+
			"say so on the function with //gvl:released-by-caller <reason>.", b.where, b.what)
	}
	for _, key := range order {
		if !seen[key] {
			t.Errorf("%s is in testdata/gvl-blocking-backlog.txt and no longer reports.\n"+
				"    Delete that line: a stale allowance is a place the next regression hides.", key)
		}
	}
}

// trimFileLine drops the column from a token.Position so a backlog entry does
// not churn when a line is reformatted.
func trimFileLine(pos string) string {
	if i := strings.LastIndexByte(pos, ':'); i >= 0 {
		return pos[:i]
	}
	return pos
}

// readBacklog reads the sites that already held the GVL when this gate was
// written. Comments and blank lines are ignored; the order is kept so the
// stale-entry report reads in file order.
func readBacklog(t *testing.T) (map[string]bool, []string) {
	t.Helper()
	const path = "testdata/gvl-blocking-backlog.txt"
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (the gate needs its backlog; an absent file is not an empty one)", path, err)
	}
	out, order := map[string]bool{}, []string{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[line] = true
		order = append(order, line)
	}
	return out, order
}

// importsNet reports whether the file imports the standard "net" package, which
// is the cheap, exact discriminator for "could a `Read` here be a socket read?".
// It is deliberately the plain import path and not a prefix match: net/http and
// net/url carry no net.Conn of their own.
func importsNet(f *ast.File) bool {
	for _, imp := range f.Imports {
		if imp.Path != nil && imp.Path.Value == `"net"` {
			return true
		}
	}
	return false
}

// enclosedByReleaser reports whether any FuncLit on the stack is an argument to
// one of the releasing wrappers. Lexical enclosure is the right test: ioBlock
// takes the closure and runs it with the GVL dropped, so a call anywhere inside
// that closure is covered, and one outside it is not.
func enclosedByReleaser(stack []ast.Node, releasing map[string]bool) bool {
	for i := len(stack) - 1; i >= 0; i-- {
		if _, ok := stack[i].(*ast.FuncLit); !ok {
			continue
		}
		// The parent of a FuncLit that is an argument is the CallExpr itself.
		for j := i - 1; j >= 0 && j >= i-2; j-- {
			call, ok := stack[j].(*ast.CallExpr)
			if !ok {
				continue
			}
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				if releasing[fn.Name] {
					return true
				}
			case *ast.SelectorExpr:
				if releasing[fn.Sel.Name] {
					return true
				}
			}
		}
	}
	return false
}

// exemptLines returns the start lines of the functions carrying the directive
//
//	//gvl:released-by-caller <reason>
//
// ⛔ THE EXEMPTION IS NAMED, NOT INFERRED. A purely lexical rule cannot see a
// HELPER whose callers wrap it — nethttpRawDial is reached only from inside an
// ioBlock, so its net.DialTimeout is already covered — and a gate that cried
// wolf there would be switched off within a week. Making the caller's promise
// explicit keeps the gate strict AND keeps every exemption readable in one
// grep, instead of hiding it in an interprocedural inference nobody can audit.
//
// A reason is required: a bare directive is a silent waiver, and this file
// exists because silent waivers are how Net::FTP went twenty sites without the
// wrapper everything else had.
func exemptLines(fset *token.FileSet, f *ast.File) map[int]bool {
	const directive = "//gvl:released-by-caller"
	out := map[int]bool{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Doc == nil {
			continue
		}
		for _, c := range fn.Doc.List {
			if !strings.HasPrefix(c.Text, directive) {
				continue
			}
			if strings.TrimSpace(strings.TrimPrefix(c.Text, directive)) == "" {
				continue // no reason given: not an exemption
			}
			out[fset.Position(fn.Pos()).Line] = true
		}
	}
	return out
}

// enclosingFuncLine is the start line of the innermost FuncDecl around a call,
// which is the key exemptLines records.
func enclosingFuncLine(fset *token.FileSet, stack []ast.Node) int {
	for i := len(stack) - 1; i >= 0; i-- {
		if fn, ok := stack[i].(*ast.FuncDecl); ok {
			return fset.Position(fn.Pos()).Line
		}
	}
	return -1
}
