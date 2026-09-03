package main

import (
	"testing"

	"sightkick/generator/internal/gen"

	sm "github.com/sightmap/sightmap/go/sightmap"
)

// fixtureSnapshotJSON is an annotated-tree fixture in the same shape
// `sightmap snapshot --json` writes (go/cmd/sightmap/cmd_snapshot_json.go):
// a root, a chain of OptionGroup > OptionButton pairs (for descendant-chain
// and predicate-op tests), and three MenuCard rows sharing a name (for
// substring-predicate and occurrence-index tests).
const fixtureSnapshotJSON = `{
  "tree": {
    "id": "root",
    "children": [
      {
        "id": "1",
        "component": "OptionGroup",
        "props": {"groupName": "Protein"},
        "children": [
          {"id": "2", "component": "OptionButton", "props": {"label": "Steak", "classes": "btn selected"}},
          {"id": "3", "component": "OptionButton", "props": {"label": "Chicken", "classes": "btn"}}
        ]
      },
      {"id": "4", "component": "MenuCard", "props": {"itemName": "Classic Burrito"}},
      {"id": "5", "component": "MenuCard", "props": {"itemName": "Bowl"}},
      {"id": "6", "component": "MenuCard", "props": {"itemName": "Veggie Bowl"}},
      {"id": "7", "component": "CardCvvField", "props": {}}
    ]
  }
}`

func TestResolveQueryAgainstSnapshot(t *testing.T) {
	cases := []struct {
		name    string
		query   string
		wantIDs []string
	}{
		{"bare component name", "CardCvvField", []string{"7"}},
		{"predicate exact op", `OptionButton[label=Steak]`, []string{"2"}},
		{"predicate prefix op", `MenuCard[itemName^=Bowl]`, []string{"5"}},
		{"predicate substring op", `MenuCard[itemName*=Bowl]`, []string{"5", "6"}},
		{"predicate case-insensitive", `OptionButton[label*=steak i]`, []string{"2"}},
		{"predicate case-sensitive miss", `OptionButton[label*=steak]`, nil},
		{"two-part descendant chain", `OptionGroup[groupName=Protein] OptionButton[label=Chicken]`, []string{"3"}},
		{"occurrence index selects one", `MenuCard[itemName*=Bowl]#1`, []string{"6"}},
		{"occurrence index out of range yields none", `MenuCard[itemName*=Bowl]#5`, nil},
		{"no match", `MenuCard[itemName=Nope]`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cands, _, err := resolveQueryAgainstSnapshot([]byte(fixtureSnapshotJSON), tc.query, nil)
			if err != nil {
				t.Fatalf("resolveQueryAgainstSnapshot(%q): %v", tc.query, err)
			}
			var gotIDs []string
			for _, c := range cands {
				gotIDs = append(gotIDs, c.Id)
			}
			if !idsEqual(gotIDs, tc.wantIDs) {
				t.Errorf("query %q: ids = %v, want %v", tc.query, gotIDs, tc.wantIDs)
			}
		})
	}
}

// TestResolveQueryAgainstSnapshot_Interpolation confirms a {{param}} inside a
// predicate value is substituted before the query is parsed — the same
// interpolation manifest fill/click/goto steps rely on for their own
// query/value/url strings.
func TestResolveQueryAgainstSnapshot_Interpolation(t *testing.T) {
	cands, _, err := resolveQueryAgainstSnapshot([]byte(fixtureSnapshotJSON), "MenuCard[itemName*={{item}}]", map[string]any{"item": "Classic"})
	if err != nil {
		t.Fatalf("resolveQueryAgainstSnapshot: %v", err)
	}
	if len(cands) != 1 || cands[0].Id != "4" {
		t.Errorf("candidates = %v, want [id=4]", cands)
	}
}

func idsEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestGuardHoldsForCandidates: the present/absent truth table against an
// already-resolved candidate set, independent of how those candidates were
// found (no snapshot or live session involved).
func TestGuardHoldsForCandidates(t *testing.T) {
	one := []*sm.ComponentNode{{Id: "x"}}
	var none []*sm.ComponentNode

	cases := []struct {
		name  string
		guard *gen.GuardBody
		cands []*sm.ComponentNode
		want  bool
	}{
		{"present holds on a match", &gen.GuardBody{Present: &gen.GuardRef{Query: "Q"}}, one, true},
		{"present does not hold with no match", &gen.GuardBody{Present: &gen.GuardRef{Query: "Q"}}, none, false},
		{"absent holds with no match", &gen.GuardBody{Absent: &gen.GuardRef{Query: "Q"}}, none, true},
		{"absent does not hold on a match", &gen.GuardBody{Absent: &gen.GuardRef{Query: "Q"}}, one, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := guardHoldsForCandidates(tc.guard, tc.cands)
			if err != nil {
				t.Fatalf("guardHoldsForCandidates: %v", err)
			}
			if got != tc.want {
				t.Errorf("holds = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGuardQueryMalformed(t *testing.T) {
	if _, _, err := guardQuery(&gen.GuardBody{}); err == nil {
		t.Error("expected an error for a guard with neither present nor absent")
	}
}

// TestValueFromCandidates: a returns.value picks the property off the FIRST
// candidate when several match, rather than erroring on ambiguity the way
// compquery.Resolve would — matching the runtime's resolveQuery(...)[0].
func TestValueFromCandidates(t *testing.T) {
	first := &sm.ComponentNode{Id: "a"}
	second := &sm.ComponentNode{Id: "b"}
	props := map[string]map[string]string{
		"a": {"total": "$10.00"},
		"b": {"total": "$99.99"},
	}
	got := valueFromCandidates([]*sm.ComponentNode{first, second}, props, "total")
	if got == nil || *got != "$10.00" {
		t.Errorf("value = %v, want $10.00 (from the first candidate)", got)
	}
}

func TestValueFromCandidates_NoMatch(t *testing.T) {
	got := valueFromCandidates(nil, nil, "total")
	if got != nil {
		t.Errorf("value = %v, want nil (no candidate -> value stays absent, not \"\")", *got)
	}
}

// TestListFromCandidates_EmptyIsNotNil: a list return with zero rows must
// still serialize as `items: []`, not omit the key — mirroring the
// runtime's `rows.map(...)`, which yields [] on an empty array.
func TestListFromCandidates_EmptyIsNotNil(t *testing.T) {
	items := listFromCandidates(nil, nil, map[string]gen.FieldDef{"x": {Property: "x"}})
	if items == nil {
		t.Fatal("items = nil, want a non-nil empty slice")
	}
	if len(items) != 0 {
		t.Errorf("items = %v, want empty", items)
	}
}

func TestListFromCandidates_OneRowPerCandidate(t *testing.T) {
	a := &sm.ComponentNode{Id: "a"}
	b := &sm.ComponentNode{Id: "b"}
	props := map[string]map[string]string{
		"a": {"errorId": "payment-declined", "message": "Card declined"},
		"b": {"errorId": "promo-invalid", "message": "Invalid code"},
	}
	fields := map[string]gen.FieldDef{
		"errorId": {Property: "errorId"},
		"message": {Property: "message"},
	}
	items := listFromCandidates([]*sm.ComponentNode{a, b}, props, fields)
	if len(items) != 2 {
		t.Fatalf("items = %v, want 2 rows", items)
	}
	if items[0]["errorId"] != "payment-declined" || items[1]["errorId"] != "promo-invalid" {
		t.Errorf("items = %v, want row order to follow candidate order", items)
	}
}

// TestStepOp mirrors gen's own manifest_test coverage of stepOp's shape
// rules for the local copy this package needs (gen.stepOp is unexported).
func TestStepOp(t *testing.T) {
	op, body, ok := stepOp(map[string]gen.StepBody{"click": {Query: "Q"}})
	if !ok || op != "click" || body.Query != "Q" {
		t.Errorf("stepOp = (%q, %+v, %v), want (click, {Query:Q}, true)", op, body, ok)
	}
	if _, _, ok := stepOp(map[string]gen.StepBody{}); ok {
		t.Error("stepOp on an empty map: ok = true, want false")
	}
	if _, _, ok := stepOp(map[string]gen.StepBody{"click": {}, "fill": {}}); ok {
		t.Error("stepOp on a two-key map: ok = true, want false")
	}
}

func TestInterpolateStr(t *testing.T) {
	cases := []struct {
		name string
		s    string
		args map[string]any
		want string
	}{
		{"substitutes a string param", "MenuCard[itemName*={{item}}]", map[string]any{"item": "Bowl"}, "MenuCard[itemName*=Bowl]"},
		{"renders a non-string param with its default string form", "count={{n}}", map[string]any{"n": float64(3)}, "count=3"},
		{"missing param becomes empty string", "x={{missing}}", map[string]any{}, "x="},
		{"nil param becomes empty string", "x={{k}}", map[string]any{"k": nil}, "x="},
		{"no placeholder is a no-op", "AddToCartButton", map[string]any{"item": "Bowl"}, "AddToCartButton"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := interpolateStr(tc.s, tc.args); got != tc.want {
				t.Errorf("interpolateStr(%q) = %q, want %q", tc.s, got, tc.want)
			}
		})
	}
}
