package main

import (
	"encoding/json"
	"fmt"
	"os"
	osexec "os/exec"
	"regexp"
	"strconv"
	"strings"

	"sightkick/generator/internal/gen"

	"github.com/sightmap/sightmap/go/compquery"
	sm "github.com/sightmap/sightmap/go/sightmap"
)

// nativeExec runs a manifest tool's steps by shelling out to `sightmap
// browser <verb>` for each one, and resolves guards/returns by taking a
// single `sightmap snapshot --json` and querying it in-process with
// compquery — the same engine `sightmap browser click`/`fill` use to resolve
// their own targets. It reproduces packages/runtime/src/executor.ts's
// runTool algorithm (ensure_view best-effort log, guard-skip, run steps,
// compute returns, attach guidance) without touching the in-page runtime.
type nativeExec struct {
	sm               string
	appDir           string
	corpusDir        string
	defaultTimeoutMs int
}

// toolOutcome is the result of running one tool, pre-JSON. Fields mirror
// packages/runtime/src/executor.ts's ToolResult: value/items are only
// present in the output when the tool actually produced them (nil = omit),
// distinct from a present-but-empty value ("") or list ([]).
type toolOutcome struct {
	ok      bool
	message string
	skipped bool
	value   *string
	items   []map[string]string
}

func (o toolOutcome) toJSON(guidance []gen.Suggestion) ([]byte, error) {
	out := map[string]any{"ok": o.ok}
	if o.value != nil {
		out["value"] = *o.value
	}
	if o.items != nil {
		out["items"] = o.items
	}
	if o.message != "" {
		out["message"] = o.message
	}
	if o.skipped {
		out["skipped"] = true
	}
	if len(guidance) > 0 {
		out["guidance"] = guidance
	}
	return json.MarshalIndent(out, "", "  ")
}

// run executes tool against the live session, mirroring runTool's order:
// ensure_view (best-effort), guard (skip if it holds), steps, returns.
func (e *nativeExec) run(tool *gen.ToolDef, args map[string]any) toolOutcome {
	if tool.EnsureView != "" {
		if err := e.waitForView(tool.EnsureView, 500); err != nil {
			fmt.Fprintf(os.Stderr, "ensure_view: %q expects view %q but the page does not match; proceeding best-effort\n", tool.Name, tool.EnsureView)
		}
	}

	if tool.Guard != nil {
		holds, err := e.guardHolds(tool.Guard, args)
		if err != nil {
			return toolOutcome{ok: false, message: fmt.Sprintf("guard: %v", err)}
		}
		if holds {
			out := toolOutcome{ok: true, skipped: true, message: "guard satisfied; steps skipped (already applied)"}
			if tool.Returns != nil {
				value, items, err := e.computeReturn(tool.Returns, args)
				if err != nil {
					return toolOutcome{ok: false, message: fmt.Sprintf("returns: %v", err)}
				}
				out.value, out.items = value, items
			}
			return out
		}
	}

	for _, step := range tool.Steps {
		op, body, ok := stepOp(step)
		if !ok {
			return toolOutcome{ok: false, message: fmt.Sprintf("tool %q has a malformed step", tool.Name)}
		}
		if err := e.runStep(op, body, args); err != nil {
			return toolOutcome{ok: false, message: fmt.Sprintf("%s: %v", op, err)}
		}
	}

	out := toolOutcome{ok: true}
	if tool.Returns != nil {
		value, items, err := e.computeReturn(tool.Returns, args)
		if err != nil {
			return toolOutcome{ok: false, message: fmt.Sprintf("returns: %v", err)}
		}
		out.value, out.items = value, items
	}
	return out
}

// stepOp returns the single op key + body of a step map, or ("", body, false)
// if the map is not a single-key mapping. Mirrors gen's own unexported
// stepOp (manifest.go) — duplicated here because it's unexported and
// gen.StepBody's fields are the public surface this package is meant to use.
func stepOp(step map[string]gen.StepBody) (string, gen.StepBody, bool) {
	if len(step) != 1 {
		return "", gen.StepBody{}, false
	}
	for k, v := range step {
		return k, v, true
	}
	return "", gen.StepBody{}, false
}

// runStep translates one manifest step into a `sightmap` invocation.
func (e *nativeExec) runStep(op string, body gen.StepBody, args map[string]any) error {
	switch op {
	case "navigate":
		// Single-page slice: navigate never performs a real navigation — it
		// only logs when the current route doesn't already match (see
		// executor.ts's own "navigate" case). It exists to name a same-page
		// destination for guidance, not to act; goto is the real-navigation op.
		fmt.Fprintf(os.Stderr, "navigate: no-op (target view %q)\n", body.View)
		return nil
	case "goto":
		url := interpolateStr(body.URL, args)
		_, err := e.sightmap("browser", "navigate", url)
		return err
	case "fill":
		query := interpolateStr(body.Query, args)
		value := interpolateStr(body.Value, args)
		_, err := e.sightmap("browser", "fill", query, value, "--clear")
		return err
	case "click":
		query := interpolateStr(body.Query, args)
		_, err := e.sightmap("browser", "click", query)
		return err
	case "keypress":
		_, err := e.sightmap("browser", "keypress", body.Key)
		return err
	case "wait_for":
		timeout := body.TimeoutMs
		if timeout == 0 {
			timeout = e.defaultTimeoutMs
		}
		if body.View != "" {
			return e.waitForView(body.View, timeout)
		}
		query := interpolateStr(body.Query, args)
		_, err := e.sightmap("browser", "wait-for", "--component", query, "--timeout-ms", strconv.Itoa(timeout))
		return err
	default:
		return fmt.Errorf("unrecognized step op %q", op)
	}
}

func (e *nativeExec) waitForView(view string, timeoutMs int) error {
	_, err := e.sightmap("browser", "wait-for", "--view", view, "--timeout-ms", strconv.Itoa(timeoutMs))
	return err
}

// guardQuery extracts the query string and present/absent polarity from a
// guard body. Split out from guardHolds so the truth-table decision
// (guardHoldsForCandidates) is testable without a live snapshot.
func guardQuery(g *gen.GuardBody) (query string, present bool, err error) {
	switch {
	case g.Present != nil:
		return g.Present.Query, true, nil
	case g.Absent != nil:
		return g.Absent.Query, false, nil
	default:
		return "", false, fmt.Errorf("guard has neither present nor absent")
	}
}

// guardHoldsForCandidates applies the present/absent truth table to an
// already-resolved candidate set. Mirrors executor.ts's guardHolds: present
// holds when at least one match exists, absent holds when none do.
func guardHoldsForCandidates(g *gen.GuardBody, cands []*sm.ComponentNode) (bool, error) {
	_, present, err := guardQuery(g)
	if err != nil {
		return false, err
	}
	if present {
		return len(cands) > 0, nil
	}
	return len(cands) == 0, nil
}

// guardHolds evaluates a tool's idempotency guard against a fresh snapshot.
func (e *nativeExec) guardHolds(g *gen.GuardBody, args map[string]any) (bool, error) {
	query, _, err := guardQuery(g)
	if err != nil {
		return false, err
	}
	cands, _, err := e.findCandidates(query, args)
	if err != nil {
		return false, err
	}
	return guardHoldsForCandidates(g, cands)
}

// valueFromCandidates reads property off the FIRST candidate, or reports no
// value at all when there are none. Mirrors executor.ts's computeReturn
// value path: `resolveQuery(...)[0]` — the first match, never an ambiguity
// error (unlike compquery.Resolve, which this deliberately does not use).
func valueFromCandidates(cands []*sm.ComponentNode, props map[string]map[string]string, property string) *string {
	if len(cands) == 0 {
		return nil // no match -> value stays absent, not ""
	}
	v := props[cands[0].Id][property]
	return &v
}

// listFromCandidates builds one row per candidate, reading each declared
// field's property. Always non-nil (even with zero candidates), matching the
// runtime's `rows.map(...)`, which yields [] rather than omitting the key.
func listFromCandidates(cands []*sm.ComponentNode, props map[string]map[string]string, fields map[string]gen.FieldDef) []map[string]string {
	items := make([]map[string]string, 0, len(cands))
	for _, c := range cands {
		row := make(map[string]string, len(fields))
		p := props[c.Id]
		for name, fd := range fields {
			row[name] = p[fd.Property]
		}
		items = append(items, row)
	}
	return items
}

// computeReturn resolves a tool's returns declaration (value or list)
// against a fresh snapshot.
func (e *nativeExec) computeReturn(ret *gen.ReturnDef, args map[string]any) (*string, []map[string]string, error) {
	switch {
	case ret.Value != nil:
		cands, props, err := e.findCandidates(ret.Value.Query, args)
		if err != nil {
			return nil, nil, err
		}
		return valueFromCandidates(cands, props, ret.Value.Property), nil, nil
	case ret.List != nil:
		cands, props, err := e.findCandidates(ret.List.Rows, args)
		if err != nil {
			return nil, nil, err
		}
		return nil, listFromCandidates(cands, props, ret.List.Fields), nil
	default:
		return nil, nil, nil // description-only return
	}
}

// snapshotNode is the subset of `sightmap snapshot --json`'s annotated tree
// (go/cmd/sightmap/cmd_snapshot_json.go's annotatedNode) that compquery
// resolution needs: identity, matched component name, extracted properties,
// and children.
type snapshotNode struct {
	ID        string            `json:"id"`
	Component string            `json:"component"`
	Props     map[string]string `json:"props"`
	Children  []*snapshotNode   `json:"children"`
}

type snapshotDoc struct {
	Tree *snapshotNode `json:"tree"`
}

// buildCompqueryInputs converts an annotated snapshot tree into the
// (root, matches, props) triple compquery.FindCandidates consumes,
// reconstructing just enough of a *sightmap.ComponentNode tree to carry
// identity and structure — compquery only reads Id, Children, and the
// caller-supplied matches/props maps.
func buildCompqueryInputs(n *snapshotNode) (*sm.ComponentNode, map[*sm.ComponentNode]*sm.ComponentMatch, map[string]map[string]string) {
	matches := map[*sm.ComponentNode]*sm.ComponentMatch{}
	props := map[string]map[string]string{}
	var convert func(n *snapshotNode) *sm.ComponentNode
	convert = func(n *snapshotNode) *sm.ComponentNode {
		node := &sm.ComponentNode{Id: n.ID}
		if n.Component != "" {
			matches[node] = &sm.ComponentMatch{Name: n.Component}
		}
		if len(n.Props) > 0 {
			props[n.ID] = n.Props
		}
		for _, c := range n.Children {
			node.Children = append(node.Children, convert(c))
		}
		return node
	}
	root := convert(n)
	return root, matches, props
}

// resolveQueryAgainstSnapshot parses an annotated snapshot JSON document and
// resolves query against it, applying compquery's `#N` occurrence index the
// same way the runtime does (dom.ts's resolveQuery): index narrows the
// candidate set to the single element at that position, or to none at all
// when out of range — never an ambiguity error, unlike compquery.Resolve.
func resolveQueryAgainstSnapshot(data []byte, query string, args map[string]any) ([]*sm.ComponentNode, map[string]map[string]string, error) {
	var doc snapshotDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse snapshot json: %w", err)
	}
	if doc.Tree == nil {
		return nil, nil, nil
	}

	root, matches, props := buildCompqueryInputs(doc.Tree)
	q, err := compquery.ParseQuery(interpolateStr(query, args))
	if err != nil {
		return nil, nil, fmt.Errorf("parse query %q: %w", query, err)
	}
	cands := compquery.FindCandidates(root, matches, props, q)
	if q.Index >= 0 {
		if q.Index >= len(cands) {
			return nil, props, nil
		}
		cands = cands[q.Index : q.Index+1]
	}
	return cands, props, nil
}

// findCandidates takes a fresh snapshot of the live page and resolves query
// against it.
func (e *nativeExec) findCandidates(query string, args map[string]any) ([]*sm.ComponentNode, map[string]map[string]string, error) {
	data, err := e.snapshotJSON()
	if err != nil {
		return nil, nil, err
	}
	return resolveQueryAgainstSnapshot(data, query, args)
}

// snapshotJSON takes a fresh `sightmap snapshot --json` of the live page and
// returns its raw bytes.
func (e *nativeExec) snapshotJSON() ([]byte, error) {
	tmp, err := os.CreateTemp("", "sightkick-snapshot-*.json")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if _, err := e.sightmap("snapshot", "--json", tmpPath); err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	return os.ReadFile(tmpPath)
}

// sightmap runs a `sightmap` subcommand from the corpus's app dir, always
// scoped to this executor's corpus dir so the CDP session lookup is
// unambiguous regardless of the manifest's `corpus:` path. stdout is
// returned for callers that need it (snapshot --json writes to a file
// instead, so most callers ignore it); a non-nil error carries sightmap's
// own stderr (or stdout, if stderr was empty) as its message.
func (e *nativeExec) sightmap(args ...string) (string, error) {
	full := append(append([]string{}, args...), "--sightmap-dir", e.corpusDir)
	cmd := osexec.Command(e.sm, full...)
	cmd.Dir = e.appDir
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), fmt.Errorf("%s", msg)
	}
	return out.String(), nil
}

// paramPattern matches a manifest {{param}} reference (same grammar as the
// runtime's packages/runtime/src/dom.ts interpolate and gen's own
// templateParams).
var paramPattern = regexp.MustCompile(`\{\{\s*([\w.]+)\s*\}\}`)

// interpolateStr substitutes {{param}} references in s from args, mirroring
// dom.ts's interpolate exactly: a missing or null value becomes "", anything
// else is rendered with its default string form.
func interpolateStr(s string, args map[string]any) string {
	return paramPattern.ReplaceAllStringFunc(s, func(m string) string {
		key := paramPattern.FindStringSubmatch(m)[1]
		v, ok := args[key]
		if !ok || v == nil {
			return ""
		}
		return fmt.Sprint(v)
	})
}
