package render

import (
	"os"
	"strings"
)

// SelectOptions describes the environment used for automatic renderer
// selection. Probe is optional and should actively test kitty support.
type SelectOptions struct {
	Explicit  string
	Env       map[string]string
	StdoutTTY bool
	StdinTTY  bool
	Probe     func() bool
}

// Selection is the chosen backend plus a concise reason suitable for doctor.
type Selection struct {
	Kind   Kind
	Reason string
}

// EnvMap converts the process environment into a map for deterministic
// selection and testing.
func EnvMap() map[string]string {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}

// Choose selects a renderer without relying on a single terminal name. It
// combines explicit configuration, multiplexer detection, capability
// variables, known terminal markers, an optional active probe, and a portable
// ANSI fallback.
func Choose(opts SelectOptions) Selection {
	env := opts.Env
	if env == nil {
		env = EnvMap()
	}
	explicit := strings.TrimSpace(opts.Explicit)
	if explicit == "" {
		explicit = strings.TrimSpace(env["CATTV_RENDERER"])
	}
	if kind, err := ParseKind(explicit); err == nil && kind != KindAuto {
		return Selection{Kind: kind, Reason: "forced by command line or CATTV_RENDERER"}
	}

	if !opts.StdoutTTY {
		return Selection{Kind: KindANSI, Reason: "standard output is not a terminal"}
	}
	if isMultiplexer(env) {
		return Selection{Kind: KindANSI, Reason: "tmux/screen detected; using portable renderer (force with --renderer or CATTV_RENDERER)"}
	}
	if strings.EqualFold(env["TERM"], "dumb") {
		return Selection{Kind: KindANSI, Reason: "TERM=dumb"}
	}

	program := strings.ToLower(env["TERM_PROGRAM"])
	termValue := strings.ToLower(env["TERM"])
	lcTerminal := strings.ToLower(env["LC_TERMINAL"])

	switch {
	case env["KITTY_WINDOW_ID"] != "",
		strings.Contains(program, "kitty"),
		strings.Contains(termValue, "kitty"),
		env["WEZTERM_PANE"] != "",
		strings.Contains(program, "wezterm"),
		strings.Contains(termValue, "wezterm"),
		env["GHOSTTY_RESOURCES_DIR"] != "",
		strings.Contains(program, "ghostty"),
		strings.Contains(termValue, "ghostty"),
		env["KONSOLE_VERSION"] != "",
		strings.Contains(program, "rio"):
		return Selection{Kind: KindKitty, Reason: "terminal advertises kitty graphics support"}
	case env["ITERM_SESSION_ID"] != "",
		program == "iterm.app",
		lcTerminal == "iterm2":
		return Selection{Kind: KindITerm2, Reason: "iTerm2 inline image environment detected"}
	}

	switch program {
	case "apple_terminal", "vscode", "hyper", "tabby", "warpterminal":
		return Selection{Kind: KindANSI, Reason: "terminal is known not to support inline graphics"}
	}

	if opts.Probe != nil && opts.StdinTTY && env["CATTV_NO_PROBE"] != "1" {
		if opts.Probe() {
			return Selection{Kind: KindKitty, Reason: "active terminal capability probe succeeded"}
		}
		return Selection{Kind: KindANSI, Reason: "no inline graphics response; using portable renderer"}
	}
	return Selection{Kind: KindANSI, Reason: "no supported inline graphics terminal detected"}
}

func isMultiplexer(env map[string]string) bool {
	if env["TMUX"] != "" || env["STY"] != "" {
		return true
	}
	termValue := strings.ToLower(env["TERM"])
	return strings.Contains(termValue, "tmux") || strings.HasPrefix(termValue, "screen")
}
