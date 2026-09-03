package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"

	"sightkick/generator/internal/gen"
)

// runCall invokes one tool from a webmcp.tools.yaml manifest against a live
// `sightmap browser` session and prints its ToolResult (including any
// compiled guidance) as JSON.
//
// Execution is native: each step shells out directly to the matching
// `sightmap browser <verb>` subcommand — a real, CDP-trusted DOM action —
// instead of injecting sightkick's runtime and driving it through
// window.__sightkick.call's synthetic, JS-dispatched click sequence. The
// synthetic path is unreliable against portal-rendered elements (dropdown
// menu items, modal buttons rendered outside the app's own DOM subtree):
// confirmed live against two Fullstory components where the identical
// element takes a real click fine and silently no-ops on the injected
// runtime's dispatched one. The injected path also requires `sightmap
// browser inject`, a subcommand neither the sightmap CLI installed in this
// environment nor this fork's own `sightmap browser` implements, so it could
// not register a tool at all. This path needs no injection and no
// `sightkick browser` session — a plain `sightmap browser start` is enough.
func runCall(args []string) error {
	fset := flag.NewFlagSet("call", flag.ContinueOnError)
	fset.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: sightkick call <app-dir> <tool> [--param k=v ...] [--timeout-ms N]")
		fset.PrintDefaults()
	}
	var params stringList
	fset.Var(&params, "param", "Tool param as key=value (repeatable). Value parses as JSON when possible, else a raw string.")
	timeoutMs := fset.Int("timeout-ms", 5000, "Default wait_for timeout (ms) for steps that don't declare their own timeout_ms")
	if len(args) >= 1 && (args[0] == "-h" || args[0] == "--help") {
		fset.Usage()
		return nil
	}
	if len(args) < 2 {
		fset.Usage()
		return fmt.Errorf("missing <app-dir> and/or <tool>")
	}
	target, toolName := args[0], args[1]
	if strings.HasPrefix(target, "-") || strings.HasPrefix(toolName, "-") {
		fset.Usage()
		return fmt.Errorf("first two arguments must be <app-dir> <tool>, got %q %q", target, toolName)
	}
	if err := fset.Parse(args[2:]); err == flag.ErrHelp {
		return nil
	} else if err != nil {
		return err
	}

	sm, err := osexec.LookPath("sightmap")
	if err != nil {
		return fmt.Errorf("the 'sightmap' CLI is required but not on PATH — install it: npm i -g @sightmap/sightmap")
	}

	appDir := target
	if info, statErr := os.Stat(target); statErr == nil && !info.IsDir() {
		appDir = filepath.Dir(target)
	}

	argsObj, err := parseCallParams(params)
	if err != nil {
		return err
	}

	manifestPath := gen.ResolveManifestPath(target)
	m, diags, err := gen.LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}
	if gen.HasErrors(diags) {
		return fmt.Errorf("manifest has errors:\n%s", gen.Format(diags))
	}

	var toolDef *gen.ToolDef
	for i := range m.Tools {
		if m.Tools[i].Name == toolName {
			toolDef = &m.Tools[i]
			break
		}
	}
	if toolDef == nil {
		return fmt.Errorf("tool %q not found in %s", toolName, manifestPath)
	}

	// Resolved to an absolute path: every `sightmap` invocation below runs
	// with cmd.Dir = appDir, so a corpus dir left relative to the CWD here
	// would get silently re-joined against appDir and doubled.
	corpusDir := m.Corpus
	if !filepath.IsAbs(corpusDir) {
		corpusDir = filepath.Join(filepath.Dir(manifestPath), corpusDir)
	}
	corpusDir, err = filepath.Abs(corpusDir)
	if err != nil {
		return fmt.Errorf("resolve corpus dir: %w", err)
	}

	// Guidance is compiled from the journey graph; build the full IR just to
	// read this tool's slice of it, so the printed result keeps its
	// next-step breadcrumbs. A build failure elsewhere in the manifest (e.g.
	// another tool's bad compquery) shouldn't block calling *this* tool, so
	// this is best-effort.
	var guidance []gen.Suggestion
	if ir, bdiags, berr := gen.Build(target); berr == nil && !gen.HasErrors(bdiags) {
		for _, t := range ir.Tools {
			if t.Name == toolName {
				guidance = t.Guidance
				break
			}
		}
	}

	ne := &nativeExec{sm: sm, appDir: appDir, corpusDir: corpusDir, defaultTimeoutMs: *timeoutMs}
	outcome := ne.run(toolDef, argsObj)

	b, err := outcome.toJSON(guidance)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	if !outcome.ok {
		return fmt.Errorf("tool %q returned ok:false", toolName)
	}
	return nil
}

// parseCallParams turns repeated --param k=v flags into a JSON-ready map. Each
// value is tried as JSON first (so --param count=3 or --param watched=true
// produce a number/bool, not a string) and falls back to the raw string.
func parseCallParams(params []string) (map[string]any, error) {
	out := map[string]any{}
	for _, p := range params {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return nil, fmt.Errorf("--param %q is not in key=value form", p)
		}
		var decoded any
		if err := json.Unmarshal([]byte(v), &decoded); err == nil {
			out[k] = decoded
		} else {
			out[k] = v
		}
	}
	return out, nil
}
