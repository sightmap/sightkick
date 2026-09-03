package main

import (
	"reflect"
	"slices"
	"testing"

	"sightkick/generator/internal/gen"
)

// fixtureSnapshot is a `sightmap snapshot --json` document: a tree of page
// elements, each tagged with the corpus component it matched and that
// component's extracted properties. It holds an OptionGroup wrapping two
// OptionButtons (for scoping and predicate cases), three MenuCards with
// overlapping names (for substring and Nth-match cases), and one component
// with no properties at all.
const fixtureSnapshot = `{
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
      {"id": "7", "component": "CardCvvField"}
    ]
  }
}`

// TestFindInSnapshot covers resolving a component query against a snapshot:
// the query grammar (names, each predicate operator, case-insensitivity,
// descendant scoping, Nth-match), param substitution into a predicate value,
// and the cases that legitimately match nothing.
func TestFindInSnapshot(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		args    map[string]any
		want    []string // ids, in document order
		wantErr bool
	}{
		{name: "component name alone", query: "CardCvvField", want: []string{"7"}},
		{name: "component with no matches", query: "MenuCard[itemName=Nope]"},

		{name: "= matches the whole value", query: "OptionButton[label=Steak]", want: []string{"2"}},
		{name: "= rejects a partial value", query: "OptionButton[label=Stea]"},
		{name: "^= matches a prefix", query: "MenuCard[itemName^=Bowl]", want: []string{"5"}},
		{name: "*= matches anywhere, keeping document order", query: "MenuCard[itemName*=Bowl]", want: []string{"5", "6"}},
		{name: "predicates are case-sensitive by default", query: "OptionButton[label*=steak]"},
		{name: "the i flag makes a predicate case-insensitive", query: "OptionButton[label*=steak i]", want: []string{"2"}},
		{name: "several predicates all have to hold", query: "OptionButton[label=Steak][classes*=selected]", want: []string{"2"}},
		{name: "a predicate on a missing property matches nothing", query: "CardCvvField[value=x]"},

		{name: "a descendant chain scopes to the ancestor", query: "OptionGroup[groupName=Protein] OptionButton[label=Chicken]", want: []string{"3"}},
		{name: "a chain whose ancestor does not match finds nothing", query: "OptionGroup[groupName=Rice] OptionButton[label=Chicken]"},

		{name: "#N picks one of several matches", query: "MenuCard[itemName*=Bowl]#1", want: []string{"6"}},
		{name: "#N past the end matches nothing rather than erroring", query: "MenuCard[itemName*=Bowl]#5"},

		{name: "a param substitutes into a predicate value", query: "MenuCard[itemName*={{item}}]", args: map[string]any{"item": "Classic"}, want: []string{"4"}},
		// An unsupplied param empties the predicate, and an empty predicate
		// value is malformed — so a forgotten param fails loudly here instead
		// of quietly matching every element.
		{name: "an unsupplied param in a predicate is an error", query: "MenuCard[itemName*={{item}}]", wantErr: true},

		{name: "a malformed query is an error", query: "MenuCard[unterminated", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			found, err := findInSnapshot([]byte(fixtureSnapshot), tc.query, tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("findInSnapshot(%q) succeeded, want an error", tc.query)
				}
				return
			}
			if err != nil {
				t.Fatalf("findInSnapshot(%q): %v", tc.query, err)
			}
			var got []string
			for _, node := range found {
				got = append(got, node.Id)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("findInSnapshot(%q) matched ids %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

// TestStepArgs covers translating each manifest step into the sightmap command
// that performs it, including param substitution, the wait_for timeout
// default, and the step shapes that are not runnable.
func TestStepArgs(t *testing.T) {
	const defaultTimeout = 5000
	args := map[string]any{"item": "Bowl", "org": "local"}

	tests := []struct {
		name    string
		op      string
		body    gen.StepBody
		want    []string // nil means "no command to run"
		wantErr bool
	}{
		{
			name: "click substitutes params into the query",
			op:   "click", body: gen.StepBody{Query: "MenuCard[itemName*={{item}}]"},
			want: []string{"browser", "click", "MenuCard[itemName*=Bowl]"},
		},
		{
			name: "fill substitutes into both query and value, and replaces existing text",
			op:   "fill", body: gen.StepBody{Query: "SearchInput", Value: "{{item}} please"},
			want: []string{"browser", "fill", "SearchInput", "Bowl please", "--clear"},
		},
		{
			name: "goto substitutes params into the URL",
			op:   "goto", body: gen.StepBody{URL: "https://example.test/ui/{{org}}/library"},
			want: []string{"browser", "navigate", "https://example.test/ui/local/library"},
		},
		{
			name: "keypress passes the key through and names no element",
			op:   "keypress", body: gen.StepBody{Key: "Enter"},
			want: []string{"browser", "keypress", "Enter"},
		},
		{
			name: "navigate runs no command",
			op:   "navigate", body: gen.StepBody{View: "Cart"},
			want: nil,
		},
		{
			name: "wait_for on a query uses the caller's default timeout",
			op:   "wait_for", body: gen.StepBody{Query: "CartTable"},
			want: []string{"browser", "wait-for", "--component", "CartTable", "--timeout-ms", "5000"},
		},
		{
			name: "wait_for on a query honours its own timeout",
			op:   "wait_for", body: gen.StepBody{Query: "CartTable", TimeoutMs: 250},
			want: []string{"browser", "wait-for", "--component", "CartTable", "--timeout-ms", "250"},
		},
		{
			name: "wait_for on a query substitutes params",
			op:   "wait_for", body: gen.StepBody{Query: "MenuCard[itemName*={{item}}]"},
			want: []string{"browser", "wait-for", "--component", "MenuCard[itemName*=Bowl]", "--timeout-ms", "5000"},
		},
		{
			name: "wait_for on a view waits on the URL instead",
			op:   "wait_for", body: gen.StepBody{View: "ItemDetail", TimeoutMs: 2000},
			want: []string{"browser", "wait-for", "--view", "ItemDetail", "--timeout-ms", "2000"},
		},
		{
			name: "wait_for naming both a view and a query is an error",
			op:   "wait_for", body: gen.StepBody{View: "ItemDetail", Query: "CartTable"}, wantErr: true,
		},
		{
			name: "wait_for naming neither is an error",
			op:   "wait_for", body: gen.StepBody{}, wantErr: true,
		},
		{
			name: "an unknown op is an error",
			op:   "teleport", body: gen.StepBody{}, wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := stepArgs(tc.op, tc.body, args, defaultTimeout)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("stepArgs(%q) = %v, want an error", tc.op, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("stepArgs(%q): %v", tc.op, err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("stepArgs(%q) = %v, want %v", tc.op, got, tc.want)
			}
		})
	}
}

// TestGuardQuery covers reading a guard's query and the match count it
// expects. A guard makes a tool idempotent: it holds when the tool's effect is
// already applied, and the tool then skips its steps.
func TestGuardQuery(t *testing.T) {
	tests := []struct {
		name          string
		guard         gen.GuardBody
		wantQuery     string
		wantMatch     bool
		wantErr       bool
		holdsWhenNone bool // what guardHolds concludes with zero matches
	}{
		{
			name:      "present expects a match",
			guard:     gen.GuardBody{Present: &gen.GuardRef{Query: "StarButton[label=Unstar]"}},
			wantQuery: "StarButton[label=Unstar]", wantMatch: true, holdsWhenNone: false,
		},
		{
			name:      "absent expects no match",
			guard:     gen.GuardBody{Absent: &gen.GuardRef{Query: "ConfirmDialog"}},
			wantQuery: "ConfirmDialog", wantMatch: false, holdsWhenNone: true,
		},
		{
			name:  "declaring neither is an error",
			guard: gen.GuardBody{}, wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			query, wantMatch, err := guardQuery(&tc.guard)
			if tc.wantErr {
				if err == nil {
					t.Fatal("guardQuery succeeded, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("guardQuery: %v", err)
			}
			if query != tc.wantQuery {
				t.Errorf("query = %q, want %q", query, tc.wantQuery)
			}
			if wantMatch != tc.wantMatch {
				t.Errorf("wantMatch = %v, want %v", wantMatch, tc.wantMatch)
			}
			// The decision guardHolds makes from a match count.
			if holds := (0 > 0) == wantMatch; holds != tc.holdsWhenNone {
				t.Errorf("with no matches the guard holds = %v, want %v", holds, tc.holdsWhenNone)
			}
			if holds := (1 > 0) == wantMatch; holds != !tc.holdsWhenNone {
				t.Errorf("with one match the guard holds = %v, want %v", holds, !tc.holdsWhenNone)
			}
		})
	}
}

// TestAssembleReturn covers shaping property reads into a tool's result: which
// element a value comes from, how a missing element differs from an empty
// property, and the shape of a list result.
func TestAssembleReturn(t *testing.T) {
	props := map[string]map[string]string{
		"a": {valueField: "$10.00", "errorId": "payment-declined", "message": "Card declined"},
		"b": {valueField: "$99.99", "errorId": "promo-invalid", "message": "Invalid code"},
		"c": {valueField: ""},
	}
	strp := func(s string) *string { return &s }

	tests := []struct {
		name      string
		spec      returnSpec
		ids       []string
		wantValue *string
		wantItems []map[string]string
	}{
		{
			name: "a value reads from the only element found",
			spec: returnSpec{query: "Totals"},
			ids:  []string{"a"}, wantValue: strp("$10.00"),
		},
		{
			name: "several elements is not an error: the first one wins",
			spec: returnSpec{query: "Totals"},
			ids:  []string{"a", "b"}, wantValue: strp("$10.00"),
		},
		{
			name: "an element whose property is empty still reports a value",
			spec: returnSpec{query: "Totals"},
			ids:  []string{"c"}, wantValue: strp(""),
		},
		{
			name: "no element at all reports no value",
			spec: returnSpec{query: "Totals"},
			ids:  nil, wantValue: nil,
		},
		{
			name: "a list emits one row per element, in the order they were found",
			spec: returnSpec{query: "ErrorBanner", isList: true},
			ids:  []string{"a", "b"},
			wantItems: []map[string]string{
				{valueField: "$10.00", "errorId": "payment-declined", "message": "Card declined"},
				{valueField: "$99.99", "errorId": "promo-invalid", "message": "Invalid code"},
			},
		},
		{
			name:      "a row with nothing read off it is an empty row, not a missing one",
			spec:      returnSpec{query: "ErrorBanner", isList: true},
			ids:       []string{"unread"},
			wantItems: []map[string]string{{}},
		},
		{
			name:      "a list with no elements is an empty list, not an absent one",
			spec:      returnSpec{query: "ErrorBanner", isList: true},
			ids:       nil,
			wantItems: []map[string]string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			value, items := assembleReturn(&tc.spec, tc.ids, props)
			if !reflect.DeepEqual(value, tc.wantValue) {
				t.Errorf("value = %v, want %v", derefOrNil(value), derefOrNil(tc.wantValue))
			}
			if !reflect.DeepEqual(items, tc.wantItems) {
				t.Errorf("items = %v, want %v", items, tc.wantItems)
			}
			// An empty list has to stay distinguishable from an absent one:
			// toJSON renders the first as [] and omits the second entirely.
			if (items == nil) != (tc.wantItems == nil) {
				t.Errorf("items nil-ness = %v, want %v", items == nil, tc.wantItems == nil)
			}
		})
	}
}

// TestReturnSpecFor covers pairing a tool's declared result with the compiled
// extractors that read it. The declaration names a property; only the compiled
// form knows how to pull it off an element, so a tool that returns something
// cannot run without it.
func TestReturnSpecFor(t *testing.T) {
	textExtractor := gen.Extractor{Kind: "text"}
	priceExtractor := gen.Extractor{Kind: "text", Within: ".result-price"}

	tests := []struct {
		name       string
		declared   *gen.ReturnDef
		compiled   *gen.Tool
		wantNil    bool
		wantErr    bool
		wantQuery  string
		wantFields map[string]gen.Extractor
		wantList   bool
	}{
		{
			name:     "a tool that returns nothing needs no spec",
			declared: nil, wantNil: true,
		},
		{
			name:     "a returns block with only a description reads nothing",
			declared: &gen.ReturnDef{Description: "just prose"}, wantNil: true,
		},
		{
			name:       "a value pairs its query with the single compiled extractor",
			declared:   &gen.ReturnDef{Value: &gen.ValueRef{Query: "StarButton", Property: "label"}},
			compiled:   &gen.Tool{Returns: &gen.Return{Kind: "value", Extractor: &textExtractor}},
			wantQuery:  "StarButton",
			wantFields: map[string]gen.Extractor{valueField: textExtractor},
		},
		{
			name: "a list pairs its row query with one extractor per named field",
			declared: &gen.ReturnDef{List: &gen.ListRef{Rows: "ResultItem", Fields: map[string]gen.FieldDef{
				"price": {Property: "price"},
			}}},
			compiled: &gen.Tool{Returns: &gen.Return{Kind: "list", Fields: map[string]gen.Field{
				"price": {Property: "price", Extractor: priceExtractor},
			}}},
			wantQuery:  "ResultItem",
			wantFields: map[string]gen.Extractor{"price": priceExtractor},
			wantList:   true,
		},
		{
			name:     "a value with no compiled form at all is an error, not an empty result",
			declared: &gen.ReturnDef{Value: &gen.ValueRef{Query: "StarButton", Property: "label"}},
			compiled: nil, wantErr: true,
		},
		{
			name:     "a value whose compiled form has no extractor is an error",
			declared: &gen.ReturnDef{Value: &gen.ValueRef{Query: "StarButton", Property: "label"}},
			compiled: &gen.Tool{Returns: &gen.Return{Kind: "value"}}, wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := returnSpecFor(tc.declared, tc.compiled)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("returnSpecFor = %+v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("returnSpecFor: %v", err)
			}
			if tc.wantNil {
				if got != nil {
					t.Errorf("spec = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("spec = nil, want one")
			}
			if got.query != tc.wantQuery {
				t.Errorf("query = %q, want %q", got.query, tc.wantQuery)
			}
			if !reflect.DeepEqual(got.fields, tc.wantFields) {
				t.Errorf("fields = %+v, want %+v", got.fields, tc.wantFields)
			}
			if got.isList != tc.wantList {
				t.Errorf("isList = %v, want %v", got.isList, tc.wantList)
			}
		})
	}
}

func derefOrNil(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// TestStepOp covers unpacking a step, which the manifest format writes as a
// map holding exactly one entry: the op name and its body.
func TestStepOp(t *testing.T) {
	tests := []struct {
		name   string
		step   map[string]gen.StepBody
		wantOp string
		wantOK bool
	}{
		{name: "one entry unpacks to that op", step: map[string]gen.StepBody{"click": {Query: "Q"}}, wantOp: "click", wantOK: true},
		{name: "no entries is not a step", step: map[string]gen.StepBody{}},
		{name: "two entries is ambiguous, so not a step", step: map[string]gen.StepBody{"click": {}, "fill": {}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			op, body, ok := stepOp(tc.step)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if op != tc.wantOp {
				t.Errorf("op = %q, want %q", op, tc.wantOp)
			}
			if body != tc.step[tc.wantOp] {
				t.Errorf("body = %+v, want %+v", body, tc.step[tc.wantOp])
			}
		})
	}
}

// TestInterpolate covers substituting the caller's params into a manifest
// string.
func TestInterpolate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		args map[string]any
		want string
	}{
		{name: "a string param substitutes verbatim", in: "MenuCard[itemName*={{item}}]", args: map[string]any{"item": "Bowl"}, want: "MenuCard[itemName*=Bowl]"},
		{name: "a number renders without quotes or decimals", in: "count={{n}}", args: map[string]any{"n": float64(3)}, want: "count=3"},
		{name: "a bool renders as true or false", in: "watched={{w}}", args: map[string]any{"w": true}, want: "watched=true"},
		{name: "surrounding whitespace in the reference is ignored", in: "x={{ item }}", args: map[string]any{"item": "Bowl"}, want: "x=Bowl"},
		{name: "several references all substitute", in: "{{a}}/{{b}}", args: map[string]any{"a": "one", "b": "two"}, want: "one/two"},
		{name: "an unsupplied param becomes empty", in: "x={{missing}}", args: map[string]any{}, want: "x="},
		{name: "a null param becomes empty", in: "x={{k}}", args: map[string]any{"k": nil}, want: "x="},
		{name: "a string with no references is unchanged", in: "AddToCartButton", args: map[string]any{"item": "Bowl"}, want: "AddToCartButton"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := interpolate(tc.in, tc.args); got != tc.want {
				t.Errorf("interpolate(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
