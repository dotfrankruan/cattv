//go:build !darwin && !linux

package terminal

import "os"

func pixelSize(_ *os.File) (width, height int) {
	return 0, 0
}
