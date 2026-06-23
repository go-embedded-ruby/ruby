//go:build !(js && wasm)

package vm

// registerJSBridge is a no-op off the browser. The interactive JS bridge — a
// Ruby `JS` module that calls into the page's DOM/Canvas — is only meaningful
// under GOOS=js GOARCH=wasm and lives in jsbridge_wasm.go (built only there, so
// it stays out of the native coverage gate, like the closed-world front-end).
func (vm *VM) registerJSBridge() { _ = vm }
