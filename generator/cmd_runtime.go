package main

import (
	"flag"
	"fmt"
	"os"

	"sightkick/generator/runtimebundle"
)

// runRuntime emits the embedded runtime bundle — the payload injected into a live
// page to register the compiled tools (window.__sightkick.load(ir)). With no -o
// it writes to stdout; with -o it writes the file. This is what lets the
// debug/inject loop work from the installed binary alone, with no repo checkout:
//
//	sightkick runtime -o /tmp/sightkick-runtime.js
//	sightmap browser eval "$(cat /tmp/sightkick-runtime.js)"
func runRuntime(args []string) error {
	fset := flag.NewFlagSet("runtime", flag.ContinueOnError)
	out := fset.String("o", "", "Write the runtime bundle to this file (default: stdout)")
	fset.StringVar(out, "out", "", "Write the runtime bundle to this file (default: stdout)")
	if err := fset.Parse(args); err != nil {
		return err
	}

	if *out == "" {
		_, err := os.Stdout.Write(runtimebundle.JS)
		return err
	}
	if err := os.WriteFile(*out, runtimebundle.JS, 0o644); err != nil {
		return fmt.Errorf("runtime: write %s: %w", *out, err)
	}
	fmt.Fprintf(os.Stderr, "✓ wrote runtime bundle (%d bytes) to %s\n", len(runtimebundle.JS), *out)
	return nil
}
