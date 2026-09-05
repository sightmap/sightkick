package gen

import (
	"fmt"
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

// resolveExtractor maps a sightmap property `extract:` string (SEP-0010 grammar)
// to an IR Extractor, resolved against the flattened component set.
// Grammar: text | attr=NAME | exists:PATH | PATH.prop.
//
// `text` yields the node's rendered text (accessible name, falling back to
// visible innerText/textContent) — the sightmap lib does the same offline, so
// there is a single text mode. The older inner_text/text_only aliases are
// rejected here to match `sightmap validate` (which never accepted them).
//
// The PATH forms are sightmap cross-references over the component tree (NOT raw
// CSS): `exists:Child` and `Parent.Child.prop` name declared child COMPONENTS.
// We resolve them to a CSS `within` selector expressed RELATIVE to the owner
// element (the runtime does owner.querySelector(within)), by looking each child
// up in the flattened set and stripping the owner's selector prefix. The leaf
// property's own kind (text/attr/...) is carried through. A path that can't be
// resolved falls back to treating the string as a raw descendant selector
// (legacy behavior) with a warning.
func (cc *compiler) resolveExtractor(extract string, owner sm.ComponentDef, all []sm.ComponentDef, where string) Extractor {
	return cc.resolveExtractorDepth(extract, owner, all, where, 0)
}

// maxExtractDepth backstops a cyclic Comp.prop chain (A.b -> B.c -> A.b ...).
// Real paths are shallow (a handful of segments); anything deeper is a cycle.
const maxExtractDepth = 16

func (cc *compiler) resolveExtractorDepth(extract string, owner sm.ComponentDef, all []sm.ComponentDef, where string, depth int) Extractor {
	s := strings.TrimSpace(extract)
	switch s {
	case "", "text":
		return Extractor{Kind: "text"}
	case "inner_text", "text_only":
		// Rejected to match `sightmap validate`. `text` now yields rendered node
		// text (incl. role-less nodes) on both sides, so it fully subsumes these.
		cc.errf("compile.extract-mode", where,
			"extract mode %q is not supported; use `text` (it captures rendered node text, including role-less nodes)", s)
		return Extractor{Kind: "text"}
	}
	if name, ok := strings.CutPrefix(s, "attr="); ok {
		return Extractor{Kind: "attr", Attr: name}
	}
	if depth > maxExtractDepth {
		cc.errf("compile.extract-cycle", where,
			"extract %q exceeds max resolution depth %d (a cyclic Comp.prop reference?)", s, maxExtractDepth)
		return Extractor{Kind: "text"}
	}
	if path, ok := strings.CutPrefix(s, "exists:"); ok {
		segs := strings.Split(path, ".")
		if _, isChild := childOf(segs[0], owner, all); isChild {
			if prefixes, _, ok := cc.resolveCompPath(segs, owner, all, where); ok {
				return Extractor{Kind: "exists", Within: strings.Join(prefixes, ", ")}
			}
		}
		return Extractor{Kind: "exists", Within: path} // raw selector (legacy)
	}
	// PATH.prop cross-reference: last segment is the property, the rest is a
	// component path descending from `owner`. Only treated as a cross-reference
	// when the first segment names a real child component; otherwise it's a raw
	// CSS descendant selector (the legacy grammar) and used verbatim, silently.
	if segs := strings.Split(s, "."); len(segs) >= 2 {
		compPath := segs[:len(segs)-1]
		if _, isChild := childOf(compPath[0], owner, all); isChild {
			propName := segs[len(segs)-1]
			if prefixes, leaf, ok := cc.resolveCompPath(compPath, owner, all, where); ok {
				for _, p := range leaf.Properties {
					if p.Name == propName {
						ex := cc.resolveExtractorDepth(p.Extract, leaf, all, where, depth+1)
						ex.Within = cc.composeWithin(prefixes, ex.Within, where, s)
						return ex
					}
				}
				cc.warnf("compile.extract-prop", where,
					"extract %q: property %q is not declared on component %q", s, propName, leaf.Name)
			} else {
				cc.warnf("compile.extract-ref", where,
					"extract %q: component path did not resolve past %q", s, compPath[0])
			}
		}
	}
	return Extractor{Kind: "text", Within: s} // legacy raw-CSS-descendant fallback
}

// composeWithin distributes the resolved component-path prefixes over the leaf
// extractor's own within. The common case (leaf within == "") is just the
// comma-joined prefixes. A leaf that itself carries a within (a nested
// Comp.prop) only fully generalizes for a single prefix; the rare multi-selector
// + nested-within combination is warned and best-effort joined.
func (cc *compiler) composeWithin(prefixes []string, leafWithin, where, extract string) string {
	if leafWithin == "" {
		return strings.Join(prefixes, ", ")
	}
	if len(prefixes) == 1 {
		return joinSel(prefixes[0], leafWithin)
	}
	cc.warnf("compile.extract-compose", where,
		"extract %q composes a multi-selector component path with a nested selector; the result may be imprecise", extract)
	return joinSel(strings.Join(prefixes, ", "), leafWithin)
}

// resolveCompPath walks a component path (e.g. ["Price"] or ["Row","Price"])
// descending from owner through the flattened child set, returning the combined
// selectors RELATIVE to owner (a cross-product alternative list when components
// carry multiple selectors) and the final component. ok is false if any segment
// can't be found.
func (cc *compiler) resolveCompPath(segs []string, owner sm.ComponentDef, all []sm.ComponentDef, where string) ([]string, sm.ComponentDef, bool) {
	cur := owner
	prefixes := []string{""}
	for _, seg := range segs {
		child, ok := childOf(seg, cur, all)
		if !ok {
			return nil, sm.ComponentDef{}, false
		}
		rels, clean := relSelectors(cur, child)
		if !clean {
			cc.warnf("compile.extract-relsel", where,
				"component %q's selector is not a clean descendant of %q; the extractor selector may be wrong at runtime", child.Name, cur.Name)
		}
		prefixes = crossJoin(prefixes, rels)
		cur = child
	}
	return prefixes, cur, true
}

// childOf finds the flattened component named `name` whose parent is `owner`
// (ParentChain == owner.ParentChain + owner.Name). Falls back to a name-only
// match when no parent-scoped one exists (children names are usually unique).
func childOf(name string, owner sm.ComponentDef, all []sm.ComponentDef) (sm.ComponentDef, bool) {
	want := append(append([]string{}, owner.ParentChain...), owner.Name)
	var byName *sm.ComponentDef
	for i := range all {
		if all[i].Name != name {
			continue
		}
		if chainEq(all[i].ParentChain, want) {
			return all[i], true
		}
		if byName == nil {
			byName = &all[i]
		}
	}
	if byName != nil {
		return *byName, true
	}
	return sm.ComponentDef{}, false
}

// descendantOf finds the flattened component named `name` anywhere beneath
// `owner`, preferring the shallowest match. A compquery part is a *descendant*
// step, so `AddVariableMenu Trigger` may name a grandchild, not just a child.
func descendantOf(name string, owner sm.ComponentDef, all []sm.ComponentDef) (sm.ComponentDef, bool) {
	want := append(append([]string{}, owner.ParentChain...), owner.Name)
	var best *sm.ComponentDef
	for i := range all {
		if all[i].Name != name || !chainHasPrefix(all[i].ParentChain, want) {
			continue
		}
		if best == nil || len(all[i].ParentChain) < len(best.ParentChain) {
			best = &all[i]
		}
	}
	if best != nil {
		return *best, true
	}
	return sm.ComponentDef{}, false
}

func chainHasPrefix(chain, prefix []string) bool {
	if len(chain) < len(prefix) {
		return false
	}
	return chainEq(chain[:len(prefix)], prefix)
}

func chainEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// relSelectors expresses each of child's selectors relative to owner (stripping
// the matching owner-selector prefix so owner.querySelector(rel) resolves it),
// deduped. Flattened child selectors are owner selector + " " + child selector.
// clean is false if any child selector isn't a descendant of an owner selector
// (flattening didn't prepend it) — the raw selector is kept as a fallback.
func relSelectors(owner, child sm.ComponentDef) (rels []string, clean bool) {
	clean = true
	seen := map[string]bool{}
	for _, cs := range child.Selectors {
		rel, ok := stripOwnerPrefix(cs, owner.Selectors)
		if !ok {
			clean = false
		}
		if !seen[rel] {
			seen[rel] = true
			rels = append(rels, rel)
		}
	}
	if len(rels) == 0 {
		return []string{""}, clean
	}
	return rels, clean
}

// stripOwnerPrefix returns childSel with a matching "<ownerSel> " prefix removed
// (ok=true), trying each owner selector; ok=false (raw childSel) if none match.
func stripOwnerPrefix(childSel string, ownerSels []string) (string, bool) {
	for _, os := range ownerSels {
		if os == "" {
			continue
		}
		if prefix := os + " "; strings.HasPrefix(childSel, prefix) {
			return strings.TrimSpace(childSel[len(prefix):]), true
		}
	}
	return childSel, false
}

// crossJoin distributes each prefix over each suffix with a descendant
// combinator (cross product), so multi-selector components compose into a
// comma-separated alternative list. Empty operands pass through.
func crossJoin(prefixes, suffixes []string) []string {
	if len(suffixes) == 0 {
		return prefixes
	}
	if len(prefixes) == 0 {
		return suffixes
	}
	var out []string
	for _, p := range prefixes {
		for _, s := range suffixes {
			out = append(out, joinSel(p, s))
		}
	}
	return out
}

func joinSel(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " " + b
	}
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

// effectiveComponents returns the name->def map (plus a sorted name list for
// candidate diagnostics) of the components a tool may reference.
//
// For a named view it mirrors Corpus.ComponentsForURL's merge: view components
// win on name collision, then non-colliding globals. For an ensure_view-less
// tool (view == nil) it isn't scoped to one route, so it resolves against the
// WHOLE corpus: every view's components plus globals, deduped by name. On a
// same-name collision across views the first occurrence in view order wins
// (globals are added last, so a view name shadows a like-named global).
func (cc *compiler) effectiveComponents(view *sm.ViewDef) (map[string]sm.ComponentDef, []string) {
	byName := map[string]sm.ComponentDef{}
	if view != nil {
		// Named view: view components win on name collision (last-wins within the
		// view, matching Corpus.ComponentsForURL), then non-colliding globals.
		for _, vc := range view.Components {
			byName[vc.Name] = vc
		}
	} else {
		// No ensure_view: the tool isn't scoped to one route, so resolve against
		// the whole corpus. Add every view's components, first-wins in view order,
		// so a same-name component across views doesn't crash (first declared wins).
		for i := range cc.c.Views {
			for _, vc := range cc.c.Views[i].Components {
				if _, ok := byName[vc.Name]; !ok {
					byName[vc.Name] = vc
				}
			}
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

// allComponents returns the flattened component defs in scope, NOT deduped by
// name — so a Comp.prop lookup can disambiguate same-named children by
// ParentChain. For a named view that's its own components + globals; for an
// ensure_view-less tool (view == nil) it's every view's components + globals,
// matching the whole-corpus scope of effectiveComponents.
func (cc *compiler) allComponents(view *sm.ViewDef) []sm.ComponentDef {
	var all []sm.ComponentDef
	if view != nil {
		all = append(all, view.Components...)
	} else {
		for i := range cc.c.Views {
			all = append(all, cc.c.Views[i].Components...)
		}
	}
	all = append(all, cc.c.GlobalComponents...)
	return all
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

// resolveProperty resolves a declared property name on `owner` to its IR
// extractor, following any Comp.prop cross-references against the flattened set.
func (cc *compiler) resolveProperty(propName string, owner sm.ComponentDef, all []sm.ComponentDef, toolName string) (Extractor, bool) {
	for _, p := range owner.Properties {
		if p.Name == propName {
			return cc.resolveExtractor(p.Extract, owner, all, toolName), true
		}
	}
	var have []string
	for _, p := range owner.Properties {
		have = append(have, p.Name)
	}
	cc.errf("compile.prop-unresolved", toolName,
		"property %q is not declared on component %q (have: %s)", propName, owner.Name, candidateList(have))
	return Extractor{}, false
}

// compileQuery parses a compquery path and resolves each part against the
// in-scope components: component name -> selectors, each predicate's property ->
// extractor. The result is a sightmap-free descendant chain the runtime resolves
// by DOM containment. Reuses the sightmap library's parser (grammar parity with
// `sightmap browser` queries); the library's live resolver stays out of the
// runtime (the firewall).
// The returned def is the LAST part's component (the target), so a collect can
// resolve its fields against the row.
func (cc *compiler) compileQuery(queryStr string, comps map[string]sm.ComponentDef, all []sm.ComponentDef, names []string, known map[string]bool, toolName string) (*Query, sm.ComponentDef, bool) {
	if strings.TrimSpace(queryStr) == "" {
		cc.errf("compile.query-missing", toolName, "a step/ref in %q has no query", toolName)
		return nil, sm.ComponentDef{}, false
	}
	q, err := compquery.ParseQuery(queryStr)
	if err != nil {
		cc.errf("compile.query-parse", toolName, "tool %q has an invalid query %q: %v", toolName, queryStr, err)
		return nil, sm.ComponentDef{}, false
	}
	ok := true
	var parts []PathPart
	var targetDef sm.ComponentDef // last part's component = the target
	havePrev := false
	for _, part := range q.Parts {
		var def sm.ComponentDef
		var found bool
		// Resolve each part after the first inside the previous part's subtree.
		// Child names are unique per-parent, not globally — every dropdown has a
		// `Trigger` — and the view-wide map keeps only one def per name, so a
		// flat lookup here silently targets a different component's child.
		if havePrev {
			def, found = descendantOf(part.Name, targetDef, all)
			if !found {
				// Not modelled as a descendant. Fall back to a corpus-wide match so
				// incompletely-modelled hierarchies still compile (the runtime scopes
				// by DOM containment regardless), but warn: this is exactly the shape
				// of an authoring typo that would otherwise fail only at runtime.
				if fb, fok := comps[part.Name]; fok {
					cc.warnf("compile.query-descendant", toolName,
						"tool %q query: %q is not a descendant of %q in the corpus; using a corpus-wide match (confirm the DOM nests it there at runtime)", toolName, part.Name, targetDef.Name)
					def, found = fb, true
				}
			}
		} else {
			def, found = comps[part.Name]
		}
		if !found {
			cc.errf("compile.query-ref", toolName,
				"tool %q query references unknown component %q. Available: %s", toolName, part.Name, candidateList(names))
			ok = false
			continue
		}
		targetDef = def
		havePrev = true
		pp := PathPart{Locators: def.Selectors}
		for _, pr := range part.Preds {
			ex, pok := cc.resolveProperty(pr.Prop, def, all, toolName)
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
		return nil, sm.ComponentDef{}, false
	}
	query := &Query{Parts: parts}
	if q.Index >= 0 {
		idx := q.Index
		query.Index = &idx
	}
	return query, targetDef, true
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
	comps map[string]sm.ComponentDef, all []sm.ComponentDef, names []string,
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

	case "goto":
		if body.URL == "" {
			cc.errf("compile.goto", toolName, "tool %q has a goto step with no url", toolName)
			return Step{}, false
		}
		cc.validateTemplate(body.URL, known, toolName)
		return Step{Op: "goto", URL: body.URL}, true

	case "fill":
		q, _, ok := cc.compileQuery(body.Query, comps, all, names, known, toolName)
		if !ok {
			return Step{}, false
		}
		cc.validateTemplate(body.Value, known, toolName)
		return Step{Op: "fill", Query: q, Value: body.Value}, true

	case "click":
		q, _, ok := cc.compileQuery(body.Query, comps, all, names, known, toolName)
		if !ok {
			return Step{}, false
		}
		return Step{Op: "click", Query: q}, true

	case "wait_for":
		timeout := body.TimeoutMs
		if timeout == 0 {
			timeout = 5000
		}
		hasQuery, hasView := strings.TrimSpace(body.Query) != "", strings.TrimSpace(body.View) != ""
		switch {
		case hasQuery && hasView:
			cc.errf("compile.wait-for-shape", toolName, "tool %q wait_for has both query and view; use exactly one", toolName)
			return Step{}, false
		case hasView:
			v := cc.c.ViewByName(body.View)
			if v == nil {
				cc.errf("compile.wait-for-view", toolName, "%s wait_for names unknown view %q", toolName, body.View)
				return Step{}, false
			}
			return Step{Op: "waitFor", View: v.Name, Route: v.Route, TimeoutMs: timeout}, true
		case hasQuery:
			q, _, ok := cc.compileQuery(body.Query, comps, all, names, known, toolName)
			if !ok {
				return Step{}, false
			}
			return Step{Op: "waitFor", Query: q, TimeoutMs: timeout}, true
		default:
			cc.errf("compile.wait-for-shape", toolName, "tool %q wait_for has neither query nor view", toolName)
			return Step{}, false
		}

	case "keypress":
		if strings.TrimSpace(body.Key) == "" {
			cc.errf("compile.keypress", toolName, "tool %q has a keypress step with no key", toolName)
			return Step{}, false
		}
		return Step{Op: "keypress", Key: body.Key}, true

	default:
		cc.errf("compile.step", toolName, "tool %q has an unrecognized step op %q", toolName, op)
		return Step{}, false
	}
}

// compileFields resolves a set of output fields against a row component's
// declared properties (deterministic order). Shared by list returns.
func (cc *compiler) compileFields(fields map[string]FieldDef, row sm.ComponentDef, all []sm.ComponentDef, toolName string) map[string]Field {
	out := map[string]Field{}
	names := make([]string, 0, len(fields))
	for f := range fields {
		names = append(names, f)
	}
	sort.Strings(names)
	for _, f := range names {
		spec := fields[f]
		if ex, ok := cc.resolveProperty(spec.Property, row, all, toolName); ok {
			out[f] = Field{Property: spec.Property, Extractor: ex}
		}
	}
	return out
}

func (cc *compiler) compileReturn(ret *ReturnDef, comps map[string]sm.ComponentDef, all []sm.ComponentDef, names []string, known map[string]bool, toolName string) *Return {
	if ret == nil {
		return nil
	}
	if ret.Value != nil && ret.List != nil {
		cc.errf("compile.return-shape", toolName, "tool %q returns has both value and list; use exactly one", toolName)
		return nil
	}

	// list: map a compquery over every match -> one Fields-shaped object per row.
	if ret.List != nil {
		q, row, ok := cc.compileQuery(ret.List.Rows, comps, all, names, known, toolName)
		if !ok {
			return nil
		}
		out := &Return{Kind: "list", Query: q, Fields: cc.compileFields(ret.List.Fields, row, all, toolName)}
		if ret.Description != "" {
			out.Description = ret.Description
		}
		return out
	}

	// value: read one declared property off the first match.
	if ret.Value != nil {
		q, target, ok := cc.compileQuery(ret.Value.Query, comps, all, names, known, toolName)
		if !ok {
			return nil
		}
		out := &Return{Kind: "value", Query: q}
		if ret.Description != "" {
			out.Description = ret.Description
		}
		if ex, ok := cc.resolveProperty(ret.Value.Property, target, all, toolName); ok {
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

func (cc *compiler) compileToolGuard(g *GuardBody, comps map[string]sm.ComponentDef, all []sm.ComponentDef, names []string, known map[string]bool, toolName string) *Guard {
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
	q, _, ok := cc.compileQuery(ref.Query, comps, all, names, known, toolName)
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
	all := cc.allComponents(view)

	tool := Tool{Name: t.Name, Mode: mode, InputSchema: inputSchema(t.Params)}
	if t.Description != "" {
		tool.Description = t.Description
	}
	if view != nil {
		tool.EnsureView = &EnsureView{View: view.Name, Route: view.Route}
	}
	if t.Guard != nil {
		tool.Guard = cc.compileToolGuard(t.Guard, comps, all, names, known, t.Name)
	}
	for _, step := range t.Steps {
		op, body, ok := stepOp(step)
		if !ok {
			cc.errf("compile.step", t.Name, "tool %q has a step that is not a single-key mapping", t.Name)
			continue
		}
		if s, ok := cc.compileStep(op, body, comps, all, names, known, t.Name); ok {
			if strings.TrimSpace(body.When) != "" {
				cc.validateTemplate(body.When, known, t.Name)
				s.When = body.When
			}
			tool.Steps = append(tool.Steps, s)
		}
	}
	if tool.Steps == nil {
		tool.Steps = []Step{}
	}
	tool.Returns = cc.compileReturn(t.Returns, comps, all, names, known, t.Name)
	// Make the result self-describing: agents read the tool description in
	// getTools(), so baking the result shape (envelope key + list field names)
	// there lets them parse first-try instead of guessing 'items'/field names.
	if hint := returnHint(tool.Returns); hint != "" {
		tool.Description = strings.TrimSpace(tool.Description + " " + hint)
	}
	return tool
}

// returnHint renders a one-line description of a tool's result shape, naming the
// envelope key the runtime actually uses (`value` or `items`) and, for a list,
// the per-row field keys. Empty when there is nothing to read.
func returnHint(ret *Return) string {
	if ret == nil {
		return ""
	}
	switch ret.Kind {
	case "list":
		keys := make([]string, 0, len(ret.Fields))
		for k := range ret.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var hint string
		if len(keys) == 0 {
			hint = "Returns `items`: a list."
		} else {
			hint = fmt.Sprintf("Returns `items`: a list of objects with keys {%s}.", strings.Join(keys, ", "))
		}
		if ret.Description != "" {
			hint += " " + ensureSentence(ret.Description)
		}
		return hint
	case "value":
		if ret.Query == nil && ret.Extractor == nil {
			return "" // description-only return with nothing to read
		}
		if ret.Description != "" {
			return "Returns `value` (string): " + ensureSentence(ret.Description)
		}
		return "Returns `value`: a single string."
	}
	return ""
}

// ensureSentence trims and adds a trailing period if missing (for clean joins).
func ensureSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "!") && !strings.HasSuffix(s, "?") {
		s += "."
	}
	return s
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
			if a.Tool == b.Tool {
				cc.errf("compile.journey-self-loop", j.Name, "journey %q lists %q twice in a row (a self-edge yields no guidance)", j.Name, a.Tool)
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
