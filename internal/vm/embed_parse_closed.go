//go:build rbgo_closed

package vm

// parseFault has nothing to guard in a closed-world binary: there is no
// parser, and an evaluation raises NotImplementedError.
func parseFault(string) error { return nil }
