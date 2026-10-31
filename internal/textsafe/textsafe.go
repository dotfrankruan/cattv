// Package textsafe makes untrusted metadata safe for terminal output.
package textsafe

import (
	"strings"
	"unicode"
)

// Terminal replaces line-breaking controls with spaces and removes other
// control characters so filenames, photographer metadata, or server errors
// cannot inject ANSI/OSC sequences into the user's terminal.
func Terminal(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
