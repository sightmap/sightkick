// Command sightkick compiles a .sightkick/ tool layer + sightmap corpus into IR.
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
	fmt.Fprintln(os.Stderr, `sightkick — compile a .sightkick/ tool layer + sightmap corpus into IR

Usage:
  sightkick outline <app-dir> [--json]
  sightkick explain <app-dir> [--journey NAME] [--view NAME] [--json] [<tool>...]
  sightkick build <app-dir | .sightkick-dir> [-o out.json] [--verify]
  sightkick browser <app-dir> [--url URL] [--webmcp] [--extensions PATHS] [--profile DIR] [--cdp-port N] [--no-start]
  sightkick call <app-dir> <tool> [--param k=v ...] [--via webmcp|cli] [--timeout-ms N]
  sightkick runtime [-o out.js]
  sightkick skills install [--target DIR]

  outline  read the tool layer at plan time, cheaply: journeys + every tool's
           one-line summary, grouped by view (stdout, or --json). The
           alternative to reading the compiled IR or the raw YAML corpus just
           to resolve a Gherkin scenario into a plan — see
           docs/scenario-testing.md §6.1.
  explain  full plan-time detail (description, params, ensure_view, returns
           shape) for the union of the named tools, journeys, and views. Run
           'outline' first to find names; 'explain' fills in the rest.
  build    compile a corpus + manifest into IR (stdout, or -o out.json).
           --verify checks each tool's returns extractors against the view's
           captured snapshots and warns on fields that resolve empty on every row.
  browser  build the IR, start a sightmap browser session (auto-URL from the
           corpus home view unless --url; --webmcp adds the native-modelContext
           blink flags), and persist-inject the runtime + IR so the tools
           register on the page. Requires the sightmap CLI on PATH.
  call     invoke one tool from the .sightkick/ tool layer by name against a
           live browser session, and print its ToolResult (with guidance) as
           JSON; exits non-zero on ok:false. --via picks how it runs:
             webmcp (default)  ask the page's own registered WebMCP tool to
                               run itself — the path a real client takes.
                               Needs the runtime on the page: run 'sightkick
                               browser' first, or serve a page that boots it.
             cli               translate the tool's steps into 'sightmap
                               browser <verb>' commands, which act through
                               real browser input events. Needs no runtime,
                               and reaches portal-rendered elements (dropdown
                               items, modal buttons) that the runtime's
                               synthetic clicks do not.
  runtime  emit the runtime bundle to inject into a live page (stdout, or -o out.js).
  skills   install the embedded agent skills (default ~/.agents/skills).`)
	os.Exit(2)
}

func main() {
	args := os.Args[1:]
	if len(args) < 1 {
		usage()
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
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
	if args[0] == "browser" {
		if err := runBrowser(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "✗ "+err.Error())
			os.Exit(1)
		}
		return
	}
	if args[0] == "call" {
		if err := runCall(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "✗ "+err.Error())
			os.Exit(1)
		}
		return
	}
	if args[0] == "outline" {
		if err := runOutline(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "✗ "+err.Error())
			os.Exit(1)
		}
		return
	}
	if args[0] == "explain" {
		if err := runExplain(args[1:]); err != nil {
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
		case "-h", "--help":
			usage()
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
