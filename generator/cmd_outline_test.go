package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"sightkick/generator/internal/gen"
)

// goldenNow is the fixed clock golden renders are generated against, so the
// banner's embedded date doesn't make every run regenerate the golden.
var goldenNow = time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

// checkGolden compares got against the named testdata file, regenerating it
// when UPDATE_GOLDEN=1 is set — the same convention as
// internal/gen/gen_test.go's IR goldens.
func checkGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated golden %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with UPDATE_GOLDEN=1 to create): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("output does not match golden %s.\n--- got ---\n%s", path, got)
	}
}

func TestOutlineGoldenTodo(t *testing.T) {
	o, diags, err := gen.BuildOutline("../examples/todo")
	if err != nil || gen.HasErrors(diags) {
		t.Fatalf("BuildOutline failed: err=%v diags=%s", err, gen.Format(diags))
	}
	var buf bytes.Buffer
	renderOutline(&buf, o.Brief(), goldenNow)
	checkGolden(t, "testdata/todo.outline.txt", buf.Bytes())
}

func TestOutlineGoldenSaucedemo(t *testing.T) {
	o, diags, err := gen.BuildOutline("../examples/saucedemo")
	if err != nil || gen.HasErrors(diags) {
		t.Fatalf("BuildOutline failed: err=%v diags=%s", err, gen.Format(diags))
	}
	var buf bytes.Buffer
	renderOutline(&buf, o.Brief(), goldenNow)
	checkGolden(t, "testdata/saucedemo.outline.txt", buf.Bytes())
}

func TestExplainGoldenSaucedemoPurchase(t *testing.T) {
	full, diags, err := gen.BuildOutline("../examples/saucedemo")
	if err != nil || gen.HasErrors(diags) {
		t.Fatalf("BuildOutline failed: err=%v diags=%s", err, gen.Format(diags))
	}
	selected, err := full.Select(gen.Selector{Journeys: []string{"purchase"}})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	renderExplain(&buf, full, selected, goldenNow)
	checkGolden(t, "testdata/saucedemo.purchase.explain.txt", buf.Bytes())
}

// TestOutlineJSONIsTiered locks in that --json (no selector) never carries
// explain-only per-tool fields — the machine form is tiered exactly like the
// text form, per Outline.Brief's doc. Checked on the parsed struct, not raw
// JSON text: a journey's own Description is legitimate at every tier (it's
// the one-line gloss `outline` always shows), so a bare string-contains
// check on `"description"` would false-positive on that.
func TestOutlineJSONIsTiered(t *testing.T) {
	o, diags, err := gen.BuildOutline("../examples/todo")
	if err != nil || gen.HasErrors(diags) {
		t.Fatalf("BuildOutline failed: err=%v diags=%s", err, gen.Format(diags))
	}
	data, err := json.Marshal(o.Brief())
	if err != nil {
		t.Fatal(err)
	}
	var round gen.Outline
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatal(err)
	}
	for _, tool := range round.Tools {
		if tool.Description != "" || tool.Mode != "" || tool.Params != nil || tool.Returns != nil {
			t.Errorf("brief tool %q carries explain-only detail: %+v", tool.Name, tool)
		}
	}
}

// TestExplainJSONFailureEnvelope locks in the sightmap `stats --json`
// contract: a broken manifest still prints exactly one JSON object, with a
// present "error" key, rather than a bare human error a --json caller (which
// parses stdout unconditionally) would have nothing to read.
func TestExplainJSONFailureEnvelope(t *testing.T) {
	dir := t.TempDir()
	skDir := dir + "/.sightkick"
	smDir := dir + "/.sightmap"
	if err := os.MkdirAll(skDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(smDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tools := `version: 1
name: broken
tools:
  - name: foo
    ensure_view: NoSuchView
    steps:
      - click:
          query: Whatever
`
	views := `version: 1
views:
  - name: Home
    route: /
`
	if err := os.WriteFile(skDir+"/tools.yaml", []byte(tools), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(smDir+"/views.yaml", []byte(views), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	_, ok, err := buildOutline(&out, dir, true)
	if ok {
		t.Fatal("buildOutline(broken manifest, json=true) unexpectedly ok")
	}
	if err != errPrinted {
		t.Errorf("buildOutline error = %v, want errPrinted (the JSON envelope already reported it)", err)
	}

	var got jsonFailure
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("failure output is not one JSON object: %v\n%s", err, out.String())
	}
	if got.Error == "" {
		t.Errorf("failure envelope has no \"error\" key: %s", out.String())
	}
	if len(got.Diagnostics) == 0 {
		t.Errorf("failure envelope carries no diagnostics: %s", out.String())
	}
}
