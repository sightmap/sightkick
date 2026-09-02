package gen

import (
	"testing"

	sm "github.com/sightmap/sightmap/go/sightmap"
)

// oneViewCorpus wraps flattened component defs in a single "/" view.
func oneViewCorpus(comps ...sm.ComponentDef) *sm.Corpus {
	return &sm.Corpus{Views: []sm.ViewDef{{Name: "V", Route: "/", Components: comps}}}
}

// listOverRow builds a manifest with a single list tool over rows=`row`
// returning each named property as a like-named field.
func listOverRow(row string, fields ...string) *Manifest {
	fm := map[string]FieldDef{}
	for _, f := range fields {
		fm[f] = FieldDef{Property: f}
	}
	return &Manifest{
		Version: 1, Name: "t", Corpus: ".",
		Tools: []ToolDef{{
			Name: "list", Mode: "live", EnsureView: "V",
			Returns: &ReturnDef{List: &ListRef{Rows: row, Fields: fm}},
		}},
	}
}

func prop(name, extract string) sm.ComponentPropertyDef {
	return sm.ComponentPropertyDef{Name: name, Extract: extract}
}

func hasDiag(diags []Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

// TestCompPropResolution locks in the SEP-0010 Comp.prop resolution: a
// PATH.prop extract compiles to the child component's selector expressed
// RELATIVE to its owner, carrying the leaf's kind. This is the path the
// todo/search goldens never exercise (they use raw-CSS extracts).
func TestCompPropResolution(t *testing.T) {
	c := oneViewCorpus(
		sm.ComponentDef{Name: "Row", Selectors: []string{".row"}, Properties: []sm.ComponentPropertyDef{
			prop("title", "Title.text"),     // single segment, single selector
			prop("code", "Badge.id"),        // leaf is attr=, carried through with within
			prop("multi", "Multi.text"),     // child has two selectors -> comma list
			prop("deep", "Wrap.Inner.text"), // multi-segment descendant chain
			prop("raw", ".legacy-css"),      // legacy raw-CSS extract, used verbatim
		}},
		sm.ComponentDef{Name: "Title", ParentChain: []string{"Row"}, Selectors: []string{".row .title"}, Properties: []sm.ComponentPropertyDef{prop("text", "text")}},
		sm.ComponentDef{Name: "Badge", ParentChain: []string{"Row"}, Selectors: []string{".row .badge"}, Properties: []sm.ComponentPropertyDef{prop("id", "attr=data-id")}},
		sm.ComponentDef{Name: "Multi", ParentChain: []string{"Row"}, Selectors: []string{".row .m", ".row .n"}, Properties: []sm.ComponentPropertyDef{prop("text", "text")}},
		sm.ComponentDef{Name: "Wrap", ParentChain: []string{"Row"}, Selectors: []string{".row .wrap"}, Properties: []sm.ComponentPropertyDef{prop("x", "text")}},
		sm.ComponentDef{Name: "Inner", ParentChain: []string{"Row", "Wrap"}, Selectors: []string{".row .wrap .inner"}, Properties: []sm.ComponentPropertyDef{prop("text", "text")}},
	)

	ir, diags := Compile(listOverRow("Row", "title", "code", "multi", "deep", "raw"), c)
	if HasErrors(diags) {
		t.Fatalf("unexpected errors:\n%s", Format(diags))
	}
	// A well-formed corpus like this must not warn (in particular the legacy
	// raw-CSS extract must resolve silently, not trip the Comp.prop warnings).
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics, got:\n%s", Format(diags))
	}

	got := ir.Tools[0].Returns.Fields
	want := map[string]Extractor{
		"title": {Kind: "text", Within: ".title"},
		"code":  {Kind: "attr", Attr: "data-id", Within: ".badge"},
		"multi": {Kind: "text", Within: ".m, .n"},
		"deep":  {Kind: "text", Within: ".wrap .inner"},
		"raw":   {Kind: "text", Within: ".legacy-css"},
	}
	for name, w := range want {
		if g := got[name].Extractor; g != w {
			t.Errorf("field %q extractor = %+v, want %+v", name, g, w)
		}
	}
}

// TestCompPropResolvesInPredicates confirms the SAME resolution powers a
// compquery predicate (selection), not just a returns field.
func TestCompPropResolvesInPredicates(t *testing.T) {
	c := oneViewCorpus(
		sm.ComponentDef{Name: "Card", Selectors: []string{".card"}, Properties: []sm.ComponentPropertyDef{prop("no", "Code.text")}},
		sm.ComponentDef{Name: "Code", ParentChain: []string{"Card"}, Selectors: []string{".card .code"}, Properties: []sm.ComponentPropertyDef{prop("text", "text")}},
	)
	m := &Manifest{Version: 1, Name: "t", Corpus: ".", Tools: []ToolDef{{
		Name: "pick", Mode: "live", EnsureView: "V",
		Steps: []map[string]StepBody{{"click": {Query: `Card[no*="B6"]`}}},
	}}}
	ir, diags := Compile(m, c)
	if HasErrors(diags) {
		t.Fatalf("unexpected errors:\n%s", Format(diags))
	}
	pred := ir.Tools[0].Steps[0].Query.Parts[0].Preds[0]
	if want := (Extractor{Kind: "text", Within: ".code"}); pred.Extractor != want {
		t.Errorf("predicate extractor = %+v, want %+v", pred.Extractor, want)
	}
}

// TestCompPropCycleGuard: a cyclic Comp.prop chain (A.x -> B.y -> A.x) must
// terminate with an error, not recurse forever.
func TestCompPropCycleGuard(t *testing.T) {
	c := oneViewCorpus(
		sm.ComponentDef{Name: "A", Selectors: []string{".a"}, Properties: []sm.ComponentPropertyDef{prop("x", "B.y")}},
		sm.ComponentDef{Name: "B", ParentChain: []string{"A"}, Selectors: []string{".a .b"}, Properties: []sm.ComponentPropertyDef{prop("y", "A.x")}},
	)
	m := &Manifest{Version: 1, Name: "t", Corpus: ".", Tools: []ToolDef{{
		Name: "v", Mode: "live", EnsureView: "V",
		Returns: &ReturnDef{Value: &ValueRef{Query: "A", Property: "x"}},
	}}}
	_, diags := Compile(m, c) // must return, not hang
	if !hasDiag(diags, "compile.extract-cycle") {
		t.Fatalf("expected compile.extract-cycle; got:\n%s", Format(diags))
	}
}

// TestCompPropPrefixFallbackWarns: when a child's flattened selector is not a
// descendant of its owner (flattening didn't prepend it), relativization can't
// be done cleanly and we warn rather than emit a silently-wrong selector.
func TestCompPropPrefixFallbackWarns(t *testing.T) {
	c := oneViewCorpus(
		sm.ComponentDef{Name: "Row", Selectors: []string{".row"}, Properties: []sm.ComponentPropertyDef{prop("weird", "Weird.text")}},
		sm.ComponentDef{Name: "Weird", ParentChain: []string{"Row"}, Selectors: []string{"#detached"}, Properties: []sm.ComponentPropertyDef{prop("text", "text")}},
	)
	_, diags := Compile(listOverRow("Row", "weird"), c)
	if !hasDiag(diags, "compile.extract-relsel") {
		t.Fatalf("expected compile.extract-relsel warning; got:\n%s", Format(diags))
	}
}
