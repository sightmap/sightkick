package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"sightkick/generator/internal/gen"
)

// toolNameColMax caps the tool-name column so one long tool name can't push a
// summary/description off a reasonable terminal width — mirrors sightmap's
// viewNameColMax/viewRouteColMax convention (go/cmd/sightmap/viewtable.go),
// applied to the one column sightkick's own tables need.
const toolNameColMax = 30

// banner renders sightmap's table-command banner convention: "<prog> <verb> ·
// <name> · <date>". now is a parameter (not time.Now() called here) so golden
// tests can render a fixed date instead of one that changes every run.
func banner(verb, name string, now time.Time) string {
	return fmt.Sprintf("sightkick %s · %s · %s", verb, name, now.Format("2006-01-02"))
}

func rule(width int) string {
	if width < 20 {
		width = 20
	}
	return strings.Repeat("─", width)
}

// nameColWidth returns the tool-name column width for a set of tools: the
// longest name, capped at toolNameColMax.
func nameColWidth(tools []gen.ToolOutline) int {
	w := 0
	for _, t := range tools {
		if n := len(t.Name); n > w {
			w = n
		}
	}
	if w > toolNameColMax {
		w = toolNameColMax
	}
	return w
}

// renderJourneys writes the journeys block shared by outline and explain: one
// name + description line, then an indented "›"-joined step chain (matching
// sightmap's YAML-breadcrumb glyph, go/cmd/sightmap/cmd_search.go).
func renderJourneys(w io.Writer, journeys []gen.JourneyOutline) {
	for _, j := range journeys {
		if j.Description != "" {
			fmt.Fprintf(w, " %s  %s\n", j.Name, j.Description)
		} else {
			fmt.Fprintf(w, " %s\n", j.Name)
		}
		fmt.Fprintf(w, "   %s\n", strings.Join(j.Steps, " › "))
	}
}

// renderToolGroups writes tools grouped by ensure_view, view order following
// the corpus (views with no tools are omitted — see the "N view(s) have no
// tools" summary line instead), with ensure_view-less tools trailing under
// "(any view)" since that is exactly true per the compiler's corpus-wide
// resolution for a tool that declares no ensure_view.
func renderToolGroups(w io.Writer, o gen.Outline) {
	byView := map[string][]gen.ToolOutline{}
	var anyView []gen.ToolOutline
	for _, t := range o.Tools {
		if t.View == "" {
			anyView = append(anyView, t)
			continue
		}
		byView[t.View] = append(byView[t.View], t)
	}
	nameW := nameColWidth(o.Tools)

	printGroup := func(header string, tools []gen.ToolOutline) {
		if len(tools) == 0 {
			return
		}
		fmt.Fprintln(w, " "+header)
		for _, t := range tools {
			fmt.Fprintf(w, "   %-*s  %s\n", nameW, t.Name, t.Summary)
		}
	}
	for _, v := range o.Views {
		printGroup(viewHeader(v), byView[v.Name])
	}
	printGroup("(any view)", anyView)
}

func viewHeader(v gen.ViewOutline) string {
	if v.Route == "" {
		return v.Name
	}
	return v.Name + "  " + v.Route
}

// renderParams writes one tool's params table, name/type/required columns
// padded, enum values rendered as `enum {A, B}`.
func renderParams(w io.Writer, params []gen.ParamOutline) {
	if len(params) == 0 {
		return
	}
	nameW := 0
	for _, p := range params {
		if len(p.Name) > nameW {
			nameW = len(p.Name)
		}
	}
	for _, p := range params {
		typ := p.Type
		if p.Type == "enum" && len(p.Values) > 0 {
			typ = "enum {" + strings.Join(p.Values, ", ") + "}"
		}
		req := ""
		if p.Required {
			req = "required"
		}
		line := fmt.Sprintf("    %-*s  %-9s  %s", nameW, p.Name, typ, req)
		if p.Description != "" {
			line = strings.TrimRight(line, " ") + " " + p.Description
		}
		fmt.Fprintln(w, strings.TrimRight(line, " "))
	}
}

// renderOutline writes the orientation pass: banner, totals, journeys (if
// any), tools grouped by view, and a summary line naming any view with no
// tools — a free gap report, in docs/scenario-testing.md §6's sense of "gap".
func renderOutline(w io.Writer, o gen.Outline, now time.Time) {
	fmt.Fprintln(w, banner("outline", o.Name, now))
	width := len(rule(0))
	fmt.Fprintln(w, rule(width))
	fmt.Fprintf(w, " %-9s %d\n", "Tools", o.Totals.Tools)
	fmt.Fprintf(w, " %-9s %d\n", "Journeys", o.Totals.Journeys)
	fmt.Fprintf(w, " %-9s %d\n", "Views", o.Totals.Views)

	if len(o.Journeys) > 0 {
		fmt.Fprintln(w, rule(width))
		renderJourneys(w, o.Journeys)
	}

	fmt.Fprintln(w, rule(width))
	renderToolGroups(w, o)
	fmt.Fprintln(w, rule(width))

	var noTools []string
	for _, v := range o.Views {
		if v.Tools == 0 {
			noTools = append(noTools, v.Name)
		}
	}
	summary := fmt.Sprintf(" %s  ·  %s  ·  %s",
		plural(o.Totals.Tools, "tool"), plural(o.Totals.Journeys, "journey"), plural(o.Totals.Views, "view"))
	if len(noTools) > 0 {
		sort.Strings(noTools)
		noun := "view has"
		if len(noTools) > 1 {
			noun = "views have"
		}
		summary += fmt.Sprintf("  (%d %s no tools: %s)", len(noTools), noun, strings.Join(noTools, ", "))
	}
	fmt.Fprintln(w, summary)
	fmt.Fprintln(w)
	fmt.Fprintln(w, " detail:  sightkick explain <dir> <tool>... | --journey NAME | --view NAME")
}

// renderExplain writes the drill-down pass for a selected subset: a header
// naming how many of the corpus's tools survived the selector, the journeys
// those tools participate in (full step chain, so the adjacency a filtered
// view would otherwise lose stays visible), then per-tool detail.
func renderExplain(w io.Writer, full, selected gen.Outline, now time.Time) {
	fmt.Fprintln(w, banner("explain", full.Name, now))
	fmt.Fprintf(w, " %d of %d tool(s)\n", len(selected.Tools), len(full.Tools))

	if len(selected.Journeys) > 0 {
		fmt.Fprintln(w)
		renderJourneys(w, selected.Journeys)
	}

	for _, t := range selected.Tools {
		fmt.Fprintln(w)
		where := "[any view]"
		if t.View != "" {
			where = "[" + viewHeader(gen.ViewOutline{Name: t.View, Route: t.Route}) + "]"
		}
		fmt.Fprintf(w, "%s  %s\n", t.Name, where)
		if t.Mode != "" && t.Mode != "live" {
			fmt.Fprintf(w, "  (mode: %s)\n", t.Mode)
		}
		if t.Description != "" {
			fmt.Fprintf(w, "  %s\n", t.Description)
		}
		renderParams(w, t.Params)
	}

	if len(selected.Tools) == 0 {
		fmt.Fprintln(w, "  no tools matched — that's a gap: see docs/scenario-testing.md §6")
	}
}

// plural renders "N word" / "N words".
func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
