package gen

import (
	"regexp"
	"sort"
	"strings"

	"github.com/sightmap/sightmap/go/compquery"
	sm "github.com/sightmap/sightmap/go/sightmap"
)

var templateRe = regexp.MustCompile(`\{\{\s*([\w.]+)\s*\}\}`)

func templateParams(s string) []string {
	var out []string
	for _, m := range templateRe.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// parseExtractor maps a sightmap property `extract:` string (SEP-0010 grammar)
// to an IR Extractor. Grammar: text | inner_text | text_only | attr=NAME |
// exists:SEL | <css-selector> (text of the matching descendant).
func parseExtractor(extract string) Extractor {
	s := strings.TrimSpace(extract)
	switch s {
	case "", "text":
		return Extractor{Kind: "text"}
	case "inner_text":
		return Extractor{Kind: "innerText"}
	case "text_only":
		return Extractor{Kind: "textOnly"}
	}
	if name, ok := strings.CutPrefix(s, "attr="); ok {
		return Extractor{Kind: "attr", Attr: name}
	}
	if sel, ok := strings.CutPrefix(s, "exists:"); ok {
		return Extractor{Kind: "exists", Within: sel}
	}
	return Extractor{Kind: "text", Within: s}
}

type compiler struct {
	c     *sm.Corpus
	diags []Diagnostic
}

func (cc *compiler) errf(code, where, format string, args ...any) {
	cc.diags = append(cc.diags, errf(code, where, format, args...))
}

func (cc *compiler) warnf(code, where, format string, args ...any) {
	cc.diags = append(cc.diags, warnf(code, where, format, args...))
}

// effectiveComponents mirrors Corpus.ComponentsForURL's merge for a named view:
// view components win on name collision, then non-colliding globals. Returns a
// name->def map plus a sorted name list for candidate diagnostics.
func (cc *compiler) effectiveComponents(view *sm.ViewDef) (map[string]sm.ComponentDef, []string) {
	byName := map[string]sm.ComponentDef{}
	if view != nil {
		for _, vc := range view.Components {
			byName[vc.Name] = vc
		}
	}
	for _, gc := range cc.c.GlobalComponents {
		if _, ok := byName[gc.Name]; !ok {
			byName[gc.Name] = gc
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	return byName, names
}

func candidateList(names []string) string {
	if len(names) > 12 {
		names = names[:12]
	}
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}

// resolvePropertyDef resolves a declared property name to its extractor against a
// component's property set.
func (cc *compiler) resolvePropertyDef(propName string, props []sm.ComponentPropertyDef, toolName string) (Extractor, bool) {
	for _, p := range props {
		if p.Name == propName {
			return parseExtractor(p.Extract), true
		}
	}
	var have []string
	for _, p := range props {
		have = append(have, p.Name)
	}
	cc.errf("compile.prop-unresolved", toolName,
		"property %q is not declared on the referenced component (have: %s)", propName, candidateList(have))
	return Extractor{}, false
}

// compileQuery parses a compquery path and resolves each part against the
// in-scope components: component name -> selectors, each predicate's property ->
// extractor. The result is a sightmap-free descendant chain the runtime resolves
// by DOM containment. Reuses the sightmap library's parser (grammar parity with
// `sightmap browser` queries); the library's live resolver stays out of the
// runtime (the firewall).
// The returned props are the LAST part's declared properties (the target
// component), so a collect can resolve its fields against the row.
func (cc *compiler) compileQuery(queryStr string, comps map[string]sm.ComponentDef, names []string, known map[string]bool, toolName string) (*Query, []sm.ComponentPropertyDef, bool) {
	if strings.TrimSpace(queryStr) == "" {
		cc.errf("compile.query-missing", toolName, "a step/ref in %q has no query", toolName)
		return nil, nil, false
	}
	q, err := compquery.ParseQuery(queryStr)
	if err != nil {
		cc.errf("compile.query-parse", toolName, "tool %q has an invalid query %q: %v", toolName, queryStr, err)
		return nil, nil, false
	}
	ok := true
	var parts []PathPart
	var targetProps []sm.ComponentPropertyDef // last part's properties = the target
	for _, part := range q.Parts {
		def, found := comps[part.Name]
		if !found {
			cc.errf("compile.query-ref", toolName,
				"tool %q query references unknown component %q. Available: %s", toolName, part.Name, candidateList(names))
			ok = false
			continue
		}
		targetProps = def.Properties
		pp := PathPart{Locators: def.Selectors}
		for _, pr := range part.Preds {
			ex, pok := cc.resolvePropertyDef(pr.Prop, def.Properties, toolName)
			if !pok {
				ok = false
				continue
			}
			cc.validateTemplate(pr.Val, known, toolName)
			pp.Preds = append(pp.Preds, Pred{Property: pr.Prop, Extractor: ex, Op: pr.Op, Value: pr.Val, CI: pr.CI})
		}
		parts = append(parts, pp)
	}
	if !ok {
		return nil, nil, false
	}
	query := &Query{Parts: parts}
	if q.Index >= 0 {
		idx := q.Index
		query.Index = &idx
	}
	return query, targetProps, true
}

func (cc *compiler) validateTemplate(s string, known map[string]bool, toolName string) {
	for _, tok := range templateParams(s) {
		if !known[tok] {
			cc.errf("compile.param", toolName, "tool %q references unknown param {{%s}}", toolName, tok)
		}
	}
}

func inputSchema(params []ParamDef) InputSchema {
	schema := InputSchema{Type: "object", Properties: map[string]SchemaProp{}}
	for _, p := range params {
		t := "string"
		switch p.Type {
		case "number":
			t = "number"
		case "boolean":
			t = "boolean"
		}
		sp := SchemaProp{Type: t}
		if p.Description != "" {
			sp.Description = p.Description
		}
		if p.Type == "enum" && len(p.Values) > 0 {
			sp.Enum = p.Values
		}
		schema.Properties[p.Name] = sp
		if p.Required {
			schema.Required = append(schema.Required, p.Name)
		}
	}
	return schema
}

func (cc *compiler) compileStep(
	op string, body StepBody,
	comps map[string]sm.ComponentDef, names []string,
	known map[string]bool, toolName string,
) (Step, bool) {
	switch op {
	case "navigate":
		v := cc.c.ViewByName(body.View)
		if v == nil {
			cc.errf("compile.nav", toolName, "%s navigates to unknown view %q", toolName, body.View)
			return Step{}, false
		}
		return Step{Op: "navigate", View: v.Name, Route: v.Route}, true

	case "fill":
		q, _, ok := cc.compileQuery(body.Query, comps, names, known, toolName)
		if !ok {
			return Step{}, false
		}
		cc.validateTemplate(body.Value, known, toolName)
		return Step{Op: "fill", Query: q, Value: body.Value}, true

	case "click":
		q, _, ok := cc.compileQuery(body.Query, comps, names, known, toolName)
		if !ok {
			return Step{}, false
		}
		return Step{Op: "click", Query: q}, true

	case "wait_for":
		timeout := body.TimeoutMs
		if timeout == 0 {
			timeout = 5000
		}
		q, _, ok := cc.compileQuery(body.Query, comps, names, known, toolName)
		if !ok {
			return Step{}, false
		}
		return Step{Op: "waitFor", Query: q, TimeoutMs: timeout}, true

	default:
		cc.errf("compile.step", toolName, "tool %q has an unrecognized step op %q", toolName, op)
		return Step{}, false
	}
}

// compileFields resolves a set of output fields against a row component's
// declared properties (deterministic order). Shared by list returns.
func (cc *compiler) compileFields(fields map[string]FieldDef, props []sm.ComponentPropertyDef, toolName string) map[string]Field {
	out := map[string]Field{}
	names := make([]string, 0, len(fields))
	for f := range fields {
		names = append(names, f)
	}
	sort.Strings(names)
	for _, f := range names {
		spec := fields[f]
		if ex, ok := cc.resolvePropertyDef(spec.Property, props, toolName); ok {
			out[f] = Field{Property: spec.Property, Extractor: ex}
		}
	}
	return out
}

func (cc *compiler) compileReturn(ret *ReturnDef, comps map[string]sm.ComponentDef, names []string, known map[string]bool, toolName string) *Return {
	if ret == nil {
		return nil
	}
	if ret.Extract != nil && ret.List != nil {
		cc.errf("compile.return-shape", toolName, "tool %q returns has both extract and list; use exactly one", toolName)
		return nil
	}

	// list: map a compquery over every match -> one Fields-shaped object per row.
	if ret.List != nil {
		q, props, ok := cc.compileQuery(ret.List.Rows, comps, names, known, toolName)
		if !ok {
			return nil
		}
		out := &Return{Kind: "list", Query: q, Fields: cc.compileFields(ret.List.Fields, props, toolName)}
		if ret.Description != "" {
			out.Description = ret.Description
		}
		return out
	}

	// extract: read one declared property off the first match.
	if ret.Extract != nil {
		q, props, ok := cc.compileQuery(ret.Extract.Query, comps, names, known, toolName)
		if !ok {
			return nil
		}
		out := &Return{Kind: "value", Query: q}
		if ret.Description != "" {
			out.Description = ret.Description
		}
		if ex, ok := cc.resolvePropertyDef(ret.Extract.Property, props, toolName); ok {
			out.Extractor = &ex
		}
		return out
	}

	// description-only: a value return with nothing to read.
	out := &Return{Kind: "value"}
	if ret.Description != "" {
		out.Description = ret.Description
	}
	return out
}

func (cc *compiler) compileToolGuard(g *GuardBody, comps map[string]sm.ComponentDef, names []string, known map[string]bool, toolName string) *Guard {
	var kind string
	var ref *GuardRef
	switch {
	case g.Present != nil:
		kind, ref = "present", g.Present
	case g.Absent != nil:
		kind, ref = "absent", g.Absent
	default:
		cc.errf("compile.guard", toolName, "tool %q guard must have a present: or absent: reference", toolName)
		return nil
	}
	q, _, ok := cc.compileQuery(ref.Query, comps, names, known, toolName)
	if !ok {
		return nil
	}
	return &Guard{Kind: kind, Query: q}
}

func (cc *compiler) compileTool(t ToolDef) Tool {
	mode := t.Mode
	if mode == "" {
		mode = "live"
	}
	known := map[string]bool{}
	for _, p := range t.Params {
		known[p.Name] = true
	}

	var view *sm.ViewDef
	if t.EnsureView != "" {
		view = cc.c.ViewByName(t.EnsureView)
		if view == nil {
			cc.errf("compile.ensure-view", t.Name, "tool %q ensure_view names unknown view %q", t.Name, t.EnsureView)
		}
	}
	comps, names := cc.effectiveComponents(view)

	tool := Tool{Name: t.Name, Mode: mode, InputSchema: inputSchema(t.Params)}
	if t.Description != "" {
		tool.Description = t.Description
	}
	if view != nil {
		tool.EnsureView = &EnsureView{View: view.Name, Route: view.Route}
	}
	if t.Guard != nil {
		tool.Guard = cc.compileToolGuard(t.Guard, comps, names, known, t.Name)
	}
	for _, step := range t.Steps {
		op, body, ok := stepOp(step)
		if !ok {
			cc.errf("compile.step", t.Name, "tool %q has a step that is not a single-key mapping", t.Name)
			continue
		}
		if s, ok := cc.compileStep(op, body, comps, names, known, t.Name); ok {
			tool.Steps = append(tool.Steps, s)
		}
	}
	if tool.Steps == nil {
		tool.Steps = []Step{}
	}
	tool.Returns = cc.compileReturn(t.Returns, comps, names, known, t.Name)
	return tool
}

func toolNames(ir *IR) []string {
	names := make([]string, 0, len(ir.Tools))
	for _, t := range ir.Tools {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	return names
}

func toolView(t *Tool) string {
	if t.EnsureView != nil {
		return t.EnsureView.View
	}
	return ""
}

// attachGuidance compiles the journey graph into per-tool guidance breadcrumbs:
// for each adjacent (a -> b) in a journey, tool a gains a suggestion to run b
// next. `when` is "after_navigation" (with the destination view) if a navigates
// to b's view, else "now". Successors are unioned + deduped across journeys.
func (cc *compiler) attachGuidance(ir *IR, journeys []JourneyDef) {
	byName := map[string]*Tool{}
	for i := range ir.Tools {
		byName[ir.Tools[i].Name] = &ir.Tools[i]
	}
	seen := map[string]map[string]bool{}

	for _, j := range journeys {
		for i := 0; i+1 < len(j.Steps); i++ {
			a, b := j.Steps[i], j.Steps[i+1]
			ta, tb := byName[a.Tool], byName[b.Tool]
			if ta == nil {
				cc.errf("compile.journey-ref", j.Name, "journey %q references unknown tool %q (have: %s)", j.Name, a.Tool, candidateList(toolNames(ir)))
				continue
			}
			if tb == nil {
				cc.errf("compile.journey-ref", j.Name, "journey %q references unknown tool %q (have: %s)", j.Name, b.Tool, candidateList(toolNames(ir)))
				continue
			}
			// If the next tool lives on a different view, reaching it requires a
			// navigation — regardless of how (app button, link, or a navigate step).
			when, view := "now", ""
			if va, vb := toolView(ta), toolView(tb); va != "" && vb != "" && va != vb {
				when, view = "after_navigation", vb
			}
			key := when + "|" + b.Tool
			if seen[a.Tool] == nil {
				seen[a.Tool] = map[string]bool{}
			}
			if seen[a.Tool][key] {
				continue
			}
			seen[a.Tool][key] = true
			sug := Suggestion{Tool: b.Tool, When: when}
			if b.Reason != "" {
				sug.Reason = b.Reason
			}
			if view != "" {
				sug.View = view
			}
			ta.Guidance = append(ta.Guidance, sug)
		}
	}
}

// Compile resolves a manifest against a corpus into the IR, accumulating
// diagnostics. The corpus is the sightmap library's already-resolved model.
func Compile(m *Manifest, c *sm.Corpus) (IR, []Diagnostic) {
	cc := &compiler{c: c}
	ir := IR{Version: 1, Name: m.Name}
	if ir.Name == "" {
		ir.Name = "sightkick"
	}
	for _, v := range c.Views {
		ir.Views = append(ir.Views, ViewRef{Name: v.Name, Route: v.Route})
	}
	if ir.Views == nil {
		ir.Views = []ViewRef{}
	}
	for _, t := range m.Tools {
		ir.Tools = append(ir.Tools, cc.compileTool(t))
	}
	if ir.Tools == nil {
		ir.Tools = []Tool{}
	}
	cc.attachGuidance(&ir, m.Journeys)
	return ir, cc.diags
}
