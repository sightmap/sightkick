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

type resolvedRef struct {
	locators []string
	props    []sm.ComponentPropertyDef
}

// resolveRef turns a component ref (by name) into concrete locators + declared
// properties. Returns false on an unknown name. (There is deliberately no css
// escape hatch: the manifest addresses the DOM only through corpus components.)
func (cc *compiler) resolveRef(
	component string,
	comps map[string]sm.ComponentDef,
	names []string,
	toolName string,
) (resolvedRef, bool) {
	if component == "" {
		cc.errf("compile.ref", toolName, "a step in %q references no component", toolName)
		return resolvedRef{}, false
	}
	def, ok := comps[component]
	if !ok {
		cc.errf("compile.ref-unresolved", toolName,
			"tool %q references unknown component %q. Available: %s", toolName, component, candidateList(names))
		return resolvedRef{}, false
	}
	return resolvedRef{locators: def.Selectors, props: def.Properties}, true
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

// resolveProperty resolves a property name against a resolved component ref.
func (cc *compiler) resolveProperty(propName string, r resolvedRef, toolName string) (Extractor, bool) {
	return cc.resolvePropertyDef(propName, r.props, toolName)
}

// compileQuery parses a compquery path and resolves each part against the
// in-scope components: component name -> selectors, each predicate's property ->
// extractor. The result is a sightmap-free descendant chain the runtime resolves
// by DOM containment. Reuses the sightmap library's parser (grammar parity with
// `sightmap browser` queries); the library's live resolver stays out of the
// runtime (the firewall).
func (cc *compiler) compileQuery(queryStr string, comps map[string]sm.ComponentDef, names []string, known map[string]bool, toolName string) (Path, bool) {
	q, err := compquery.ParseQuery(queryStr)
	if err != nil {
		cc.errf("compile.query-parse", toolName, "tool %q has an invalid query %q: %v", toolName, queryStr, err)
		return nil, false
	}
	ok := true
	var path Path
	for _, part := range q.Parts {
		def, found := comps[part.Name]
		if !found {
			cc.errf("compile.query-ref", toolName,
				"tool %q query references unknown component %q. Available: %s", toolName, part.Name, candidateList(names))
			ok = false
			continue
		}
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
		path = append(path, pp)
	}
	if q.Index >= 0 {
		cc.warnf("compile.query-index", toolName, "tool %q query occurrence index #%d is not yet supported; ignoring", toolName, q.Index)
	}
	return path, ok
}

func (cc *compiler) validateTemplate(s string, known map[string]bool, toolName string) {
	for _, tok := range templateParams(s) {
		if !known[tok] {
			cc.errf("compile.param", toolName, "tool %q references unknown param {{%s}}", toolName, tok)
		}
	}
}

func (cc *compiler) compileWhere(where map[string]string, r resolvedRef, known map[string]bool, toolName string) *Where {
	if len(where) == 0 {
		return nil
	}
	keys := make([]string, 0, len(where))
	for k := range where {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic when (mis)authored with multiple keys
	if len(keys) > 1 {
		cc.warnf("compile.where-multi", toolName, "tool %q where-clause has multiple keys; using %q", toolName, keys[0])
	}
	prop := keys[0]
	equals := where[prop]
	cc.validateTemplate(equals, known, toolName)
	ex, ok := cc.resolveProperty(prop, r, toolName)
	if !ok {
		return nil
	}
	return &Where{Property: prop, Extractor: ex, Equals: equals}
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
		if body.Query != "" {
			path, ok := cc.compileQuery(body.Query, comps, names, known, toolName)
			if !ok {
				return Step{}, false
			}
			cc.validateTemplate(body.Value, known, toolName)
			return Step{Op: "fill", Path: path, Value: body.Value}, true
		}
		r, ok := cc.resolveRef(body.Component, comps, names, toolName)
		if !ok {
			return Step{}, false
		}
		cc.validateTemplate(body.Value, known, toolName)
		return Step{Op: "fill", Locators: r.locators, Value: body.Value, Where: cc.compileWhere(body.Where, r, known, toolName)}, true

	case "click":
		if body.Query != "" {
			path, ok := cc.compileQuery(body.Query, comps, names, known, toolName)
			if !ok {
				return Step{}, false
			}
			return Step{Op: "click", Path: path}, true
		}
		r, ok := cc.resolveRef(body.Component, comps, names, toolName)
		if !ok {
			return Step{}, false
		}
		return Step{Op: "click", Locators: r.locators, Where: cc.compileWhere(body.Where, r, known, toolName)}, true

	case "wait_for":
		timeout := body.TimeoutMs
		if timeout == 0 {
			timeout = 5000
		}
		if body.Query != "" {
			path, ok := cc.compileQuery(body.Query, comps, names, known, toolName)
			if !ok {
				return Step{}, false
			}
			return Step{Op: "waitFor", Path: path, TimeoutMs: timeout}, true
		}
		r, ok := cc.resolveRef(body.Component, comps, names, toolName)
		if !ok {
			return Step{}, false
		}
		return Step{Op: "waitFor", Locators: r.locators, TimeoutMs: timeout, Where: cc.compileWhere(body.Where, r, known, toolName)}, true

	case "collect":
		r, ok := cc.resolveRef(body.Component, comps, names, toolName)
		if !ok {
			return Step{}, false
		}
		fields := map[string]Field{}
		fieldNames := make([]string, 0, len(body.Fields))
		for f := range body.Fields {
			fieldNames = append(fieldNames, f)
		}
		sort.Strings(fieldNames)
		for _, f := range fieldNames {
			spec := body.Fields[f]
			ex, ok := cc.resolveProperty(spec.Property, r, toolName)
			if ok {
				fields[f] = Field{Property: spec.Property, Extractor: ex}
			}
		}
		return Step{Op: "collect", Locators: r.locators, Fields: fields}, true

	default:
		cc.errf("compile.step", toolName, "tool %q has an unrecognized step op %q", toolName, op)
		return Step{}, false
	}
}

func (cc *compiler) compileReturn(ret *ReturnDef, comps map[string]sm.ComponentDef, names []string, known map[string]bool, toolName string) *Return {
	if ret == nil {
		return nil
	}
	if ret.Extract == nil {
		out := &Return{Kind: "value"}
		if ret.Description != "" {
			out.Description = ret.Description
		}
		return out
	}
	r, ok := cc.resolveRef(ret.Extract.Component, comps, names, toolName)
	if !ok {
		return nil
	}
	out := &Return{Kind: "value", Locators: r.locators}
	if ret.Description != "" {
		out.Description = ret.Description
	}
	if ex, ok := cc.resolveProperty(ret.Extract.Property, r, toolName); ok {
		out.Extractor = &ex
	}
	if w := cc.compileWhere(ret.Extract.Where, r, known, toolName); w != nil {
		out.Where = w
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
	r, ok := cc.resolveRef(ref.Component, comps, names, toolName)
	if !ok {
		return nil
	}
	guard := &Guard{Kind: kind, Locators: r.locators}
	if w := cc.compileWhere(ref.Where, r, known, toolName); w != nil {
		guard.Where = w
	}
	return guard
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
