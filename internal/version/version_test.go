package version

import "testing"

func TestCurrentAndUserAgentNonEmpty(t *testing.T) {
	v := Current()
	if v == "" {
		t.Fatal("Current returned an empty version")
	}
	want := "cattv/" + v + " (+https://github.com/dotfrankruan/cattv)"
	if got := UserAgent(); got != want {
		t.Fatalf("UserAgent = %q, want %q", got, want)
	}
}
