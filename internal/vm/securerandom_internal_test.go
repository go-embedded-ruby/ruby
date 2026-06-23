package vm

import (
	"errors"
	"io"
	"math/big"
	"testing"
)

// TestSecureBytesFailure covers the otherwise-unreachable crypto/rand failure
// paths by injecting failing readers through the seams.
func TestSecureRandomFailures(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		defer func() {
			secureRandRead = realSecureRandRead
			if r := recover(); r == nil {
				t.Fatal("secureBytes did not panic on a read failure")
			}
		}()
		secureRandRead = func([]byte) (int, error) { return 0, errors.New("boom") }
		secureBytes(4)
	})

	t.Run("int", func(t *testing.T) {
		defer func() {
			secureRandInt = realSecureRandInt
			if r := recover(); r == nil {
				t.Fatal("secureInt did not panic on a rand.Int failure")
			}
		}()
		secureRandInt = func(io.Reader, *big.Int) (*big.Int, error) { return nil, errors.New("boom") }
		secureInt(10)
	})
}

var (
	realSecureRandRead = secureRandRead
	realSecureRandInt  = secureRandInt
)
