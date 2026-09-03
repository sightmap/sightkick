package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// runCall invokes one already-registered tool in a running `sightkick browser`
// session by name, and prints its structured ToolResult (including any
// compiled guidance) as JSON. It exists so driving a tool from the CLI/agent
// side is a single parseable command instead of the hand-typed
// `sightmap browser eval "window.__sightkick.call(...)"` one-liner `browser`
// prints — which, on its own, is incomplete: sightmap's `browser eval` runs
// with awaitPromise:false, so it returns the pending Promise object, never the
// resolved result. `call` works around that by kicking off the call with one
// eval, then polling a second eval for the stashed resolved value — the same
// poll-until-ready shape `waitForTab` already uses for session startup.
func runCall(args []string) error {
	fset := flag.NewFlagSet("call", flag.ContinueOnError)
	fset.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: sightkick call <app-dir> <tool> [--param k=v ...] [--timeout-ms N]")
		fset.PrintDefaults()
	}
	var params stringList
	fset.Var(&params, "param", "Tool param as key=value (repeatable). Value parses as JSON when possible, else a raw string.")
	timeoutMs := fset.Int("timeout-ms", 10000, "How long to wait for the tool's promise to resolve")
	if len(args) >= 1 && (args[0] == "-h" || args[0] == "--help") {
		fset.Usage()
		return nil
	}
	if len(args) < 2 {
		fset.Usage()
		return fmt.Errorf("missing <app-dir> and/or <tool>")
	}
	target, tool := args[0], args[1]
	if strings.HasPrefix(target, "-") || strings.HasPrefix(tool, "-") {
		fset.Usage()
		return fmt.Errorf("first two arguments must be <app-dir> <tool>, got %q %q", target, tool)
	}
	if err := fset.Parse(args[2:]); err == flag.ErrHelp {
		return nil
	} else if err != nil {
		return err
	}

	sm, err := exec.LookPath("sightmap")
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
	argsJSON, err := json.Marshal(argsObj)
	if err != nil {
		return err
	}
	toolJSON, err := json.Marshal(tool)
	if err != nil {
		return err
	}

	// A fresh, unique key per invocation, so two overlapping `call`s (or a
	// leftover from a killed one) never read each other's stashed result.
	slot := fmt.Sprintf("__sightkick_call_%d", time.Now().UnixNano())

	kickoff := fmt.Sprintf(
		`window.%s = null; window.__sightkick.call(%s, %s).then(function(r){ window.%s = JSON.stringify(r); }); "started"`,
		slot, toolJSON, argsJSON, slot,
	)
	if _, err := runSightmapEval(sm, appDir, kickoff); err != nil {
		return fmt.Errorf("start tool call: %w", err)
	}

	deadline := time.Now().Add(time.Duration(*timeoutMs) * time.Millisecond)
	poll := fmt.Sprintf("window.%s", slot)
	for {
		out, err := runSightmapEval(sm, appDir, poll)
		if err != nil {
			return fmt.Errorf("poll tool result: %w", err)
		}
		out = strings.TrimSpace(out)
		if out != "" && out != "null" {
			// runSightmapEval already returned sightmap's own JSON encoding of the
			// JS string value, i.e. a JSON string literal wrapping our JSON.
			var resultJSONString string
			if err := json.Unmarshal([]byte(out), &resultJSONString); err != nil {
				return fmt.Errorf("unexpected eval output %q: %w", out, err)
			}
			var pretty map[string]any
			if err := json.Unmarshal([]byte(resultJSONString), &pretty); err != nil {
				// Not an object (shouldn't happen for a ToolResult) — print raw.
				fmt.Println(resultJSONString)
				return nil
			}
			b, _ := json.MarshalIndent(pretty, "", "  ")
			fmt.Println(string(b))
			if ok, _ := pretty["ok"].(bool); !ok {
				return fmt.Errorf("tool %q returned ok:false", tool)
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("tool %q did not resolve within %dms", tool, *timeoutMs)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// runSightmapEval runs `sightmap browser eval <script>` from dir and returns
// its stdout (sightmap's own JSON-encoded eval result), trimmed.
func runSightmapEval(sm, dir, script string) (string, error) {
	cmd := exec.Command(sm, "browser", "eval", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
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
