package gen

import (
	"testing"

	sm "github.com/sightmap/sightmap/go/sightmap"
)

// twoViewCorpus is oneViewCorpus's sibling for tests that need a second, named
// navigation target (wait_for's view: form has nothing to resolve against a
// single-view fixture).
func twoViewCorpus() *sm.Corpus {
	return &sm.Corpus{Views: []sm.ViewDef{
		{Name: "V", Route: "/"},
		{Name: "Other", Route: "/other"},
	}}
}

func waitForManifest(body StepBody) *Manifest {
	return &Manifest{
		Version: 1, Name: "t", Corpus: ".",
		Tools: []ToolDef{{
			Name: "go", Mode: "live", EnsureView: "V",
			Steps: []map[string]StepBody{{"wait_for": body}},
		}},
	}
}

// TestWaitForView: wait_for's `view:` form is an alternative to `query:` for a
// tool whose last act navigates away and has no view-scoped content of its own
// to wait on — it resolves the named view and compiles to a Step carrying
// View+Route instead of a Query, mirroring `navigate`'s own resolution.
func TestWaitForView(t *testing.T) {
	ir, diags := Compile(waitForManifest(StepBody{View: "Other", TimeoutMs: 2000}), twoViewCorpus())
	if HasErrors(diags) {
		t.Fatalf("unexpected errors:\n%s", Format(diags))
	}
	step := ir.Tools[0].Steps[0]
	if step.Op != "waitFor" {
		t.Fatalf("op = %q, want waitFor", step.Op)
	}
	if step.View != "Other" || step.Route != "/other" {
		t.Errorf("view/route = %q/%q, want Other//other", step.View, step.Route)
	}
	if step.Query != nil {
		t.Errorf("query = %+v, want nil for the view: form", step.Query)
	}
	if step.TimeoutMs != 2000 {
		t.Errorf("timeoutMs = %d, want 2000", step.TimeoutMs)
	}
}

// TestWaitForViewUnknown: an unresolvable view name is a compile error, not a
// runtime surprise — same treatment as navigate's own unknown-view check.
func TestWaitForViewUnknown(t *testing.T) {
	_, diags := Compile(waitForManifest(StepBody{View: "Nope"}), twoViewCorpus())
	if !hasDiag(diags, "compile.wait-for-view") {
		t.Errorf("expected compile.wait-for-view; got:\n%s", Format(diags))
	}
	if !HasErrors(diags) {
		t.Error("unknown wait_for view must be an error")
	}
}

// TestWaitForShapeExactlyOne: query and view are alternatives, not a pair — a
// step naming both or neither is a manifest error caught at compile time.
func TestWaitForShapeExactlyOne(t *testing.T) {
	t.Run("both", func(t *testing.T) {
		_, diags := Compile(waitForManifest(StepBody{Query: "Foo", View: "Other"}), twoViewCorpus())
		if !hasDiag(diags, "compile.wait-for-shape") {
			t.Errorf("expected compile.wait-for-shape; got:\n%s", Format(diags))
		}
	})
	t.Run("neither", func(t *testing.T) {
		_, diags := Compile(waitForManifest(StepBody{}), twoViewCorpus())
		if !hasDiag(diags, "compile.wait-for-shape") {
			t.Errorf("expected compile.wait-for-shape; got:\n%s", Format(diags))
		}
	})
}

func keypressManifest(body StepBody) *Manifest {
	return &Manifest{
		Version: 1, Name: "t", Corpus: ".",
		Tools: []ToolDef{{
			Name: "press", Mode: "live", EnsureView: "V",
			Steps: []map[string]StepBody{{"keypress": body}},
		}},
	}
}

// TestKeypress: keypress has no query of its own (targets whatever's
// focused) — it compiles straight to a Step carrying just the key.
func TestKeypress(t *testing.T) {
	ir, diags := Compile(keypressManifest(StepBody{Key: "Enter"}), oneViewCorpus())
	if HasErrors(diags) {
		t.Fatalf("unexpected errors:\n%s", Format(diags))
	}
	step := ir.Tools[0].Steps[0]
	if step.Op != "keypress" || step.Key != "Enter" || step.Query != nil {
		t.Errorf("step = %+v, want {op:keypress key:Enter query:nil}", step)
	}
}

// TestKeypressMissingKey: an empty key is a compile error, not a silent no-op.
func TestKeypressMissingKey(t *testing.T) {
	_, diags := Compile(keypressManifest(StepBody{}), oneViewCorpus())
	if !hasDiag(diags, "compile.keypress") {
		t.Errorf("expected compile.keypress; got:\n%s", Format(diags))
	}
}
