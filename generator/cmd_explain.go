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
	target, err := parseTargetArgs(fset, args)
	if err == flag.ErrHelp {
		return nil
	} else if err != nil {
		return err
	}

	// Go's flag package stops parsing at the first positional, so a flag written
	// after a tool name arrives here as a tool name. Say that, rather than
	// letting Select report `no such tool "--json"`.
	tools := fset.Args()
	for _, t := range tools {
		if strings.HasPrefix(t, "-") {
			fset.Usage()
			return fmt.Errorf("flag %q must come before the positional <tool> names", t)
		}
	}

	sel := gen.Selector{Tools: tools, Journeys: journeys, Views: views}
	if sel.Empty() {
		fset.Usage()
		return errors.New("nothing selected — pass one or more <tool> names, --journey NAME, or --view NAME")
	}

	full, ok, err := buildOutline(os.Stdout, target, *asJSON)
	if !ok {
		return err
	}

	selected, serr := full.Select(sel)
	if serr != nil {
		if *asJSON {
			writeJSON(os.Stdout, jsonFailure{Error: serr.Error()})
			return errPrinted
		}
		return serr
	}

	if *asJSON {
		return writeJSON(os.Stdout, selected)
	}
	renderExplain(os.Stdout, full, selected, time.Now())
	return nil
}
