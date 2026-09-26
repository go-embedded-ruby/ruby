// smoke.mjs runs the GOOS=js GOARCH=wasm playground module in a real JS host and
// asserts it INTERPRETED Ruby, not merely that it linked.
//
// The distinction is the point. `go build` for js/wasm proves the module compiles
// and links; it says nothing about whether syscall/js, the runtime's event loop or
// the interpreter work under a JS host. Issue #682 was a compile break, but the
// lane that catches the next one has to get further than the compiler.
//
//   node web/smoke.mjs web/rbgo.wasm
//
// Exits non-zero with a diagnosis on any failure. The host is node with Go's own
// wasm_exec.js glue (the same glue web/index.html loads in a browser), so what
// runs here is what the page runs.

import { readFile } from "node:fs/promises";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import path from "node:path";

const wasmPath = process.argv[2] ?? "web/rbgo.wasm";

// Go's loader glue ships with the toolchain; take it from the active GOROOT
// rather than a copy, so the glue and the module always come from one Go.
const { stdout } = await promisify(execFile)("go", ["env", "GOROOT"]);
const goroot = stdout.trim();
const glue = path.join(goroot, "lib", "wasm", "wasm_exec.js");
await import(glue).catch(async (err) => {
  // Go < 1.24 kept it under misc/wasm. Fail loudly rather than silently skipping.
  await import(path.join(goroot, "misc", "wasm", "wasm_exec.js")).catch(() => {
    throw new Error(`cannot load wasm_exec.js from ${goroot}: ${err.message}`);
  });
});

if (typeof globalThis.Go !== "function") {
  throw new Error("wasm_exec.js did not define globalThis.Go");
}

const go = new globalThis.Go();
const bytes = await readFile(wasmPath);
const { instance } = await WebAssembly.instantiate(bytes, go.importObject);

// main() ends in select{}, so go.run() never resolves: the module stays alive so
// its exported functions remain callable. Awaiting it would hang forever.
go.run(instance).catch((err) => {
  console.error(`the module exited: ${err}`);
  process.exit(1);
});

// main() sets rbgoReady last, after publishing both entry points.
const deadline = Date.now() + 60_000;
while (!globalThis.rbgoReady) {
  if (Date.now() > deadline) {
    throw new Error("rbgoReady was never set: main() did not finish publishing its entry points");
  }
  await new Promise((r) => setTimeout(r, 20));
}
for (const fn of ["rbgoEval", "rbgoImage"]) {
  if (typeof globalThis[fn] !== "function") {
    throw new Error(`the module did not publish ${fn}()`);
  }
}

let failed = 0;
const check = (what, got, want) => {
  if (got === want) {
    console.log(`  ok   ${what}: ${JSON.stringify(got)}`);
  } else {
    console.error(`  FAIL ${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
    failed++;
  }
};

// Arithmetic and a range: the compiler, the VM and $stdout capture.
const sum = globalThis.rbgoEval("puts (1..10).sum");
check("stdout of `puts (1..10).sum`", sum.output, "55\n");
check("no error", sum.error, "");

// An Errno constant, because that is the subsystem #682 broke. Both halves of
// errno.go's bucket rule are asserted: a name with a number, and one that js/wasm
// has no number for and so must be an Errno::NOERROR constant.
const errno = globalThis.rbgoEval(
  'puts Errno::ENOENT::Errno; puts Errno::ETXTBSY::Errno; ' +
  'puts Errno::ETXTBSY.equal?(Errno::NOERROR); puts Errno.constants.size',
);
check("Errno::ENOENT::Errno", errno.output.split("\n")[0], "2");
check("Errno::ETXTBSY::Errno on js/wasm (no number here)", errno.output.split("\n")[1], "0");
check("Errno::ETXTBSY is Errno::NOERROR on js/wasm", errno.output.split("\n")[2], "true");
check("Errno.constants.size (MRI known_errors)", errno.output.split("\n")[3], "158");
check("no error from the Errno snippet", errno.error, "");

// An exception path, so a raise crossing the VM in a JS host is exercised too.
const raised = globalThis.rbgoEval('begin; File.open("/nope/nope"); rescue => e; puts e.class; end');
check("a rescued open of a missing path names its class", raised.output, "Errno::ENOENT\n");

// A syntax error must come back as an error string, not a crash.
const bad = globalThis.rbgoEval("1 +");
check("a syntax error is reported, not fatal", bad.error !== "", true);

console.log(failed === 0 ? "smoke: all checks passed" : `smoke: ${failed} check(s) failed`);
process.exit(failed === 0 ? 0 : 1);
