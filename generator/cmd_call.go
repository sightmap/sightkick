package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"sightkick/generator/internal/gen"
)

// The two ways `call` can run a tool. webmcp is the default because it is the
// path a real WebMCP client takes: the page runs its own compiled tool, so a
// result proves the shipped contract works. cli is the fallback for a page
// that has no runtime on it, and for clicks the runtime's synthetic events
// cannot deliver.
const (
	viaWebMCP = "webmcp"
	viaCLI    = "cli"
)

// runCall invokes one tool from the .sightkick/ tool layer against a live
// browser session and prints its result as JSON, exiting non-zero if the tool
// failed. The session has to already be running (`sightmap browser start`);
// nothing is injected into the page. --via selects which of the two execution
// paths runs the tool; see session's runWebMCP and runNative.
func runCall(args []string) error {
	fset := flag.NewFlagSet("call", flag.ContinueOnError)
	fset.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: sightkick call <app-dir> <tool> [--param k=v ...] [--via webmcp|cli] [--timeout-ms N]")
		fset.PrintDefaults()
	}
	var params stringList
	fset.Var(&params, "param", "Tool param as key=value (repeatable). Value parses as JSON when possible, else a raw string.")
	via := fset.String("via", viaWebMCP, "How to run the tool: 'webmcp' asks the page's registered WebMCP tool to run itself (the path a real client takes; the page must have the runtime); 'cli' translates the tool's steps into 'sightmap browser' commands (real input events, no runtime needed).")
	timeoutMs := fset.Int("timeout-ms", 5000, "For --via webmcp, how long to wait for the tool to finish. For --via cli, the default wait_for timeout for steps that don't declare their own timeout_ms.")
	if len(args) >= 1 && (args[0] == "-h" || args[0] == "--help") {
		fset.Usage()
		return nil
	}
	if len(args) < 2 {
		fset.Usage()
		return errors.New("missing <app-dir> and/or <tool>")
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

	sightmapPath, err := exec.LookPath("sightmap")
	if err != nil {
		return errors.New("the 'sightmap' CLI is required but not on PATH — install it: npm i -g @sightmap/sightmap")
	}

	toolArgs, err := parseCallParams(params)
	if err != nil {
		return err
	}

	skDir, err := gen.ResolveSightkickDir(target)
	if err != nil {
		return err
	}
	manifest, diags, err := gen.LoadManifest(skDir)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}
	if gen.HasErrors(diags) {
		return fmt.Errorf("manifest has errors:\n%s", gen.Format(diags))
	}

	i := slices.IndexFunc(manifest.Tools, func(t gen.ToolDef) bool { return t.Name == toolName })
	if i < 0 {
		return fmt.Errorf("tool %q not found in %s", toolName, skDir)
	}

	// sightmap runs with its working directory set to appDir (the parent of the
	// .sightkick dir), so the corpus path has to be absolute — a relative one
	// would resolve against appDir a second time and point somewhere that
	// doesn't exist.
	appDir := filepath.Dir(skDir)
	corpusDir := manifest.Corpus
	if !filepath.IsAbs(corpusDir) {
		corpusDir = filepath.Join(skDir, corpusDir)
	}
	if corpusDir, err = filepath.Abs(corpusDir); err != nil {
		return fmt.Errorf("resolve corpus dir: %w", err)
	}

	toolDef := &manifest.Tools[i]
	sess := &session{sm: sightmapPath, appDir: appDir, corpusDir: corpusDir, defaultTimeoutMs: *timeoutMs}

	var outcome toolOutcome
	switch *via {
	case viaWebMCP:
		// The page holds the compiled tool and reports its own result,
		// guidance included, so there is nothing to prepare here.
		if outcome, err = sess.runWebMCP(toolName, toolArgs, *timeoutMs); err != nil {
			return err
		}

	case viaCLI:
		// Two things come from compiling the manifest rather than reading it:
		// the "call this next" hints, which are derived from its journeys, and
		// the extractors behind a tool's returns, which are declared in the
		// corpus and only named by the manifest.
		compiled := compiledTool(target, toolName)
		ret, specErr := returnSpecFor(toolDef.Returns, compiled)
		if specErr != nil {
			return specErr
		}
		outcome = sess.runNative(toolDef, ret, toolArgs)
		if compiled != nil {
			outcome.guidance = compiled.Guidance
		}

	default:
		return fmt.Errorf("unknown --via %q; expected %q or %q", *via, viaWebMCP, viaCLI)
	}

	out, err := outcome.toJSON()
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	if !outcome.ok {
		return fmt.Errorf("tool %q returned ok:false", toolName)
	}
	return nil
}

// compiledTool compiles the manifest and returns this tool's compiled form, or
// nil if the manifest does not compile. A compile failure may have nothing to
// do with the tool being called, so it isn't fatal on its own — the caller
// decides whether it needs what compiling would have produced.
func compiledTool(target, toolName string) *gen.Tool {
	ir, diags, err := gen.Build(target)
	if err != nil || gen.HasErrors(diags) {
		return nil
	}
	if i := slices.IndexFunc(ir.Tools, func(t gen.Tool) bool { return t.Name == toolName }); i >= 0 {
		return &ir.Tools[i]
	}
	return nil
}

// returnSpecFor pairs a tool's declared result with the compiled extractors
// that read it. It returns nil for a tool that reads nothing, including a
// returns: block carrying only a description.
func returnSpecFor(declared *gen.ReturnDef, compiled *gen.Tool) (*returnSpec, error) {
	if declared == nil || (declared.Value == nil && declared.List == nil) {
		return nil, nil
	}
	// Reading a property means running the corpus's `extract:` directive for
	// it, which only exists in compiled form. Without it there is no way to
	// produce the result the tool promises, and returning nothing would look
	// like a legitimately empty page.
	if compiled == nil || compiled.Returns == nil {
		return nil, errors.New("this tool returns a value, which needs the manifest to compile — run `sightkick build` to see why it doesn't")
	}

	if declared.Value != nil {
		if compiled.Returns.Extractor == nil {
			return nil, errors.New("compiled returns is missing its extractor")
		}
		return &returnSpec{
			query:  declared.Value.Query,
			fields: map[string]gen.Extractor{valueField: *compiled.Returns.Extractor},
		}, nil
	}

	fields := make(map[string]gen.Extractor, len(compiled.Returns.Fields))
	for name, f := range compiled.Returns.Fields {
		fields[name] = f.Extractor
	}
	return &returnSpec{query: declared.List.Rows, fields: fields, isList: true}, nil
}

// parseCallParams turns repeated --param k=v flags into the tool's arguments.
// Each value is parsed as JSON when it can be, so --param count=3 gives a
// number and --param watched=true a boolean; anything else stays a string.
func parseCallParams(params []string) (map[string]any, error) {
	out := map[string]any{}
	for _, p := range params {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return nil, fmt.Errorf("--param %q is not in key=value form", p)
		}
		var decoded any
		if err := json.Unmarshal([]byte(v), &decoded); err != nil {
			out[k] = v
			continue
		}
		out[k] = decoded
	}
	return out, nil
}
