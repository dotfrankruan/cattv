package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/dotfrankruan/cattv/internal/app"
	"github.com/dotfrankruan/cattv/internal/fetch"
	"github.com/dotfrankruan/cattv/internal/version"
)

const maxDownloadBytes = 8 << 20

func (c *cli) runNext(ctx context.Context, args []string) error {
	flags, err := newDisplayFlags()
	if err != nil {
		return err
	}
	fs := newFlagSet("cattv next")
	flags.register(fs)
	positional, err := parseFlags(fs, args)
	if err != nil {
		return &usageError{err: err}
	}
	if flags.help {
		c.printNextHelp()
		return nil
	}
	if len(positional) != 0 {
		return &usageError{err: fmt.Errorf("unexpected argument %q", positional[0])}
	}
	if err := flags.validate(); err != nil {
		return err
	}
	setup, err := c.setupDisplay(flags)
	if err != nil {
		return err
	}

	networkCtx, cancel := context.WithTimeout(ctx, flags.timeout)
	defer cancel()
	fetcher := fetch.New(minDuration(flags.timeout, 10*time.Second), flags.retries, maxDownloadBytes, version.UserAgent())
	a, err := app.New(displayOptions(c, flags, setup).options(), fetcher)
	if err != nil {
		return err
	}
	_, err = a.Next(networkCtx)
	if err != nil {
		return err
	}
	return nil
}

func (c *cli) runShow(args []string) error {
	flags, err := newDisplayFlags()
	if err != nil {
		return err
	}
	fs := newFlagSet("cattv show")
	flags.register(fs)
	positional, err := parseFlags(fs, args)
	if err != nil {
		return &usageError{err: err}
	}
	if flags.help {
		c.printShowHelp()
		return nil
	}
	if len(positional) != 1 {
		return &usageError{err: fmt.Errorf("show requires exactly one image path")}
	}
	if err := flags.validate(); err != nil {
		return err
	}
	setup, err := c.setupDisplay(flags)
	if err != nil {
		return err
	}
	a, err := app.New(displayOptions(c, flags, setup).options(), nil)
	if err != nil {
		return err
	}
	_, err = a.ShowFile(positional[0])
	return err
}

func (c *cli) runLocal(ctx context.Context, args []string) error {
	flags, err := newDisplayFlags()
	if err != nil {
		return err
	}
	fs := newFlagSet("cattv local")
	flags.register(fs)
	positional, err := parseFlags(fs, args)
	if err != nil {
		return &usageError{err: err}
	}
	if flags.help {
		c.printLocalHelp()
		return nil
	}
	if len(positional) != 0 {
		return &usageError{err: fmt.Errorf("unexpected argument %q", positional[0])}
	}
	if err := flags.validate(); err != nil {
		return err
	}
	setup, err := c.setupDisplay(flags)
	if err != nil {
		return err
	}
	a, err := app.New(displayOptions(c, flags, setup).options(), nil)
	if err != nil {
		return err
	}
	_, err = a.Local(ctx)
	return err
}

// appOptionsBuilder avoids repeating the many fields in two display commands.
type appOptionsBuilder struct {
	c     *cli
	flags *displayFlags
	setup displaySetup
}

func displayOptions(c *cli, flags *displayFlags, setup displaySetup) appOptionsBuilder {
	return appOptionsBuilder{c: c, flags: flags, setup: setup}
}

func (b appOptionsBuilder) options() app.Options {
	return app.Options{
		CatalogPath: b.c.cfg.CatalogPath,
		LibraryPath: b.c.cfg.LibraryPath,
		StatePath:   b.c.cfg.StatePath,
		Renderer:    b.setup.kind,
		Geometry:    b.setup.geometry,
		MaxColumns:  b.flags.width,
		MaxRows:     b.flags.height,
		NoCaption:   b.flags.noCaption,
		Color:       b.setup.geometry.TTY,
		Out:         b.c.out,
		Log:         b.c.errOut,
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
