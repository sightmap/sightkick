package gen

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"
)

// Outline is the plan-time discovery projection over a compiled tool layer:
// journeys, views, and every tool's plan-relevant facts (description, params,
// ensure_view, returns shape) — deliberately excluding steps, guards, and every
// compiled Query/PathPart/Pred/Extractor, which are runtime DOM-addressing
// detail worthless to a scenario-to-plan resolver and the overwhelming majority
// of the IR's bytes. Modeled on sightmap's Stats/Totals/ViewStats
// (go/sightmap/stats.go): a plain data type with a stable JSON shape, plus one
// builder. The field names are printed by `sightkick outline`/`explain --json`
// and read by whatever eventually automates scenario resolution — keep them
// stable, the way sightmap keeps Stats' field names stable.
type Outline struct {
	Name     string           `json:"name"`
	Totals   Totals           `json:"totals"`
	Journeys []JourneyOutline `json:"journeys"`
	Views    []ViewOutline    `json:"views"`
	Tools    []ToolOutline    `json:"tools"`
}

// Totals is split out from Outline, mirroring sightmap's Stats/Totals split,
// so the three counts can be printed (or checked) without the rest of the
// payload.
type Totals struct {
	Tools    int `json:"tools"`
	Journeys int `json:"journeys"`
	Views    int `json:"views"`
}

// JourneyOutline is one authored journey: its name, its description (the
// one-line gloss an author writes for it — see JourneyDef), and the ordered
// tool names it walks. This is the first artifact that surfaces a journey's
// Description at all; Compile discards it once guidance is attached.
type JourneyOutline struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Steps       []string `json:"steps"`
}

// ViewOutline is one corpus view and how many tools declare ensure_view on it.
type ViewOutline struct {
	Name  string `json:"name"`
	Route string `json:"route,omitempty"`
	Tools int    `json:"tools"`
}

// ToolOutline is one tool's plan-time facts. View/Route are empty for a tool
// with no ensure_view (grouped under "(any view)" by the renderer). Mode,
// Description, Params, and Returns are zeroed by Brief() — they are
// explain-only detail, not part of the orientation pass.
type ToolOutline struct {
	Name        string         `json:"name"`
	Summary     string         `json:"summary"`
	Description string         `json:"description,omitempty"`
	View        string         `json:"view,omitempty"`
	Route       string         `json:"route,omitempty"`
	Mode        string         `json:"mode,omitempty"`
	Params      []ParamOutline `json:"params,omitempty"`
	Returns     *ReturnOutline `json:"returns,omitempty"`
}

// ParamOutline is one tool parameter, in authored order (sourced from
// Manifest.ToolDef.Params, not Tool.InputSchema — InputSchema.Properties is a
// map and loses the order an author wrote params in, which is also the order
// `sightkick call --param` reads best in).
type ParamOutline struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Required    bool     `json:"required,omitempty"`
	Description string   `json:"description,omitempty"`
	Values      []string `json:"values,omitempty"` // enum only
}

// ReturnOutline is a tool's result shape, structured. Key is the envelope key
// the runtime actually uses ("value" or "items") — see returnHint, the single
// source of that naming; the two must not drift. Text rendering skips this
// entirely: compileTool already bakes the same facts into Description via
// returnHint, so printing both would say the same thing twice. --json carries
// it here because a machine can't parse the prose form.
type ReturnOutline struct {
	Kind        string   `json:"kind"` // value | list
	Key         string   `json:"key"`  // value | items
	Fields      []string `json:"fields,omitempty"`
	Description string   `json:"description,omitempty"`
}

// BuildOutline compiles target's manifest + corpus once and projects the
// result into an Outline. It needs both the compiled IR (Tool.Description
// carries the returnHint suffix baked in at compile time — see compileTool)
// and the raw manifest (authored param order, and Manifest.Journeys, which
// Compile consumes into per-tool Guidance and otherwise discards). load()
// produces both from a single parse/compile pass.
func BuildOutline(target string) (Outline, []Diagnostic, error) {
	l, diags, err := load(target)
	if err != nil {
		return Outline{}, diags, err
	}
	if !l.compiled {
		return Outline{Name: l.manifest.Name}, diags, nil
	}

	byName := make(map[string]*ToolDef, len(l.manifest.Tools))
	for i := range l.manifest.Tools {
		byName[l.manifest.Tools[i].Name] = &l.manifest.Tools[i]
	}

	out := Outline{Name: l.ir.Name}
	for _, t := range l.ir.Tools {
		to := ToolOutline{
			Name:        t.Name,
			Summary:     summaryLine(t.Description),
			Description: t.Description,
			Mode:        t.Mode,
			Returns:     returnOutline(t.Returns),
		}
		if t.EnsureView != nil {
			to.View, to.Route = t.EnsureView.View, t.EnsureView.Route
		}
		// A manifest entry should always exist by the time diagnostics are clean
		// (compileTool walks m.Tools 1:1 into ir.Tools), but a future non-YAML
		// tool source could break that, so this stays a lookup, not an index.
		if td, ok := byName[t.Name]; ok {
			for _, p := range td.Params {
				to.Params = append(to.Params, ParamOutline{
					Name: p.Name, Type: p.Type, Required: p.Required,
					Description: p.Description, Values: p.Values,
				})
			}
		}
		out.Tools = append(out.Tools, to)
	}

	for _, j := range l.manifest.Journeys {
		jo := JourneyOutline{Name: j.Name, Description: j.Description}
		for _, s := range j.Steps {
			jo.Steps = append(jo.Steps, s.Tool)
		}
		out.Journeys = append(out.Journeys, jo)
	}

	viewTools := map[string]int{}
	for _, t := range out.Tools {
		if t.View != "" {
			viewTools[t.View]++
		}
	}
	for _, v := range l.ir.Views {
		out.Views = append(out.Views, ViewOutline{Name: v.Name, Route: v.Route, Tools: viewTools[v.Name]})
	}

	out.Totals = Totals{Tools: len(out.Tools), Journeys: len(out.Journeys), Views: len(out.Views)}
	return out, diags, nil
}

// returnOutline projects a compiled Return, or nil if the tool reads nothing
// (including a returns: block carrying only a description with no query).
func returnOutline(r *Return) *ReturnOutline {
	if r == nil {
		return nil
	}
	ro := &ReturnOutline{Kind: r.Kind, Description: r.Description}
	switch r.Kind {
	case "list":
		ro.Key = "items"
		ro.Fields = slices.Sorted(maps.Keys(r.Fields))
	case "value":
		if r.Query == nil && r.Extractor == nil {
			return nil // description-only, nothing to read — see returnHint
		}
		ro.Key = "value"
	}
	return ro
}

// summaryLine collapses a (possibly multi-line, YAML-block-scalar) description
// to one line short enough for the orientation pass: whitespace collapsed,
// then cut at the first sentence boundary that reads like a real sentence end
// rather than an abbreviation, then hard-capped at 140 runes.
//
// "Sentence boundary" is a period followed by whitespace and then either an
// uppercase letter or a backtick, or by end-of-string. The uppercase/backtick
// lookahead is load-bearing: without it, descriptions with a mid-sentence
// "e.g." or "i.e." (common in this corpus — see log_in, read_login_error)
// would cut after "e.g." instead of at the real sentence end, because a bare
// period-plus-space isn't enough to tell the two apart.
func summaryLine(desc string) string {
	s := strings.Join(strings.Fields(desc), " ")
	if s == "" {
		return s
	}

	runes := []rune(s)
	for i, r := range runes {
		if r != '.' {
			continue
		}
		if i == len(runes)-1 {
			s = string(runes[:i+1])
			break
		}
		if !unicode.IsSpace(runes[i+1]) {
			continue
		}
		j := i + 1
		for j < len(runes) && unicode.IsSpace(runes[j]) {
			j++
		}
		if j >= len(runes) || unicode.IsUpper(runes[j]) || runes[j] == '`' {
			s = string(runes[:i+1])
			break
		}
	}

	runes = []rune(s)
	if len(runes) <= 140 {
		return s
	}
	cut := string(runes[:140])
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ") + "…"
}

// Selector names a subset of an Outline's tools by three alternative routes —
// explicit tool name, journey membership, or view membership — that UNION
// rather than intersect: they are alternative ways of naming a region an
// agent wants detail on ("this journey, plus one extra tool"), not
// conjunctive predicates a tool must satisfy all of.
type Selector struct {
	Tools    []string
	Journeys []string
	Views    []string
}

// Empty reports whether the selector names nothing, i.e. explain was given no
// --journey/--view and no positional tool names.
func (s Selector) Empty() bool {
	return len(s.Tools) == 0 && len(s.Journeys) == 0 && len(s.Views) == 0
}

// Select narrows o to the union of the named tools, journeys, and views. An
// unresolvable name is an error naming candidates (matching compquery's
// zero-match-errors convention) rather than a silent empty result — a typo
// should not read as "no such tool exists". A resolvable selector naming zero
// tools (e.g. a view nobody wrote a tool for) is not an error: it's the
// cheapest possible gap report, in the sense docs/scenario-testing.md §6
// calls an unresolvable Gherkin line a gap.
func (o Outline) Select(sel Selector) (Outline, error) {
	toolByName := make(map[string]ToolOutline, len(o.Tools))
	for _, t := range o.Tools {
		toolByName[t.Name] = t
	}
	journeyByName := make(map[string]JourneyOutline, len(o.Journeys))
	for _, j := range o.Journeys {
		journeyByName[j.Name] = j
	}
	viewByName := make(map[string]ViewOutline, len(o.Views))
	for _, v := range o.Views {
		viewByName[v.Name] = v
	}

	want := map[string]bool{}
	for _, name := range sel.Tools {
		if _, ok := toolByName[name]; !ok {
			return Outline{}, fmt.Errorf("no such tool %q. Available: %s", name, candidateList(slices.Sorted(maps.Keys(toolByName))))
		}
		want[name] = true
	}
	usedJourneys := map[string]bool{}
	for _, name := range sel.Journeys {
		j, ok := journeyByName[name]
		if !ok {
			return Outline{}, fmt.Errorf("no such journey %q. Available: %s", name, candidateList(slices.Sorted(maps.Keys(journeyByName))))
		}
		usedJourneys[name] = true
		for _, tn := range j.Steps {
			want[tn] = true
		}
	}
	for _, name := range sel.Views {
		if _, ok := viewByName[name]; !ok {
			return Outline{}, fmt.Errorf("no such view %q. Available: %s", name, candidateList(slices.Sorted(maps.Keys(viewByName))))
		}
		for _, t := range o.Tools {
			if t.View == name {
				want[t.Name] = true
			}
		}
	}

	out := Outline{Name: o.Name}
	for _, t := range o.Tools {
		if want[t.Name] {
			out.Tools = append(out.Tools, t)
		}
	}

	// A journey is included in the result if it was named explicitly, or if any
	// of its steps survived the selection — so `explain --view Cart` still shows
	// the journey header for context even though the journey wasn't named.
	for _, j := range o.Journeys {
		if usedJourneys[j.Name] {
			out.Journeys = append(out.Journeys, j)
			continue
		}
		for _, tn := range j.Steps {
			if want[tn] {
				out.Journeys = append(out.Journeys, j)
				break
			}
		}
	}

	viewTools := map[string]int{}
	for _, t := range out.Tools {
		if t.View != "" {
			viewTools[t.View]++
		}
	}
	for _, v := range o.Views {
		if n := viewTools[v.Name]; n > 0 {
			out.Views = append(out.Views, ViewOutline{Name: v.Name, Route: v.Route, Tools: n})
		}
	}

	out.Totals = Totals{Tools: len(out.Tools), Journeys: len(out.Journeys), Views: len(out.Views)}
	return out, nil
}

// Brief projects o down to the orientation pass: journeys, views, and each
// tool's name/summary/view only — no description, params, mode, or returns.
// --json goes through this too when no selector/--detail is given, so the
// machine form is tiered exactly like the text form and never quietly ships
// explain-sized bytes under the outline command.
func (o Outline) Brief() Outline {
	out := o
	out.Tools = make([]ToolOutline, len(o.Tools))
	for i, t := range o.Tools {
		out.Tools[i] = ToolOutline{Name: t.Name, Summary: t.Summary, View: t.View, Route: t.Route}
	}
	return out
}
