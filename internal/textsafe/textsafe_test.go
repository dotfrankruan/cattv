package textsafe

import "testing"

func TestTerminal(t *testing.T) {
	got := Terminal("cat\x1b]0;pwned\a\nname\tfile")
	if got != "cat]0;pwned name file" {
		t.Fatalf("got %q", got)
	}
	if got := Terminal("普通猫 🐱"); got != "普通猫 🐱" {
		t.Fatalf("got %q", got)
	}
}
