// Command sightkick compiles a webmcp.tools.yaml + sightmap corpus into IR.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"sightkick/generator/internal/gen"
)

// Version is stamped by the release build (goreleaser ldflags); "dev" otherwise.
var Version = "dev"

func usage() {
	fmt.Fprintln(os.Stderr, `sightkick — compile a webmcp.tools.yaml + sightmap corpus into IR

Usage:
  sightkick build <manifest.yaml | app-dir> [-o out.json] [--verify]
  sightkick runtime [-o out.js]
  sightkick skills install [--target DIR]

  build    compile a corpus + manifest into IR (stdout, or -o out.json).
           --verify checks each tool's returns extractors against the view's
           captured snapshots and warns on fields that resolve empty on every row.
  runtime  emit the runtime bundle to inject into a live page (stdout, or -o out.js).
  skills   install the embedded agent skills (default ~/.agents/skills).`)
	os.Exit(2)
}

func main() {
	args := os.Args[1:]
	if len(args) < 1 {
		usage()
	}
	if args[0] == "skills" {
		if err := runSkills(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "✗ "+err.Error())
			os.Exit(1)
		}
		return
	}
	if args[0] == "runtime" {
		if err := runRuntime(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "✗ "+err.Error())
			os.Exit(1)
		}
		return
	}
	if args[0] != "build" {
		usage()
	}

	var target, out string
	var verify bool
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "-o", "--out":
			if i+1 < len(rest) {
				i++
				out = rest[i]
			}
		case "--verify":
			verify = true
		default:
			if target == "" {
				target = rest[i]
			}
		}
	}
	if target == "" {
		usage()
	}

	buildFn := gen.Build
	if verify {
		buildFn = gen.BuildVerified
	}
	ir, diags, err := buildFn(target)
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
