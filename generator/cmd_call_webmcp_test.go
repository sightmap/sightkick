package main

import (
	"encoding/json"
	"testing"
)

// TestParseEnvelopeResult: `--via webmcp` now delegates to `sightmap browser mcp
// call --json`, which prints the CallToolResult envelope. The runtime encodes its
// ToolResult as the text of the first content part; parseEnvelopeResult must
// unwrap that back into a toolOutcome.
func TestParseEnvelopeResult(t *testing.T) {
	toolResult := `{"ok":true,"value":"42","guidance":[{"tool":"list_results","reason":"see them"}]}`
	env, _ := json.Marshal(map[string]any{
		"content": []map[string]any{{"type": "text", "text": toolResult}},
		"isError": false,
	})
	out, err := parseEnvelopeResult(string(env))
	if err != nil {
		t.Fatalf("parseEnvelopeResult: %v", err)
	}
	if !out.ok {
		t.Error("ok = false, want true")
	}
	if out.value == nil || *out.value != "42" {
		t.Errorf("value = %v, want 42", out.value)
	}
	if len(out.guidance) != 1 || out.guidance[0].Tool != "list_results" {
		t.Errorf("guidance = %+v, want one entry for list_results", out.guidance)
	}
}

// An isError envelope still carries the ToolResult (ok:false) — mcp call exits
// non-zero, but the payload is a valid, parseable outcome.
func TestParseEnvelopeResult_ToolFailure(t *testing.T) {
	env, _ := json.Marshal(map[string]any{
		"content": []map[string]any{{"type": "text", "text": `{"ok":false,"message":"nope"}`}},
		"isError": true,
	})
	out, err := parseEnvelopeResult(string(env))
	if err != nil {
		t.Fatalf("parseEnvelopeResult: %v", err)
	}
	if out.ok || out.message != "nope" {
		t.Errorf("outcome = %+v, want ok:false message:nope", out)
	}
}

func TestParseEnvelopeResult_Empty(t *testing.T) {
	if _, err := parseEnvelopeResult(`{"content":[],"isError":false}`); err == nil {
		t.Error("want an error for an empty content envelope")
	}
	if _, err := parseEnvelopeResult(`not json`); err == nil {
		t.Error("want an error for non-JSON output")
	}
}
