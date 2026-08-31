// Command sightkick compiles a webmcp.tools.yaml + sightmap corpus into IR.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"sightkick/generator/internal/gen"
)

func usage() {
	fmt.Fprintln(os.Stderr, `sightkick — compile a webmcp.tools.yaml + sightmap corpus into IR

Usage:
  sightkick build <manifest.yaml | app-dir> [-o out.json]

  <app-dir> is a directory containing webmcp.tools.yaml.
  Without -o, the IR is written to stdout.`)
	os.Exit(2)
}

func main() {
	args := os.Args[1:]
	if len(args) < 1 || args[0] != "build" {
		usage()
	}

	var target, out string
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "-o", "--out":
			if i+1 < len(rest) {
				i++
				out = rest[i]
			}
		default:
			if target == "" {
				target = rest[i]
			}
		}
	}
	if target == "" {
		usage()
	}

	ir, diags, err := gen.Build(target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗ "+err.Error())
		os.Exit(1)
	}
	if len(diags) > 0 {
		fmt.Fprintln(os.Stderr, gen.Format(diags))
		fmt.Fprintln(os.Stderr, "")
	}
	if gen.HasErrors(diags) {
		fmt.Fprintf(os.Stderr, "✗ build failed: %d error(s)\n", gen.CountErrors(diags))
		os.Exit(1)
	}

	data, err := json.MarshalIndent(ir, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗ "+err.Error())
		os.Exit(1)
	}
	if out != "" {
		if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "✗ "+err.Error())
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "✓ wrote %d tool(s) to %s\n", len(ir.Tools), out)
	} else {
		os.Stdout.Write(append(data, '\n'))
	}
}
