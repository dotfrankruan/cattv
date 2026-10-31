package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/dotfrankruan/cattv/internal/render"
	"github.com/dotfrankruan/cattv/internal/terminal"
)

type displayFlags struct {
	renderer  string
	width     int
	height    int
	noCaption bool
	timeout   time.Duration
	retries   int
	help      bool
}

func newDisplayFlags() (*displayFlags, error) {
	timeout := 20 * time.Second
	if value := os.Getenv("CATTV_TIMEOUT"); value != "" {
		parsed, err := parseFlexibleDuration(value)
		if err != nil {
			return nil, fmt.Errorf("invalid CATTV_TIMEOUT: %w", err)
		}
		timeout = parsed
	}
	retries := 2
	if value := os.Getenv("CATTV_RETRIES"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("invalid CATTV_RETRIES %q", value)
		}
		retries = parsed
	}
	return &displayFlags{timeout: timeout, retries: retries}, nil
}

func (d *displayFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&d.renderer, "renderer", d.renderer, "image backend: auto, kitty, iterm2, or ansi")
	fs.IntVar(&d.width, "width", 0, "maximum image width in terminal cells (0 = fit terminal)")
	fs.IntVar(&d.height, "height", 0, "maximum image height in terminal cells (0 = fit terminal)")
	fs.BoolVar(&d.noCaption, "no-caption", false, "hide the attribution caption")
	fs.Var(durationValue{target: &d.timeout}, "timeout", "overall network deadline for the image, e.g. 20s")
	fs.IntVar(&d.retries, "retries", d.retries, "retry attempts for transient image-server errors")
	addHelpFlag(fs, &d.help)
}

func (d *displayFlags) validate() error {
	// The automatic probe can discover kitty support, but a bad explicit choice
	// should fail before a network request is made.
	if _, err := render.ParseKind(d.renderer); err != nil {
		return &usageError{err: err}
	}
	if env := os.Getenv("CATTV_RENDERER"); d.renderer == "" && env != "" {
		if _, err := render.ParseKind(env); err != nil {
			return &usageError{err: fmt.Errorf("invalid CATTV_RENDERER: %w", err)}
		}
	}
	if d.width < 0 || d.width > 1000 {
		return &usageError{err: errors.New("--width must be between 0 and 1000")}
	}
	if d.height < 0 || d.height > 1000 {
		return &usageError{err: errors.New("--height must be between 0 and 1000")}
	}
	if d.timeout < time.Second || d.timeout > 10*time.Minute {
		return &usageError{err: errors.New("--timeout must be between 1s and 10m")}
	}
	if d.retries < 0 || d.retries > 10 {
		return &usageError{err: errors.New("--retries must be between 0 and 10")}
	}
	return nil
}

type displaySetup struct {
	kind     render.Kind
	reason   string
	geometry terminal.Geometry
}

func (c *cli) setupDisplay(d *displayFlags) (displaySetup, error) {
	geometry := terminal.Detect(c.out)
	env := render.EnvMap()
	probe := func() bool {
		return terminal.ProbeKitty(c.in, c.out, 120*time.Millisecond)
	}
	selection := render.Choose(render.SelectOptions{
		Explicit:  d.renderer,
		Env:       env,
		StdoutTTY: geometry.TTY,
		StdinTTY:  terminal.IsTTY(c.in),
		Probe:     probe,
	})
	if selection.Kind == render.KindAuto {
		selection.Kind = render.KindANSI
		selection.Reason = "automatic selection returned no concrete renderer"
	}
	return displaySetup{kind: selection.Kind, reason: selection.Reason, geometry: geometry}, nil
}

type durationValue struct {
	target *time.Duration
}

func (v durationValue) String() string {
	if v.target == nil {
		return ""
	}
	return v.target.String()
}

func (v durationValue) Set(s string) error {
	if v.target == nil {
		return errors.New("nil duration target")
	}
	d, err := parseFlexibleDuration(s)
	if err != nil {
		return err
	}
	*v.target = d
	return nil
}

func parseFlexibleDuration(s string) (time.Duration, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	if seconds, err := strconv.Atoi(s); err == nil {
		return time.Duration(seconds) * time.Second, nil
	}
	return 0, fmt.Errorf("invalid duration %q (use values like 20s or 2m)", s)
}
