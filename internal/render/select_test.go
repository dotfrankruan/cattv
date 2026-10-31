package render

import "testing"

func TestChooseRenderer(t *testing.T) {
	tests := []struct {
		name      string
		opts      SelectOptions
		want      Kind
		probeCall bool
	}{
		{
			name: "explicit renderer works when piped",
			opts: SelectOptions{Explicit: "kitty", Env: map[string]string{}, StdoutTTY: false},
			want: KindKitty,
		},
		{
			name: "redirected output falls back",
			opts: SelectOptions{Env: map[string]string{"TERM_PROGRAM": "ghostty"}, StdoutTTY: false},
			want: KindANSI,
		},
		{
			name: "ghostty kitty support",
			opts: SelectOptions{Env: map[string]string{"TERM_PROGRAM": "ghostty"}, StdoutTTY: true},
			want: KindKitty,
		},
		{
			name: "wezterm kitty support",
			opts: SelectOptions{Env: map[string]string{"WEZTERM_PANE": "3"}, StdoutTTY: true},
			want: KindKitty,
		},
		{
			name: "iterm native inline images",
			opts: SelectOptions{Env: map[string]string{"TERM_PROGRAM": "iTerm.app"}, StdoutTTY: true},
			want: KindITerm2,
		},
		{
			name: "iterm session overrides generic term",
			opts: SelectOptions{Env: map[string]string{"ITERM_SESSION_ID": "w0t0p0", "TERM": "xterm-256color"}, StdoutTTY: true},
			want: KindITerm2,
		},
		{
			name: "tmux falls back safely",
			opts: SelectOptions{Env: map[string]string{"TERM_PROGRAM": "ghostty", "TMUX": "/tmp/tmux"}, StdoutTTY: true},
			want: KindANSI,
		},
		{
			name: "screen falls back safely",
			opts: SelectOptions{Env: map[string]string{"TERM": "screen-256color", "TERM_PROGRAM": "ghostty"}, StdoutTTY: true},
			want: KindANSI,
		},
		{
			name: "apple terminal is a known negative",
			opts: SelectOptions{
				Env:       map[string]string{"TERM_PROGRAM": "Apple_Terminal"},
				StdoutTTY: true,
				StdinTTY:  true,
				Probe: func() bool {
					panic("probe must not run for known unsupported terminal")
				},
			},
			want: KindANSI,
		},
		{
			name: "unknown terminal with successful probe",
			opts: SelectOptions{
				Env:       map[string]string{"TERM": "xterm-256color"},
				StdoutTTY: true,
				StdinTTY:  true,
				Probe: func() bool {
					return true
				},
			},
			want:      KindKitty,
			probeCall: true,
		},
		{
			name: "unknown terminal without successful probe",
			opts: SelectOptions{
				Env:       map[string]string{"TERM": "xterm-256color"},
				StdoutTTY: true,
				StdinTTY:  true,
				Probe: func() bool {
					return false
				},
			},
			want: KindANSI,
		},
		{
			name: "probe requires stdin tty",
			opts: SelectOptions{
				Env:       map[string]string{"TERM": "xterm-256color"},
				StdoutTTY: true,
				StdinTTY:  false,
				Probe: func() bool {
					panic("probe must not run without an input terminal")
				},
			},
			want: KindANSI,
		},
		{
			name: "environment override",
			opts: SelectOptions{Env: map[string]string{"CATTV_RENDERER": "ansi", "TERM_PROGRAM": "ghostty"}, StdoutTTY: true},
			want: KindANSI,
		},
		{
			name: "probe disabled by environment",
			opts: SelectOptions{
				Env:       map[string]string{"TERM": "xterm-256color", "CATTV_NO_PROBE": "1"},
				StdoutTTY: true,
				StdinTTY:  true,
				Probe: func() bool {
					panic("probe must honor CATTV_NO_PROBE")
				},
			},
			want: KindANSI,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Choose(tt.opts)
			if got.Kind != tt.want {
				t.Fatalf("kind = %s, want %s (reason: %s)", got.Kind, tt.want, got.Reason)
			}
			if got.Reason == "" {
				t.Fatal("selection has no diagnostic reason")
			}
		})
	}
}

func TestParseKind(t *testing.T) {
	for input, want := range map[string]Kind{
		"": KindAuto, "auto": KindAuto, "kitty": KindKitty, "iTerm2": KindITerm2,
		"iterm": KindITerm2, "ansi": KindANSI, "unicode": KindANSI,
	} {
		got, err := ParseKind(input)
		if err != nil || got != want {
			t.Errorf("ParseKind(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := ParseKind("sixel"); err == nil {
		t.Fatal("expected error for unsupported renderer")
	}
}
