package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"sightkick/generator/internal/gen"

	"github.com/sightmap/sightmap/go/compquery"
	sm "github.com/sightmap/sightmap/go/sightmap"
)

// ensureViewTimeoutMs bounds the ensure_view precondition check. It's short
// because a mismatch is only logged, never fatal — there is nothing to gain by
// waiting longer for an answer that won't change what happens next.
const ensureViewTimeoutMs = 500

// nativeExec runs one tool from a webmcp.tools.yaml manifest against a live
// Chrome session by shelling out to the `sightmap` CLI: every step becomes a
// `sightmap browser <verb>` command, which acts on the page through real
// browser input events.
//
// The alternative — injecting a JS runtime into the page and having it
// dispatch synthetic events — cannot click elements rendered into a portal
// (dropdown menus, modal dialogs), where a synthetic click silently does
// nothing. Driving the CLI avoids that class of failure entirely.
type nativeExec struct {
	sm               string // path to the sightmap binary
	appDir           string // directory to run sightmap from
	corpusDir        string // absolute path to the .sightmap/ corpus
	defaultTimeoutMs int    // wait_for timeout for steps that declare none
}

// toolOutcome is one tool's result, before serialization.
type toolOutcome struct {
	ok      bool
	message string
	skipped bool
	value   *string             // nil when the tool declares no value to read
	items   []map[string]string // nil when the tool declares no list to read
}

func failf(format string, a ...any) toolOutcome {
	return toolOutcome{ok: false, message: fmt.Sprintf(format, a...)}
}

// toJSON renders the outcome for stdout.
//
// It builds a map rather than marshalling a struct because `items` has three
// distinct states that a struct tag cannot express: absent (this tool reads no
// list), an empty list (it reads a list and found no rows), and a populated
// list. `omitempty` collapses the first two, and omitting the tag renders the
// first as `null`; both would misreport an empty result as something else.
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

// run executes a tool: check that the page is where the tool expects, skip the
// steps if the tool's effect is already applied, otherwise run them, then read
// back whatever the tool declares as its result.
func (e *nativeExec) run(tool *gen.ToolDef, args map[string]any) toolOutcome {
	// A page mismatch is reported but not fatal. Tools are often callable from
	// more than one place, and the steps themselves fail loudly enough if the
	// page really is wrong.
	if tool.EnsureView != "" {
		if err := e.sightmap(waitForViewArgs(tool.EnsureView, ensureViewTimeoutMs)...); err != nil {
			fmt.Fprintf(os.Stderr, "ensure_view: %q expects view %q but the page does not match; proceeding best-effort\n",
				tool.Name, tool.EnsureView)
		}
	}

	skip := false
	if tool.Guard != nil {
		var err error
		if skip, err = e.guardHolds(tool.Guard, args); err != nil {
			return failf("guard: %v", err)
		}
	}

	out := toolOutcome{ok: true, skipped: skip}
	if skip {
		out.message = "guard satisfied; steps skipped (already applied)"
	} else {
		for _, step := range tool.Steps {
			op, body, ok := stepOp(step)
			if !ok {
				return failf("tool %q has a step that is not a single op", tool.Name)
			}
			if err := e.runStep(op, body, args); err != nil {
				return failf("%s: %v", op, err)
			}
		}
	}

	if tool.Returns != nil {
		value, items, err := e.computeReturn(tool.Returns, args)
		if err != nil {
			return failf("returns: %v", err)
		}
		out.value, out.items = value, items
	}
	return out
}

// stepOp unpacks a step, which is a map holding exactly one entry: the op name
// and its body. ok is false for any other shape.
func stepOp(step map[string]gen.StepBody) (op string, body gen.StepBody, ok bool) {
	if len(step) != 1 {
		return "", gen.StepBody{}, false
	}
	for op, body := range step {
		return op, body, true
	}
	return "", gen.StepBody{}, false
}

// waitForViewArgs builds the command that blocks until the page's URL matches
// a named view in the corpus.
func waitForViewArgs(view string, timeoutMs int) []string {
	return []string{"browser", "wait-for", "--view", view, "--timeout-ms", strconv.Itoa(timeoutMs)}
}

// stepArgs translates one step into the `sightmap` arguments that perform it,
// substituting the caller's params into any query, value, or URL first. It
// returns nil args for a step that performs no command.
func stepArgs(op string, body gen.StepBody, args map[string]any, defaultTimeoutMs int) ([]string, error) {
	switch op {
	case "navigate":
		// Names a destination already on this page, so there is nothing to do.
		// `goto` is the op that actually moves the browser.
		return nil, nil

	case "goto":
		return []string{"browser", "navigate", interpolate(body.URL, args)}, nil

	case "fill":
		// --clear replaces the field's contents instead of appending, so
		// running a tool twice sets the same value rather than doubling it.
		return []string{"browser", "fill", interpolate(body.Query, args), interpolate(body.Value, args), "--clear"}, nil

	case "click":
		return []string{"browser", "click", interpolate(body.Query, args)}, nil

	case "keypress":
		// Targets whatever a preceding fill or click left focused; it has no
		// element of its own.
		return []string{"browser", "keypress", body.Key}, nil

	case "wait_for":
		timeout := body.TimeoutMs
		if timeout == 0 {
			timeout = defaultTimeoutMs
		}
		switch {
		case body.View != "" && body.Query != "":
			return nil, errors.New("wait_for names both a view and a query; it takes exactly one")
		case body.View != "":
			return waitForViewArgs(body.View, timeout), nil
		case body.Query != "":
			return []string{"browser", "wait-for", "--component", interpolate(body.Query, args),
				"--timeout-ms", strconv.Itoa(timeout)}, nil
		default:
			return nil, errors.New("wait_for names neither a view nor a query")
		}

	default:
		return nil, fmt.Errorf("unrecognized step op %q", op)
	}
}

func (e *nativeExec) runStep(op string, body gen.StepBody, args map[string]any) error {
	cmdArgs, err := stepArgs(op, body, args, e.defaultTimeoutMs)
	if err != nil {
		return err
	}
	if cmdArgs == nil {
		fmt.Fprintf(os.Stderr, "%s: nothing to do (target view %q is this page)\n", op, body.View)
		return nil
	}
	return e.sightmap(cmdArgs...)
}

// guardQuery reads a guard's query and the match count it expects: a `present`
// guard is satisfied when its query matches something, an `absent` guard when
// it matches nothing.
func guardQuery(g *gen.GuardBody) (query string, wantMatch bool, err error) {
	switch {
	case g.Present != nil:
		return g.Present.Query, true, nil
	case g.Absent != nil:
		return g.Absent.Query, false, nil
	default:
		return "", false, errors.New("guard declares neither present nor absent")
	}
}

// guardHolds reports whether the tool's effect is already applied, meaning its
// steps should be skipped.
func (e *nativeExec) guardHolds(g *gen.GuardBody, args map[string]any) (bool, error) {
	query, wantMatch, err := guardQuery(g)
	if err != nil {
		return false, err
	}
	found, _, err := e.findElements(query, args)
	if err != nil {
		return false, err
	}
	return (len(found) > 0) == wantMatch, nil
}

// returnValues reads a tool's declared result off elements already found on
// the page.
//
// A `value` return reads one property from the first match; several matches is
// not an error, the first one wins. Finding nothing yields no value at all,
// which is different from finding an element whose property is empty. A `list`
// return yields one row per match, and an empty list when there are no matches.
func returnValues(ret *gen.ReturnDef, found []*sm.ComponentNode, props map[string]map[string]string) (*string, []map[string]string) {
	switch {
	case ret.Value != nil:
		if len(found) == 0 {
			return nil, nil
		}
		v := props[found[0].Id][ret.Value.Property]
		return &v, nil

	case ret.List != nil:
		items := make([]map[string]string, 0, len(found))
		for _, node := range found {
			row := make(map[string]string, len(ret.List.Fields))
			for name, field := range ret.List.Fields {
				row[name] = props[node.Id][field.Property]
			}
			items = append(items, row)
		}
		return nil, items

	default:
		return nil, nil // a returns: block with only a description reads nothing
	}
}

func (e *nativeExec) computeReturn(ret *gen.ReturnDef, args map[string]any) (*string, []map[string]string, error) {
	var query string
	switch {
	case ret.Value != nil:
		query = ret.Value.Query
	case ret.List != nil:
		query = ret.List.Rows
	default:
		return nil, nil, nil
	}
	found, props, err := e.findElements(query, args)
	if err != nil {
		return nil, nil, err
	}
	value, items := returnValues(ret, found, props)
	return value, items, nil
}

// snapshotNode is the part of a `sightmap snapshot --json` tree this file
// needs. Each node carries the name of the corpus component it matched (empty
// if it matched none) and that component's extracted property values.
type snapshotNode struct {
	ID        string            `json:"id"`
	Component string            `json:"component"`
	Props     map[string]string `json:"props"`
	Children  []*snapshotNode   `json:"children"`
}

type snapshotDoc struct {
	Tree *snapshotNode `json:"tree"`
}

// findInSnapshot resolves a component query against a snapshot, returning the
// elements it matched and every element's properties keyed by node id.
//
// It reuses the same query engine the sightmap CLI uses to resolve its own
// click and fill targets, so a query that selects an element here selects the
// same one when acted on. A trailing `#N` in the query picks the Nth match;
// out of range yields no match rather than an error.
func findInSnapshot(data []byte, query string, args map[string]any) ([]*sm.ComponentNode, map[string]map[string]string, error) {
	var doc snapshotDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse snapshot json: %w", err)
	}
	if doc.Tree == nil {
		return nil, nil, nil
	}
	root, matches, props := snapshotToTree(doc.Tree)

	q, err := compquery.ParseQuery(interpolate(query, args))
	if err != nil {
		return nil, nil, fmt.Errorf("parse query %q: %w", query, err)
	}
	found := compquery.FindCandidates(root, matches, props, q)
	if q.Index >= 0 {
		if q.Index >= len(found) {
			return nil, props, nil
		}
		found = found[q.Index : q.Index+1]
	}
	return found, props, nil
}

// snapshotToTree rebuilds the snapshot as the three inputs the query engine
// takes: a node tree, the component name each node matched, and each node's
// properties. Only ids and child links are carried over — the engine reads
// nothing else off a node.
func snapshotToTree(n *snapshotNode) (*sm.ComponentNode, map[*sm.ComponentNode]*sm.ComponentMatch, map[string]map[string]string) {
	matches := map[*sm.ComponentNode]*sm.ComponentMatch{}
	props := map[string]map[string]string{}

	var convert func(*snapshotNode) *sm.ComponentNode
	convert = func(n *snapshotNode) *sm.ComponentNode {
		node := &sm.ComponentNode{Id: n.ID}
		if n.Component != "" {
			matches[node] = &sm.ComponentMatch{Name: n.Component}
		}
		if len(n.Props) > 0 {
			props[n.ID] = n.Props
		}
		for _, child := range n.Children {
			node.Children = append(node.Children, convert(child))
		}
		return node
	}
	return convert(n), matches, props
}

// findElements snapshots the live page and resolves query against it.
func (e *nativeExec) findElements(query string, args map[string]any) ([]*sm.ComponentNode, map[string]map[string]string, error) {
	// `sightmap snapshot` writes to a path rather than stdout, so hand it a
	// scratch file and read it straight back.
	f, err := os.CreateTemp("", "sightkick-snapshot-*.json")
	if err != nil {
		return nil, nil, err
	}
	f.Close()
	defer os.Remove(f.Name())

	if err := e.sightmap("snapshot", "--json", f.Name()); err != nil {
		return nil, nil, fmt.Errorf("snapshot: %w", err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		return nil, nil, err
	}
	return findInSnapshot(data, query, args)
}

// sightmap runs one sightmap subcommand. Every call names the corpus dir,
// which selects both the component definitions to resolve queries against and
// the browser session to act on. A failure surfaces sightmap's own stderr,
// where it explains things like a query that matched nothing.
func (e *nativeExec) sightmap(args ...string) error {
	cmd := exec.Command(e.sm, slices.Concat(args, []string{"--sightmap-dir", e.corpusDir})...)
	cmd.Dir = e.appDir
	if _, err := cmd.Output(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if msg := strings.TrimSpace(string(exitErr.Stderr)); msg != "" {
				return errors.New(msg)
			}
		}
		return err
	}
	return nil
}

// paramPattern matches a {{param}} reference in a manifest string.
var paramPattern = regexp.MustCompile(`\{\{\s*([\w.]+)\s*\}\}`)

// interpolate substitutes the caller's params into a manifest string. A param
// that was not supplied, or was supplied as null, becomes an empty string.
func interpolate(s string, args map[string]any) string {
	return paramPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := paramPattern.FindStringSubmatch(match)[1]
		if v := args[name]; v != nil {
			return fmt.Sprint(v)
		}
		return ""
	})
}
