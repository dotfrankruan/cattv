// Package render turns decoded images into terminal-native output using the
// kitty graphics protocol, iTerm2 inline images, or true-colour half blocks.
package render

import (
	"fmt"
	"image"
	"io"
	"strings"
)

// Kind identifies a rendering backend.
type Kind string

const (
	KindAuto   Kind = "auto"
	KindKitty  Kind = "kitty"
	KindITerm2 Kind = "iterm2"
	KindANSI   Kind = "ansi"
)

// ParseKind validates a user-supplied renderer name. A few familiar aliases
// are accepted to keep scripts readable.
func ParseKind(s string) (Kind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return KindAuto, nil
	case "kitty":
		return KindKitty, nil
	case "iterm", "iterm2", "iterm2-inline":
		return KindITerm2, nil
	case "ansi", "unicode", "blocks", "halfblock":
		return KindANSI, nil
	default:
		return "", fmt.Errorf("unknown renderer %q (choose auto, kitty, iterm2, or ansi)", s)
	}
}

// Env describes the drawable terminal rectangle. Rows should exclude space
// reserved for the attribution line.
type Env struct {
	Columns    int
	Rows       int
	CellWidth  int
	CellHeight int
}

// Result reports the amount of terminal space occupied by the rendered image.
type Result struct {
	Columns int
	Rows    int
	Width   int // raster width in pixels
	Height  int // raster height in pixels
}

// Renderer writes one image and leaves the cursor at the start of the line
// immediately below it. Implementations never persist image data.
type Renderer interface {
	Kind() Kind
	Render(w io.Writer, img image.Image, env Env) (Result, error)
}

// New constructs a renderer by kind.
func New(kind Kind) (Renderer, error) {
	switch kind {
	case KindKitty:
		return kittyRenderer{}, nil
	case KindITerm2:
		return iTerm2Renderer{}, nil
	case KindANSI:
		return ansiRenderer{}, nil
	default:
		return nil, fmt.Errorf("renderer %q cannot be constructed directly", kind)
	}
}
