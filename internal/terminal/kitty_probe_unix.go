//go:build darwin || linux

package terminal

import (
	"bytes"
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const (
	kittyQuery = "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"
	da1Query   = "\x1b[c"
)

// ProbeKitty actively tests for the kitty graphics protocol. It only runs
// with a real input and output terminal, restores terminal mode afterward,
// and bounds the wait to timeout. A positive response is authoritative.
func ProbeKitty(in, out *os.File, timeout time.Duration) bool {
	if in == nil || out == nil ||
		!term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return false
	}
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return false
	}
	defer func() { _ = term.Restore(int(in.Fd()), state) }()

	if _, err := out.WriteString(kittyQuery + da1Query); err != nil {
		return false
	}

	deadline := time.Now().Add(timeout)
	fd := int32(in.Fd())
	var response bytes.Buffer
	buf := make([]byte, 512)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		fds := []unix.PollFd{{Fd: fd, Events: unix.POLLIN}}
		n, err := unix.Poll(fds, int(remaining.Milliseconds()))
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			break
		}
		if n == 0 {
			break
		}
		count, err := unix.Read(int(fd), buf)
		if err != nil {
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
				continue
			}
			break
		}
		if count > 0 {
			_, _ = response.Write(buf[:count])
			text := response.String()
			if bytes.Contains([]byte(text), []byte("Gi=31;OK")) {
				return true
			}
			// A device-attributes reply without a graphics reply is a reliable
			// negative signal from terminals that answer DA1.
			if bytes.Contains([]byte(text), []byte("\x1b[?")) && bytes.Contains([]byte(text), []byte("c")) {
				return false
			}
		}
	}
	return false
}
