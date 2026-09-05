package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"sightkick/generator/internal/gen"
)

// runExplain is the drill-down pass: full plan-time detail (description,
// params, ensure_view, returns shape — guidance excluded, see outline.go's
// ReturnOutline doc) for a subset named by positional tool names and/or
// repeatable --journey/--view flags, which UNION rather than intersect (see
// gen.Selector). Deliberately no --tool flag: positional args plus --scope-like
// flags mirrors sightmap's `explain [SELECTOR]` / `gap --scope COMPONENT`
// rather than three symmetric flags.
func runExplain(args []string) error {
	fset := flag.NewFlagSet("explain", flag.ContinueOnError)
	var journeys, views stringList
	fset.Var(&journeys, "journey", "Include this journey's tools (repeatable; union, not intersection, with other selectors).")
	fset.Var(&views, "view", "Include this view's tools (repeatable).")
	asJSON := fset.Bool("json", false, "Print the selected detail as one JSON object instead of text.")
	fset.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: sightkick explain <app-dir> [--journey NAME] [--view NAME] [--json] [<tool>...]")
		fmt.Fprintln(os.Stderr, "\nfull plan-time detail (description, params, ensure_view, returns shape) for")
		fmt.Fprintln(os.Stderr, "the union of the named tools, journeys, and views. Run `sightkick outline`")
		fmt.Fprintln(os.Stderr, "first to find names.")
		fmt.Fprintln(os.Stderr, "\nFlags must come before any positional <tool> names (Go's flag package stops")
		fmt.Fprintln(os.Stderr, "parsing at the first positional argument).\n\nFlags:")
		fset.PrintDefaults()
	}
	if len(args) >= 1 && (args[0] == "-h" || args[0] == "--help") {
		fset.Usage()
		return nil
	}
	if len(args) < 1 {
		fset.Usage()
		return errors.New("missing <app-dir>")
	}
	target := args[0]
	if strings.HasPrefix(target, "-") {
		fset.Usage()
		return fmt.Errorf("first argument must be the app dir, got flag %q", target)
	}
	if err := fset.Parse(args[1:]); err == flag.ErrHelp {
		return nil
	} else if err != nil {
		return err
	}
	tools := fset.Args()

	if len(tools) == 0 && len(journeys) == 0 && len(views) == 0 {
		fset.Usage()
		return errors.New("nothing selected — pass one or more <tool> names, --journey NAME, or --view NAME")
	}

	full, ok, err := buildOutline(target, *asJSON)
	if !ok {
		return err
	}

	selected, serr := full.Select(gen.Selector{Tools: tools, Journeys: journeys, Views: views})
	if serr != nil {
		if *asJSON {
			writeJSON(jsonFailure{Error: serr.Error()})
			return errPrinted
		}
		return serr
	}

	if *asJSON {
		return writeJSON(selected)
	}
	renderExplain(os.Stdout, full, selected, time.Now())
	return nil
}
