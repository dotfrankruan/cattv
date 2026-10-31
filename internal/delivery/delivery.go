// Package delivery exposes CatTV's experimental terminal stream transports.
//
// The FIFO endpoint intentionally has no message framing: POSIX FIFOs do not
// provide message boundaries or reader identity. The Unix socket endpoint uses
// HTTP framing for strict one-response-per-request semantics.
package delivery

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/dotfrankruan/cattv/internal/textsafe"
)

// MaxFrameSize bounds a single rendered terminal response. Legitimate images
// are far smaller; the cap protects clients and servers from runaway frames.
const MaxFrameSize = 16 << 20

// ErrAlreadyRunning indicates another delivery server owns the endpoint.
var ErrAlreadyRunning = errors.New("another delivery server is already running")

// Generator renders one complete terminal response. It must not write to the
// FIFO or socket directly; returning a complete byte slice lets the transport
// close the writer after a complete frame.
type Generator interface {
	Generate(context.Context) ([]byte, error)
}

// GeneratorFunc adapts a function to Generator.
type GeneratorFunc func(context.Context) ([]byte, error)

// Generate implements Generator.
func (f GeneratorFunc) Generate(ctx context.Context) ([]byte, error) { return f(ctx) }

func validateGenerator(g Generator) error {
	if g == nil {
		return errors.New("delivery generator is required")
	}
	return nil
}

func frameForError(err error) []byte {
	if err == nil {
		err = errors.New("unknown error")
	}
	return []byte("cattv: " + textsafe.Terminal(err.Error()) + "\n")
}

func checkedFrame(g Generator, ctx context.Context) ([]byte, error) {
	payload, err := g.Generate(ctx)
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, errors.New("generator returned an empty response")
	}
	if len(payload) > MaxFrameSize {
		return nil, fmt.Errorf("generated response is too large (%d bytes)", len(payload))
	}
	return payload, nil
}

func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if n > 0 {
			p = p[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
