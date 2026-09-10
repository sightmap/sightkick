package gen

import (
	"testing"

	sm "github.com/sightmap/sightmap/go/sightmap"
)

// signalCorpus has two views and one signal of each ref kind. Crucially,
// CheckoutBanner lives in the Checkout view, NOT in view V — so a tool scoped to
// ensure_view V that names the `checkout-reached` signal exercises the whole
// point of resolving signals against the WHOLE corpus (a post-condition names
// something in the destination view, out of the tool's own component scope).
func signalCorpus() *sm.Corpus {
	return &sm.Corpus{
		Views: []sm.ViewDef{
			{Name: "V", Route: "/", Components: []sm.ComponentDef{
				{Name: "ContinueButton", Selectors: []string{".continue"}},
			}},
			{Name: "Checkout", Route: "/checkout", Components: []sm.ComponentDef{
				{Name: "CheckoutBanner", Selectors: []string{".checkout-banner"}},
			}},
		},
		Signals: []sm.SignalDef{
			{Name: "checkout-reached", Ref: "CheckoutBanner"}, // component ref -> present-wait
			{Name: "on-checkout", Ref: "Checkout"},            // view ref -> route-wait
		},
	}
}

// oneWaitTool builds a manifest with a single wait_for tool scoped to view V.
func oneWaitTool(body StepBody) *Manifest {
	return &Manifest{
		Version: 1, Name: "t", Corpus: ".",
		Tools: []ToolDef{{
			Name: "go", Mode: "live", EnsureView: "V",
			Steps: []map[string]StepBody{{"wait_for": body}},
		}},
	}
}

// TestWaitForSignalComponent: `wait_for {signal}` whose ref names a component
// desugars to a present-wait carrying that component's selectors as the Query —
// identical to the `wait_for {query: Component}` form — even though the
// component lives outside the tool's ensure_view scope.
func TestWaitForSignalComponent(t *testing.T) {
	ir, diags := Compile(oneWaitTool(StepBody{Signal: "checkout-reached", TimeoutMs: 1234}), signalCorpus())
	if HasErrors(diags) {
		t.Fatalf("unexpected errors:\n%s", Format(diags))
	}
	step := ir.Tools[0].Steps[0]
	if step.Op != "waitFor" {
		t.Fatalf("op = %q, want waitFor", step.Op)
	}
	if step.Query == nil || len(step.Query.Parts) != 1 {
		t.Fatalf("query = %+v, want a single-part query", step.Query)
	}
	if got := step.Query.Parts[0].Locators; len(got) != 1 || got[0] != ".checkout-banner" {
		t.Errorf("locators = %v, want [.checkout-banner]", got)
	}
	if step.View != "" || step.Route != "" {
		t.Errorf("view/route = %q/%q, want empty for a component-ref signal", step.View, step.Route)
	}
	if step.TimeoutMs != 1234 {
		t.Errorf("timeoutMs = %d, want 1234 (passthrough)", step.TimeoutMs)
	}
}

// TestWaitForSignalView: `wait_for {signal}` whose ref names a view desugars to
// a route-wait (View+Route, no Query), identical to `wait_for {view}`.
func TestWaitForSignalView(t *testing.T) {
	ir, diags := Compile(oneWaitTool(StepBody{Signal: "on-checkout"}), signalCorpus())
	if HasErrors(diags) {
		t.Fatalf("unexpected errors:\n%s", Format(diags))
	}
	step := ir.Tools[0].Steps[0]
	if step.Op != "waitFor" || step.View != "Checkout" || step.Route != "/checkout" {
		t.Errorf("step = %+v, want waitFor Checkout //checkout", step)
	}
	if step.Query != nil {
		t.Errorf("query = %+v, want nil for a view-ref signal", step.Query)
	}
	if step.TimeoutMs != defaultWaitTimeoutMs {
		t.Errorf("timeoutMs = %d, want default %d", step.TimeoutMs, defaultWaitTimeoutMs)
	}
}

// TestWaitForSignalUnknown: naming a signal the corpus doesn't declare is an
// author typo, caught at compile time (not a runtime surprise).
func TestWaitForSignalUnknown(t *testing.T) {
	_, diags := Compile(oneWaitTool(StepBody{Signal: "nope"}), signalCorpus())
	if !hasDiag(diags, "compile.signal-unknown") {
		t.Errorf("expected compile.signal-unknown; got:\n%s", Format(diags))
	}
	if !HasErrors(diags) {
		t.Error("unknown signal must be an error")
	}
}

// TestWaitForShapeExactlyOneWithSignal: query/view/signal are three alternatives,
// not a combination — naming more than one is a shape error.
func TestWaitForShapeExactlyOneWithSignal(t *testing.T) {
	_, diags := Compile(oneWaitTool(StepBody{Query: "ContinueButton", Signal: "on-checkout"}), signalCorpus())
	if !hasDiag(diags, "compile.wait-for-shape") {
		t.Errorf("expected compile.wait-for-shape; got:\n%s", Format(diags))
	}
}

// TestThenPostConditionAppendsWait: a step's `then:` post-condition compiles to
// the action step FOLLOWED BY a signal wait, so the step completes only once the
// named signal holds — the signal-shaped completion predicate.
func TestThenPostConditionAppendsWait(t *testing.T) {
	m := &Manifest{
		Version: 1, Name: "t", Corpus: ".",
		Tools: []ToolDef{{
			Name: "cont", Mode: "live", EnsureView: "V",
			Steps: []map[string]StepBody{
				{"click": {Query: "ContinueButton", Then: "checkout-reached"}},
			},
		}},
	}
	ir, diags := Compile(m, signalCorpus())
	if HasErrors(diags) {
		t.Fatalf("unexpected errors:\n%s", Format(diags))
	}
	steps := ir.Tools[0].Steps
	if len(steps) != 2 {
		t.Fatalf("got %d steps, want 2 (click + appended wait)", len(steps))
	}
	if steps[0].Op != "click" {
		t.Errorf("step 0 op = %q, want click", steps[0].Op)
	}
	if steps[1].Op != "waitFor" || steps[1].Query == nil ||
		len(steps[1].Query.Parts[0].Locators) != 1 || steps[1].Query.Parts[0].Locators[0] != ".checkout-banner" {
		t.Errorf("step 1 = %+v, want waitFor on .checkout-banner", steps[1])
	}
}

// TestThenPostConditionInheritsWhen: an optional step and its post-condition must
// skip together — the appended wait carries the same When guard, so it never
// waits for a signal a skipped step could not have produced.
func TestThenPostConditionInheritsWhen(t *testing.T) {
	m := &Manifest{
		Version: 1, Name: "t", Corpus: ".",
		Tools: []ToolDef{{
			Name: "cont", Mode: "live", EnsureView: "V",
			Params: []ParamDef{{Name: "flag"}},
			Steps: []map[string]StepBody{
				{"click": {Query: "ContinueButton", Then: "checkout-reached", When: "{{flag}}"}},
			},
		}},
	}
	ir, diags := Compile(m, signalCorpus())
	if HasErrors(diags) {
		t.Fatalf("unexpected errors:\n%s", Format(diags))
	}
	steps := ir.Tools[0].Steps
	if len(steps) != 2 {
		t.Fatalf("got %d steps, want 2", len(steps))
	}
	if steps[0].When != "{{flag}}" || steps[1].When != "{{flag}}" {
		t.Errorf("when = %q / %q, want both {{flag}}", steps[0].When, steps[1].When)
	}
}
