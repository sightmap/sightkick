package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sightkick/generator/internal/gen"
)

// A traceRec is the structured record of one --via cli tool run: the internal
// work the tool does that its ToolResult hides. It captures the ensure_view
// check, the guard evaluation, each step's resolved command, and — for a tool
// that returns data — the provenance of that data (which component query ran,
// which nodes it matched, and what each field's extractor read off them). It is
// what `--trace` writes, so a demo can show the machinery, not just the result.
//
// Everything here is already computed while the tool runs; the tracer only
// records it. A nil *traceRec on the session disables all recording, so the
// hooks are cheap no-ops when tracing is off.
type traceRec struct {
	Tool       string           `json:"tool"`
	Via        string           `json:"via"`
	Params     map[string]any   `json:"params,omitempty"`
	EnsureView *ensureViewTrace `json:"ensureView,omitempty"`
	Guard      *guardTrace      `json:"guard,omitempty"`
	Steps      []stepTrace      `json:"steps"`
	Returns    *returnTrace     `json:"returns,omitempty"`
	// Result is the tool's public ToolResult (ok/value/items/guidance), the same
	// object printed to stdout, folded in so the trace is self-contained.
	Result map[string]any `json:"result,omitempty"`
}

// ensureViewTrace records the idempotent self-positioning precondition: the
// view the tool expects, and whether the live page matched it.
type ensureViewTrace struct {
	View       string `json:"view"`
	Route      string `json:"route,omitempty"`
	Matched    bool   `json:"matched"`
	Screenshot string `json:"screenshot,omitempty"`
}

// queryPart is one component in a compiled query's descendant chain: the
// flattened CSS locator(s) that match it, plus the predicates that filter it
// (e.g. flight_no*="UA 2371"). Together the parts are the resolved form of an
// authored compquery like `FlightCard[flight_no*="UA 2371"] FareOption[...]`.
type queryPart struct {
	Locators []string   `json:"locators"`
	Preds    []predView `json:"preds,omitempty"`
}

// predView is one compiled predicate: read a property with Extractor and compare
// it to Value with Op ("=", "^=", "*="), optionally case-insensitively.
type predView struct {
	Property  string        `json:"property,omitempty"`
	Op        string        `json:"op"`
	Value     string        `json:"value"`
	CI        bool          `json:"ci,omitempty"`
	Extractor gen.Extractor `json:"extractor"`
}

// guardTrace records a tool's idempotency guard: the component query it checks
// and whether the effect was already applied (so the steps were skipped).
type guardTrace struct {
	Kind     string      `json:"kind"` // present | absent
	Query    string      `json:"query"`
	Compiled []queryPart `json:"compiled,omitempty"`
	Matched  int         `json:"matched"`
	Skipped  bool        `json:"skipped"`
}

// stepTrace records one executed step: the authored op, the query/value/url
// after params were interpolated in, the compiled CSS the query resolves to,
// the exact `sightmap browser` command that ran, and the outcome. Screenshot
// is the page state captured right after the step — the interaction's result.
type stepTrace struct {
	Index         int         `json:"index"`
	Op            string      `json:"op"`
	QueryTemplate string      `json:"queryTemplate,omitempty"` // as authored, with {{param}}
	Query         string      `json:"query,omitempty"`         // interpolated
	Compiled      []queryPart `json:"compiled,omitempty"`      // resolved locators + preds
	Value         string      `json:"value,omitempty"`
	URL           string      `json:"url,omitempty"`
	Key           string      `json:"key,omitempty"`
	TimeoutMs     int         `json:"timeoutMs,omitempty"`
	Command       []string    `json:"command"` // sightmap args, sans the corpus flag
	Ok            bool        `json:"ok"`
	Error         string      `json:"error,omitempty"`
	Screenshot    string      `json:"screenshot,omitempty"`
}

// returnTrace records where a tool's result came from: the list/value query,
// the compiled CSS it resolves to, the per-field extractors, and one entry per
// matched node carrying that node's component name and the values read off it.
// This is the provenance behind a returns: clause.
type returnTrace struct {
	Kind          string                `json:"kind"` // value | list
	QueryTemplate string                `json:"queryTemplate,omitempty"`
	Query         string                `json:"query"`
	Compiled      []queryPart           `json:"compiled,omitempty"`
	Fields        map[string]fieldTrace `json:"fields,omitempty"`
	Matches       []matchTrace          `json:"matches"`
	Screenshot    string                `json:"screenshot,omitempty"`
}

// fieldTrace pairs an output field with the extractor that reads it — the
// compiled form of the corpus `extract:` directive named by the manifest.
type fieldTrace struct {
	Property  string        `json:"property,omitempty"`
	Extractor gen.Extractor `json:"extractor"`
}

// matchTrace is one node a returns query matched: its snapshot node id, the
// corpus component it matched, and the field values read off it.
type matchTrace struct {
	NodeID    string            `json:"nodeId"`
	Component string            `json:"component,omitempty"`
	Values    map[string]string `json:"values"`
}

// compiledParts renders a compiled query as its resolved descendant chain: one
// queryPart per component, carrying the flattened CSS locator(s) and the
// predicates that filter it. This is the faithful resolved form of the authored
// compquery string (which the trace also keeps verbatim).
func compiledParts(q *gen.Query) []queryPart {
	if q == nil {
		return nil
	}
	parts := make([]queryPart, 0, len(q.Parts))
	for _, p := range q.Parts {
		qp := queryPart{Locators: p.Locators}
		for _, pred := range p.Preds {
			qp.Preds = append(qp.Preds, predView{
				Property:  pred.Property,
				Op:        pred.Op,
				Value:     pred.Value,
				CI:        pred.CI,
				Extractor: pred.Extractor,
			})
		}
		parts = append(parts, qp)
	}
	return parts
}

// stepQuery pulls the compiled query out of a step, whichever op carries one.
// Returns nil for ops that address no element (goto, keypress, navigate).
func stepQuery(s *gen.Step) *gen.Query {
	if s == nil {
		return nil
	}
	return s.Query
}

// snap captures the live page to <dir>/<NN>-<tool>-<label>.png and returns the
// path, or "" if screenshots are disabled or the capture failed. A failed
// screenshot never fails the tool — it is demo scaffolding, not the work.
func (e *session) snap(label string) string {
	if e.shotDir == "" {
		return ""
	}
	e.shotSeq++
	name := fmt.Sprintf("%02d-%s-%s.png", e.shotSeq, sanitize(e.shotTool), sanitize(label))
	path := filepath.Join(e.shotDir, name)
	if err := e.sightmap("browser", "screenshot", "--out", path); err != nil {
		fmt.Fprintf(os.Stderr, "screenshot %q: %v\n", label, err)
		return ""
	}
	e.lastShot = path
	return path
}

// sanitize makes a label safe for a filename: lowercase, non-alphanumerics to
// dashes, collapsed and trimmed.
func sanitize(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// traceStepBegin appends a stepTrace for step i and returns a pointer to it, or
// nil when tracing is off. It resolves the query/value/url with the caller's
// params (what actually ran) and pulls the compiled CSS from the matching
// compiled step, so the record shows both the authored query and the concrete
// selector it resolves to.
func (e *session) traceStepBegin(i int, op string, body gen.StepBody, compiled *gen.Tool, args map[string]any) *stepTrace {
	if e.trace == nil {
		return nil
	}
	st := stepTrace{Index: i + 1, Op: op}
	if body.Query != "" {
		st.QueryTemplate = body.Query
		st.Query = interpolate(body.Query, args)
	}
	if body.Value != "" {
		st.Value = interpolate(body.Value, args)
	}
	if body.URL != "" {
		st.URL = interpolate(body.URL, args)
	}
	st.Key = body.Key
	st.TimeoutMs = body.TimeoutMs
	if cmd, err := stepArgs(op, body, args, e.defaultTimeoutMs); err == nil {
		st.Command = cmd
	}
	if compiled != nil && i < len(compiled.Steps) {
		if q := stepQuery(&compiled.Steps[i]); q != nil {
			st.Compiled = compiledParts(q)
		}
	}
	e.trace.Steps = append(e.trace.Steps, st)
	return &e.trace.Steps[len(e.trace.Steps)-1]
}

// traceStepEnd records a step's outcome and captures the page state it left
// behind — the interaction's visible result.
func (e *session) traceStepEnd(st *stepTrace, err error) {
	if st == nil {
		return
	}
	st.Ok = err == nil
	if err != nil {
		st.Error = err.Error()
	}
	st.Screenshot = e.snap(fmt.Sprintf("step%d-%s", st.Index, st.Op))
}

// writeTrace serializes the record to path, creating parent dirs as needed.
func writeTrace(path string, rec *traceRec) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
