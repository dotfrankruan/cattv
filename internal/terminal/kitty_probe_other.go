//go:build !darwin && !linux

package terminal

import (
	"os"
	"time"
)

// ProbeKitty is unsupported on this platform; known terminal environment
// signals still allow explicit kitty selection.
func ProbeKitty(_, _ *os.File, _ time.Duration) bool {
	return false
}
