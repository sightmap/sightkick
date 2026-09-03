package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"sightkick/generator/internal/gen"
)

// runWebMCP runs a tool by asking the page's registered WebMCP tool to run
// itself, through the sightmap CLI's own WebMCP driver, `sightmap browser mcp
// call`. That driver talks to the standard `document.modelContext` surface and
// owns the differences between Chrome's built-in modelContext (the whole
// registered tool from getTools(), arguments as a JSON string, the result as a
// JSON string of the envelope) and the runtime's polyfill (objects throughout).
// Delegating means we no longer re-implement executeTool here, and `--via
// webmcp` genuinely exercises the standard-surface contract a real WebMCP client
// depends on — rather than reaching for the runtime's private global.
//
// Nothing here interprets the tool's steps: the page holds the compiled tool and
// reports its own result, guidance included. The cost is that the tool must be
// registered on the page first, and that the runtime clicks by dispatching
// synthetic events, which do not reach elements rendered into a portal (use
// `--via cli` for those).
//
// Requires the sightmap CLI to provide `browser mcp call` with native-argument
// serialization — sightmap >= 0.31.x including the native-args fix. The
// timeoutMs argument is unused here: the CLI bounds the call itself.
func (s *session) runWebMCP(toolName string, args map[string]any, _ int) (toolOutcome, error) {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return toolOutcome{}, err
	}

	// --json emits the raw CallToolResult envelope. Capture stdout independent of
	// the exit code: `mcp call` exits non-zero when the tool reports isError
	// (ok:false), which is a valid outcome we still want to parse and report,
	// distinct from being unable to run the tool at all (no result on stdout).
	cmd := exec.Command(s.sm, "browser", "mcp", "call", toolName,
		"--args", string(argsJSON), "--json", "--sightmap-dir", s.corpusDir)
	cmd.Dir = s.appDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()

	if body := strings.TrimSpace(stdout.String()); body != "" {
		return parseEnvelopeResult(body)
	}

	// No result payload — the tool could not be run (no WebMCP surface on the
	// page, the tool isn't registered on this view, ...). Surface sightmap's own
	// explanation, which already distinguishes those cases.
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return toolOutcome{}, errors.New(msg)
	}
	if runErr != nil {
		return toolOutcome{}, fmt.Errorf("run tool %q via mcp call: %w", toolName, runErr)
	}
	return toolOutcome{}, fmt.Errorf("mcp call %q returned no result", toolName)
}

// parseEnvelopeResult unwraps the runtime's ToolResult from a WebMCP
// CallToolResult envelope ({content:[{type,text}], isError}) as printed by
// `sightmap browser mcp call --json`, and maps it to a toolOutcome. The
// runtime encodes its ToolResult as the text of the first content part.
func parseEnvelopeResult(s string) (toolOutcome, error) {
	var env struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(s), &env); err != nil {
		return toolOutcome{}, fmt.Errorf("unexpected mcp call output %q: %w", s, err)
	}
	if len(env.Content) == 0 || strings.TrimSpace(env.Content[0].Text) == "" {
		return toolOutcome{}, fmt.Errorf("mcp call returned an empty result envelope: %q", s)
	}
	return parseWebMCPResult(env.Content[0].Text)
}

// webMCPResult is the result shape the runtime reports. items and value are
// pointers so an absent one stays distinguishable from an empty one.
type webMCPResult struct {
	OK       bool                 `json:"ok"`
	Value    *string              `json:"value"`
	Items    *[]map[string]string `json:"items"`
	Message  string               `json:"message"`
	Skipped  bool                 `json:"skipped"`
	Guidance []gen.Suggestion     `json:"guidance"`
}

func parseWebMCPResult(s string) (toolOutcome, error) {
	var res webMCPResult
	if err := json.Unmarshal([]byte(s), &res); err != nil {
		return toolOutcome{}, fmt.Errorf("unexpected tool result %q: %w", s, err)
	}
	out := toolOutcome{
		ok:       res.OK,
		message:  res.Message,
		skipped:  res.Skipped,
		value:    res.Value,
		guidance: res.Guidance,
	}
	if res.Items != nil {
		out.items = *res.Items
	}
	return out, nil
}
