package gen

import (
	"encoding/json"
	"strings"
	"testing"
)

const saucedemoDir = "../../../examples/saucedemo"

// TestOutlineNoCompiledQueries is the invariant the whole feature rests on:
// the outline projection never carries a compiled Query/PathPart/Pred (the DOM
// addressing that is most of the IR's bytes and worthless at plan time).
// "steps" and "guard" are deliberately not checked here — JourneyOutline has
// its own legitimate "steps" key (a list of tool names, not compiled Step
// structs), and "guard" appears in ordinary tool prose (e.g.
// go_to_checkout's description); neither would be a leak.
func TestOutlineNoCompiledQueries(t *testing.T) {
	o, diags, err := BuildOutline(saucedemoDir)
	if err != nil || HasErrors(diags) {
		t.Fatalf("BuildOutline failed: err=%v diags=%s", err, Format(diags))
	}
	full, jerr := json.Marshal(o)
	if jerr != nil {
		t.Fatal(jerr)
	}
	for _, forbidden := range []string{`"locators"`, `"parts"`, `"preds"`, `"timeoutMs"`, `"ensureView"`} {
		if strings.Contains(string(full), forbidden) {
			t.Errorf("outline JSON contains %s, a compiled-IR-only field:\n%s", forbidden, full)
		}
	}
}

// TestOutlineIsSmallerThanIR checks a ratio, not a byte count, so it isn't
// brittle to prose edits but still fails if someone re-inlines the compiled
// queries into the outline.
func TestOutlineIsSmallerThanIR(t *testing.T) {
	ir, diags, err := Build(saucedemoDir)
	if err != nil || HasErrors(diags) {
		t.Fatalf("Build failed: err=%v diags=%s", err, Format(diags))
	}
	o, diags, err := BuildOutline(saucedemoDir)
	if err != nil || HasErrors(diags) {
		t.Fatalf("BuildOutline failed: err=%v diags=%s", err, Format(diags))
	}

	irJSON, _ := json.Marshal(ir)
	briefJSON, _ := json.Marshal(o.Brief())
	if len(briefJSON) >= len(irJSON)/3 {
		t.Errorf("brief outline (%d bytes) is not under 1/3 of the full IR (%d bytes)", len(briefJSON), len(irJSON))
	}
}

// TestOutlineSurfacesJourneys checks the one capability that exists nowhere
// else in sightkick today: journey names, descriptions, and ordered step
// names, readable offline. Compile discards all of this once guidance is
// attached (see attachGuidance) — JourneyDef.Description is otherwise dead.
func TestOutlineSurfacesJourneys(t *testing.T) {
	cases := []struct {
		dir     string
		journey string
		desc    string
		steps   []string
	}{
		{todoDirForOutline, "add_and_review", "Add a todo, then review the list.", []string{"add_todo", "list_todos"}},
		{searchDir, "book_flow", "Search, review, select a flight, and book it.", []string{"search", "list_results", "select_flight", "book"}},
		{saucedemoDir, "purchase", "Log in, buy one item, and read the confirmation.",
			[]string{"log_in", "open_item", "add_current_item_to_cart", "go_to_cart", "go_to_checkout", "fill_checkout_info", "place_order", "read_confirmation"}},
	}
	for _, c := range cases {
		o, diags, err := BuildOutline(c.dir)
		if err != nil || HasErrors(diags) {
			t.Fatalf("BuildOutline(%s) failed: err=%v diags=%s", c.dir, err, Format(diags))
		}
		var found *JourneyOutline
		for i := range o.Journeys {
			if o.Journeys[i].Name == c.journey {
				found = &o.Journeys[i]
			}
		}
		if found == nil {
			t.Fatalf("%s: journey %q not found in outline", c.dir, c.journey)
		}
		if found.Description != c.desc {
			t.Errorf("%s: journey %q description = %q, want %q", c.dir, c.journey, found.Description, c.desc)
		}
		if strings.Join(found.Steps, ",") != strings.Join(c.steps, ",") {
			t.Errorf("%s: journey %q steps = %v, want %v", c.dir, c.journey, found.Steps, c.steps)
		}
	}
}

// todoDirForOutline avoids redeclaring exampleDir (gen_test.go already binds
// that name to the todo example) while keeping this test's cases visually
// aligned by dir.
const todoDirForOutline = exampleDir

func TestSummaryLine(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			name: "e.g. mid-sentence is not a sentence boundary",
			in:   "Read the login error banner, if any (e.g. after log_in with locked_out_user).",
			want: "Read the login error banner, if any (e.g. after log_in with locked_out_user).",
		},
		{
			name: "short first sentence followed by lowercase continuation gets hard-capped, not sentence-cut",
			in: "Log in from the Login page. saucedemo's published test users are all valid usernames " +
				"(standard_user, locked_out_user, problem_user, performance_glitch_user); the password " +
				"for every one of them is secret_sauce. A bad login does not navigate.",
			want: "Log in from the Login page. saucedemo's published test users are all valid usernames (standard_user, locked_out_user, problem_user,…",
		},
		{
			name: "sentence boundary before an uppercase-led continuation cuts there",
			in:   "Fill the first checkout step and continue to the order review. Leaving any field empty is a fixture.",
			want: "Fill the first checkout step and continue to the order review.",
		},
		{
			name: "a description that is only a result hint",
			in:   "Returns `value`: a single string.",
			want: "Returns `value`: a single string.",
		},
		{
			name: "collapses embedded newlines from a YAML block scalar",
			in:   "Add a product to the cart\nby name, from the Inventory\nlist.",
			want: "Add a product to the cart by name, from the Inventory list.",
		},
		{
			name: "empty description",
			in:   "",
			want: "",
		},
	}
	for _, c := range cases {
		if got := summaryLine(c.in); got != c.want {
			t.Errorf("%s:\n  summaryLine(%q)\n  =    %q\n  want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestSelect(t *testing.T) {
	o, diags, err := BuildOutline(saucedemoDir)
	if err != nil || HasErrors(diags) {
		t.Fatalf("BuildOutline failed: err=%v diags=%s", err, Format(diags))
	}

	t.Run("union of a journey and an extra tool", func(t *testing.T) {
		sel, err := o.Select(Selector{Journeys: []string{"purchase"}, Tools: []string{"back_to_products"}})
		if err != nil {
			t.Fatal(err)
		}
		// purchase has 8 steps; back_to_products isn't one of them.
		if len(sel.Tools) != 9 {
			names := make([]string, len(sel.Tools))
			for i, t := range sel.Tools {
				names[i] = t.Name
			}
			t.Errorf("got %d tools, want 9 (union, not intersection): %v", len(sel.Tools), names)
		}
	})

	t.Run("unknown journey errors with a candidate", func(t *testing.T) {
		_, err := o.Select(Selector{Journeys: []string{"purchse"}})
		if err == nil || !strings.Contains(err.Error(), "purchase") {
			t.Errorf("Select(unknown journey) error = %v, want it to name a candidate", err)
		}
	})

	t.Run("unknown view errors with a candidate", func(t *testing.T) {
		_, err := o.Select(Selector{Views: []string{"Nope"}})
		if err == nil || !strings.Contains(err.Error(), "Cart") {
			t.Errorf("Select(unknown view) error = %v, want it to name a candidate", err)
		}
	})

	t.Run("unknown tool errors with a candidate", func(t *testing.T) {
		_, err := o.Select(Selector{Tools: []string{"nope"}})
		if err == nil || !strings.Contains(err.Error(), "log_in") {
			t.Errorf("Select(unknown tool) error = %v, want it to name a candidate", err)
		}
	})

	t.Run("a valid view with tools returns them, no error", func(t *testing.T) {
		sel, err := o.Select(Selector{Views: []string{"Cart"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(sel.Tools) != 4 {
			t.Errorf("got %d tools for view Cart, want 4", len(sel.Tools))
		}
	})
}
