package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/dotfrankruan/cattv/internal/library"
	"github.com/dotfrankruan/cattv/internal/textsafe"
)

func (c *cli) runImport(ctx context.Context, args []string) error {
	fs := newFlagSet("cattv import")
	var help bool
	addHelpFlag(fs, &help)
	paths, err := parseFlags(fs, args)
	if err != nil {
		return &usageError{err: err}
	}
	if help {
		c.printImportHelp()
		return nil
	}
	if len(paths) == 0 {
		return &usageError{err: errors.New("import requires at least one file or directory")}
	}
	store := library.NewStore(c.cfg.LibraryPath)
	idx, report, err := store.ImportContext(ctx, paths)
	if err != nil {
		return err
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(c.errOut, "cattv: warning: %s\n", textsafe.Terminal(warning))
	}
	if report.Added+report.Updated == 0 {
		return &commandError{
			code: exitRuntime,
			err:  fmt.Errorf("no supported images were imported (checked %d path(s))", len(paths)),
		}
	}
	fmt.Fprintf(c.out, "Imported %d new image(s); refreshed %d; skipped %d.\n",
		report.Added, report.Updated, report.Skipped)
	fmt.Fprintf(c.out, "Local library: %s (%d image(s))\n", textsafe.Terminal(c.cfg.LibraryPath), len(idx.Items))
	if len(idx.Items) > 0 {
		fmt.Fprintln(c.out, "Run `cattv local` to view a local image without network access.")
	}
	return nil
}

func (c *cli) runLibrary(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		c.printLibraryHelp()
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return c.runLibraryList(rest)
	case "remove":
		return c.runLibraryRemove(rest)
	case "clear":
		return c.runLibraryClear(rest)
	case "add":
		return c.runImport(ctx, rest)
	default:
		return &usageError{err: fmt.Errorf("unknown library command %q", sub)}
	}
}

func (c *cli) runLibraryList(args []string) error {
	fs := newFlagSet("cattv library list")
	var help bool
	addHelpFlag(fs, &help)
	positional, err := parseFlags(fs, args)
	if err != nil {
		return &usageError{err: err}
	}
	if help {
		c.printLibraryHelp()
		return nil
	}
	if len(positional) != 0 {
		return &usageError{err: fmt.Errorf("unexpected argument %q", positional[0])}
	}
	idx, err := library.NewStore(c.cfg.LibraryPath).Load()
	if err != nil {
		return err
	}
	if len(idx.Items) == 0 {
		fmt.Fprintln(c.out, "Local library is empty.")
		fmt.Fprintln(c.out, "Add images with: cattv import ~/Pictures/cats")
		return nil
	}
	tw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tFORMAT\tSIZE\tDIMENSIONS\tPATH")
	for _, item := range idx.Items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d×%d\t%s\n",
			item.ID, strings.ToUpper(item.Format), humanBytes(item.Bytes), item.Width, item.Height, textsafe.Terminal(item.Path))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "\n%d image(s) at %s\n", len(idx.Items), textsafe.Terminal(c.cfg.LibraryPath))
	return nil
}

func (c *cli) runLibraryRemove(args []string) error {
	fs := newFlagSet("cattv library remove")
	var help bool
	addHelpFlag(fs, &help)
	positional, err := parseFlags(fs, args)
	if err != nil {
		return &usageError{err: err}
	}
	if help {
		c.printLibraryHelp()
		return nil
	}
	if len(positional) != 1 {
		return &usageError{err: errors.New("library remove requires one id or path")}
	}
	idx, removed, err := library.NewStore(c.cfg.LibraryPath).Remove(positional[0])
	if err != nil {
		return err
	}
	if removed == 0 {
		return &commandError{code: exitRuntime, err: fmt.Errorf("no library item matched %q", textsafe.Terminal(positional[0]))}
	}
	fmt.Fprintf(c.out, "Removed %d item(s); %d remain.\n", removed, len(idx.Items))
	return nil
}

func (c *cli) runLibraryClear(args []string) error {
	fs := newFlagSet("cattv library clear")
	var help bool
	addHelpFlag(fs, &help)
	positional, err := parseFlags(fs, args)
	if err != nil {
		return &usageError{err: err}
	}
	if help {
		c.printLibraryHelp()
		return nil
	}
	if len(positional) != 0 {
		return &usageError{err: fmt.Errorf("unexpected argument %q", positional[0])}
	}
	store := library.NewStore(c.cfg.LibraryPath)
	idx, err := store.Load()
	if err != nil {
		return err
	}
	count := len(idx.Items)
	if err := store.Clear(); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Cleared %d item(s) from the local library. Image files were not touched.\n", count)
	return nil
}
