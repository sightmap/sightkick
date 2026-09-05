package gen

import (
	"testing"

	sm "github.com/sightmap/sightmap/go/sightmap"
)

// TestEnsureViewlessResolvesViewScopedComponent is the minimal repro from
// sightkick#9: a tool that omits ensure_view references a component that is
// declared on a view (not a global). Before the fix, view == nil dropped every
// view-scoped component from the candidate set, so the query failed
// compile.query-ref with "Available: (none)" — even though `sightmap validate`
// was happy. An ensure_view-less tool is not scoped to a route, so it must
// resolve component names against the whole corpus.
func TestEnsureViewlessResolvesViewScopedComponent(t *testing.T) {
	c := oneViewCorpus(
		sm.ComponentDef{Name: "Row", Selectors: []string{".row"}, Properties: []sm.ComponentPropertyDef{
			prop("title", "text"),
		}},
	)
	// Same tool as listOverRow but with NO ensure_view.
	m := &Manifest{
		Version: 1, Name: "t", Corpus: ".",
		Tools: []ToolDef{{
			Name: "list", Mode: "live",
			Returns: &ReturnDef{List: &ListRef{Rows: "Row", Fields: map[string]FieldDef{
				"title": {Property: "title"},
			}}},
		}},
	}

	ir, diags := Compile(m, c)
	if HasErrors(diags) {
		t.Fatalf("ensure_view-less tool should resolve a view-scoped component, got:\n%s", Format(diags))
	}
	if hasDiag(diags, "compile.query-ref") {
		t.Fatalf("unexpected compile.query-ref for a validly-declared component:\n%s", Format(diags))
	}
	if got := ir.Tools[0].Returns.Query.Parts[0].Locators[0]; got != ".row" {
		t.Errorf("Row locator = %q, want %q", got, ".row")
	}
	// ensure_view was omitted, so the compiled tool carries no scoping hint.
	if ir.Tools[0].EnsureView != nil {
		t.Errorf("EnsureView = %+v, want nil (tool omitted ensure_view)", ir.Tools[0].EnsureView)
	}
}

// TestEnsureViewlessResolvesAcrossMultipleViews confirms the whole-corpus scope
// spans every view, not just the first: an ensure_view-less tool resolves
// components declared on different views in one corpus.
func TestEnsureViewlessResolvesAcrossMultipleViews(t *testing.T) {
	c := &sm.Corpus{Views: []sm.ViewDef{
		{Name: "List", Route: "/", Components: []sm.ComponentDef{
			{Name: "Row", Selectors: []string{".row"}, Properties: []sm.ComponentPropertyDef{prop("title", "text")}},
		}},
		{Name: "Detail", Route: "/item/:id", Components: []sm.ComponentDef{
			{Name: "Card", Selectors: []string{".card"}, Properties: []sm.ComponentPropertyDef{prop("name", "text")}},
		}},
	}}
	m := &Manifest{
		Version: 1, Name: "t", Corpus: ".",
		Tools: []ToolDef{
			{Name: "rows", Mode: "live", Returns: &ReturnDef{List: &ListRef{
				Rows: "Row", Fields: map[string]FieldDef{"title": {Property: "title"}},
			}}},
			{Name: "cards", Mode: "live", Returns: &ReturnDef{List: &ListRef{
				Rows: "Card", Fields: map[string]FieldDef{"name": {Property: "name"}},
			}}},
		},
	}
	_, diags := Compile(m, c)
	if HasErrors(diags) {
		t.Fatalf("components from different views should both resolve, got:\n%s", Format(diags))
	}
}

// TestEnsureViewStillScopes locks in that ensure_view is unchanged: when a tool
// names a view, the compiled tool still carries the scoping/route hint and the
// query resolves within that view.
func TestEnsureViewStillScopes(t *testing.T) {
	c := oneViewCorpus(
		sm.ComponentDef{Name: "Row", Selectors: []string{".row"}, Properties: []sm.ComponentPropertyDef{prop("title", "text")}},
	)
	ir, diags := Compile(listOverRow("Row", "title"), c) // listOverRow sets EnsureView: "V"
	if HasErrors(diags) {
		t.Fatalf("unexpected errors:\n%s", Format(diags))
	}
	ev := ir.Tools[0].EnsureView
	if ev == nil || ev.View != "V" || ev.Route != "/" {
		t.Errorf("EnsureView = %+v, want {View:V Route:/}", ev)
	}
}
