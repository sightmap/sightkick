package gen

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/sightmap/sightmap/go/compquery"
	"github.com/sightmap/sightmap/go/match"
	sm "github.com/sightmap/sightmap/go/sightmap"
)

// Verify checks each tool's returns extractors against the view's captured
// snapshots and warns when a declared field resolves empty on every captured
// instance — the silent-empty class of bug the compiler cannot catch without a
// DOM (a structurally-valid IR whose extractors yield "" at runtime).
//
// It runs the sightmap matcher over each capture (so it resolves properties
// exactly as the corpus defines them, including SEP-0010 cross-refs and the
// role-less text fallback) and reports, per tool:
//
//   - verify.no-capture   the tool's view has no captured tree to check against
//   - verify.no-rows      the row/target component matched 0 captured instances
//   - verify.field-empty  a field's property is empty on every captured instance
//
// corpusDir is the .sightmap directory (its snapshots/ subtree holds captures).
func Verify(m *Manifest, corpus *sm.Corpus, corpusDir string) []Diagnostic {
	var diags []Diagnostic
	matcher := match.NewMatcher(corpus)
	cache := map[string][]map[*sm.ComponentNode]*sm.ComponentMatch{}

	// captures returns the matched component maps for a view's captures (cached),
	// the view's representative URL, and whether the view exists.
	captures := func(viewName string) ([]map[*sm.ComponentNode]*sm.ComponentMatch, *sm.ViewDef, bool) {
		view := corpus.ViewByName(viewName)
		if view == nil {
			return nil, nil, false
		}
		if got, ok := cache[viewName]; ok {
			return got, view, true
		}
		dir := filepath.Join(corpusDir, "snapshots", view.SnapBasename())
		trees, _ := filepath.Glob(filepath.Join(dir, "*.snap.tree.json"))
		var out []map[*sm.ComponentNode]*sm.ComponentMatch
		for _, tp := range trees {
			data, err := os.ReadFile(tp)
			if err != nil {
				continue
			}
			var root sm.ComponentNode
			if err := json.Unmarshal(data, &root); err != nil {
				continue
			}
			out = append(out, matcher.Match(&root, view.URL))
		}
		cache[viewName] = out
		return out, view, true
	}

	for _, tool := range m.Tools {
		if tool.Returns == nil {
			continue
		}
		where := "tool " + tool.Name
		caps, _, ok := captures(tool.EnsureView)
		if !ok {
			continue // no such view; a separate compile diagnostic already covers it
		}
		if len(caps) == 0 {
			diags = append(diags, warnf("verify.no-capture", where,
				"tool %q returns data but view %q has no captured snapshot to verify against; capture the view and re-run --verify",
				tool.Name, tool.EnsureView))
			continue
		}

		switch {
		case tool.Returns.List != nil:
			comp := baseComponent(tool.Returns.List.Rows)
			insts := instancesOf(caps, comp)
			if len(insts) == 0 {
				diags = append(diags, warnf("verify.no-rows", where,
					"tool %q lists rows of %q but no captured instance was found in view %q; the field extractors can't be verified (capture the view with rows present)",
					tool.Name, comp, tool.EnsureView))
				break
			}
			for fieldName, fd := range tool.Returns.List.Fields {
				prop := fd.Property
				if prop == "" {
					prop = fieldName
				}
				if !anyHasProp(insts, prop) {
					diags = append(diags, warnf("verify.field-empty", where,
						"tool %q list field %q (%s.%s) resolved empty on all %d captured row(s) in view %q",
						tool.Name, fieldName, comp, prop, len(insts), tool.EnsureView))
				}
			}

		case tool.Returns.Value != nil:
			comp := baseComponent(tool.Returns.Value.Query)
			insts := instancesOf(caps, comp)
			if len(insts) == 0 {
				diags = append(diags, warnf("verify.no-rows", where,
					"tool %q returns a value from %q but no captured instance was found in view %q",
					tool.Name, comp, tool.EnsureView))
				break
			}
			if prop := tool.Returns.Value.Property; prop != "" && !anyHasProp(insts, prop) {
				diags = append(diags, warnf("verify.field-empty", where,
					"tool %q value (%s.%s) resolved empty on all %d captured instance(s) in view %q",
					tool.Name, comp, prop, len(insts), tool.EnsureView))
			}
		}
	}
	return diags
}

// baseComponent returns the component name a compquery ultimately addresses: the
// last path part (the target entity). Falls back to the raw string if the query
// doesn't parse (a compile diagnostic already covers that case).
func baseComponent(query string) string {
	q, err := compquery.ParseQuery(query)
	if err != nil || len(q.Parts) == 0 {
		return query
	}
	return q.Parts[len(q.Parts)-1].Name
}

// instancesOf collects every match of the named component across all captures.
func instancesOf(caps []map[*sm.ComponentNode]*sm.ComponentMatch, name string) []*sm.ComponentMatch {
	var out []*sm.ComponentMatch
	for _, m := range caps {
		for _, cm := range m {
			if cm.Name == name {
				out = append(out, cm)
			}
		}
	}
	return out
}

// anyHasProp reports whether any instance carries a non-empty value for prop.
func anyHasProp(insts []*sm.ComponentMatch, prop string) bool {
	for _, cm := range insts {
		if pv, ok := cm.Property(prop); ok && pv.Value != "" {
			return true
		}
	}
	return false
}
