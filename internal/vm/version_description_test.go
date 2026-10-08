package vm_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-embedded-ruby/ruby/internal/vm"
)

// TestDescriptionIsTheConstantTheVMReports: `rbgo -v` prints vm.vm.Description()
// while a running program reads RUBY_DESCRIPTION, and the two could drift
// because the version string was a const local to registerVersionConstants. A
// CLI that announces a version the VM underneath it does not report is worse
// than one that prints nothing, so this pins them to one source.
//
// Measured against MRI 4.0.7: `ruby -v`, `ruby --version` and
// `ruby -e 'puts RUBY_DESCRIPTION'` print the same bytes.
func TestDescriptionIsTheConstantTheVMReports(t *testing.T) {
	checkCases(t, []runCase{
		{fmt.Sprintf("p RUBY_DESCRIPTION == %q", vm.Description()), "true\n"},
	})
	// And it has to actually say something: an empty vm.Description() would satisfy
	// the equality above while making `rbgo -v` print a blank line.
	if !strings.HasPrefix(vm.Description(), "ruby ") {
		t.Errorf("vm.Description() = %q, want it to start with %q", vm.Description(), "ruby ")
	}
}
