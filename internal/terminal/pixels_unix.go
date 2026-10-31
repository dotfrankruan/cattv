//go:build darwin || linux

package terminal

import (
	"os"

	"golang.org/x/sys/unix"
)

func pixelSize(f *os.File) (width, height int) {
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws == nil {
		return 0, 0
	}
	return int(ws.Xpixel), int(ws.Ypixel)
}
