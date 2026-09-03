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
// back whatever the tool declares as its result. ret is nil for a tool that
// declares no result to read.
func (e *nativeExec) run(tool *gen.ToolDef, ret *returnSpec, args map[string]any) toolOutcome {
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

	if ret != nil {
		value, items, err := e.computeReturn(ret, args)
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
	found, err := e.findElements(query, args)
	if err != nil {
		return false, err
	}
	return (len(found) > 0) == wantMatch, nil
}

// valueField is the field name a `value` return's single read is stored under
// while it is being fetched. A list return's fields are named by the author,
// and an empty name is not a legal one, so this cannot collide.
const valueField = ""

// returnSpec is what reading a tool's result takes: a query to find elements
// with, and how to read each named field off one of them.
//
// The two halves come from different places. The query is the manifest's own
// compquery string, resolved against a snapshot. The extractors are compiled
// from the corpus, because reading a property means running the corpus's
// `extract:` directive, which the manifest only names.
type returnSpec struct {
	query  string
	fields map[string]gen.Extractor
	isList bool
}

// computeReturn reads a tool's declared result off the live page: find the
// elements the query names, then read each field off them.
//
// A `value` return reads from the first element found; several matches is not
// an error, the first one wins. Finding nothing yields no value at all, which
// is different from finding an element whose property is empty. A `list`
// return yields one row per element, and an empty list when there are none.
func (e *nativeExec) computeReturn(spec *returnSpec, args map[string]any) (*string, []map[string]string, error) {
	found, err := e.findElements(spec.query, args)
	if err != nil {
		return nil, nil, err
	}

	ids := make([]string, len(found))
	for i, node := range found {
		ids[i] = node.Id
	}
	props, err := e.readProps(ids, spec.fields)
	if err != nil {
		return nil, nil, err
	}
	value, items := assembleReturn(spec, ids, props)
	return value, items, nil
}

// assembleReturn shapes per-element property reads into the tool's result.
func assembleReturn(spec *returnSpec, ids []string, props map[string]map[string]string) (*string, []map[string]string) {
	if spec.isList {
		items := make([]map[string]string, 0, len(ids))
		for _, id := range ids {
			row := props[id]
			if row == nil {
				row = map[string]string{}
			}
			items = append(items, row)
		}
		return nil, items
	}
	if len(ids) == 0 {
		return nil, nil
	}
	v := props[ids[0]][valueField]
	return &v, nil
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
// elements it matched.
//
// It reuses the same query engine the sightmap CLI uses to resolve its own
// click and fill targets, so a query that selects an element here selects the
// same one when acted on. A trailing `#N` in the query picks the Nth match;
// out of range yields no match rather than an error.
func findInSnapshot(data []byte, query string, args map[string]any) ([]*sm.ComponentNode, error) {
	var doc snapshotDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse snapshot json: %w", err)
	}
	if doc.Tree == nil {
		return nil, nil
	}
	root, matches, props := snapshotToTree(doc.Tree)

	q, err := compquery.ParseQuery(interpolate(query, args))
	if err != nil {
		return nil, fmt.Errorf("parse query %q: %w", query, err)
	}
	found := compquery.FindCandidates(root, matches, props, q)
	if q.Index >= 0 {
		if q.Index >= len(found) {
			return nil, nil
		}
		found = found[q.Index : q.Index+1]
	}
	return found, nil
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

// findElements snapshots the live page and resolves query against it. The
// snapshot also tags every element on the page with its node id, which is how
// readProps finds them again afterwards.
func (e *nativeExec) findElements(query string, args map[string]any) ([]*sm.ComponentNode, error) {
	// `sightmap snapshot` writes to a path rather than stdout, so hand it a
	// scratch file and read it straight back.
	f, err := os.CreateTemp("", "sightkick-snapshot-*.json")
	if err != nil {
		return nil, err
	}
	f.Close()
	defer os.Remove(f.Name())

	if err := e.sightmap("snapshot", "--json", f.Name()); err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		return nil, err
	}
	found, err := findInSnapshot(data, query, args)
	return found, err
}

// extractJS reads a set of named properties off a set of elements, addressing
// each element by the node id the last snapshot tagged it with.
//
// The extraction has to happen in the page. sightmap can also report property
// values offline, from the snapshot alone, but its offline extractor
// implements only a subset of the corpus `extract:` grammar — a property
// declared as a bare CSS selector resolves to nothing there, and one declared
// as `text` reads the accessibility tree's computed name, which is empty for a
// plain container whose text sits in a child. Both cases silently return "",
// which is indistinguishable from a genuinely empty value. Reading in the page
// instead reproduces the extractor semantics exactly: prefer an explicit
// accessible name, else the rendered text.
const extractJS = `(function(ids, specs){
  function accessibleText(el){
    var labelledby = el.getAttribute && el.getAttribute("aria-labelledby");
    if (labelledby) {
      var text = labelledby.split(/\s+/)
        .map(function(id){ var r = el.ownerDocument.getElementById(id); return r ? r.textContent.trim() : ""; })
        .filter(Boolean).join(" ");
      if (text) return text;
    }
    var aria = el.getAttribute && el.getAttribute("aria-label");
    if (aria != null && aria.trim() !== "") return aria.trim();
    if (el.labels && el.labels.length) {
      var t = Array.prototype.map.call(el.labels, function(l){ return l.textContent.trim(); })
        .filter(Boolean).join(" ");
      if (t) return t;
    }
    var alt = el.getAttribute && el.getAttribute("alt");
    if (alt != null && alt.trim() !== "") return alt.trim();
    if (typeof el.innerText === "string" && el.innerText.trim() !== "") return el.innerText.trim();
    return (el.textContent || "").trim();
  }
  function extract(el, ex){
    if (ex.kind === "exists") return el.querySelector(ex.within || "*") ? "true" : "false";
    var target = ex.within ? el.querySelector(ex.within) : el;
    if (!target) return "";
    if (ex.kind === "attr") return ex.attr ? (target.getAttribute(ex.attr) || "") : "";
    return accessibleText(target);
  }
  var out = {};
  ids.forEach(function(id){
    var row = {};
    var el = document.querySelector('[data-sightmap-id="' + id + '"]');
    Object.keys(specs).forEach(function(name){ row[name] = el ? extract(el, specs[name]) : ""; });
    out[id] = row;
  });
  return JSON.stringify(out);
})(%s, %s)`

// readProps reads every field in fields off every element in ids, in one round
// trip, and returns them keyed by node id then field name.
func (e *nativeExec) readProps(ids []string, fields map[string]gen.Extractor) (map[string]map[string]string, error) {
	if len(ids) == 0 || len(fields) == 0 {
		return map[string]map[string]string{}, nil
	}
	idsJSON, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	specsJSON, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}

	out, err := e.sightmapOutput("browser", "eval", fmt.Sprintf(extractJS, idsJSON, specsJSON))
	if err != nil {
		return nil, fmt.Errorf("read properties: %w", err)
	}

	// eval prints the expression's value JSON-encoded on the last line. The
	// expression is itself a JSON string, so peel that one layer off first.
	lines := strings.FieldsFunc(out, func(r rune) bool { return r == '\n' })
	if len(lines) == 0 {
		return nil, errors.New("read properties: eval returned nothing")
	}
	var inner string
	if err := json.Unmarshal([]byte(strings.TrimSpace(lines[len(lines)-1])), &inner); err != nil {
		return nil, fmt.Errorf("read properties: unexpected eval output %q: %w", out, err)
	}
	props := map[string]map[string]string{}
	if err := json.Unmarshal([]byte(inner), &props); err != nil {
		return nil, fmt.Errorf("read properties: %w", err)
	}
	return props, nil
}

// sightmap runs one sightmap subcommand. Every call names the corpus dir,
// which selects both the component definitions to resolve queries against and
// the browser session to act on. A failure surfaces sightmap's own stderr,
// where it explains things like a query that matched nothing.
func (e *nativeExec) sightmap(args ...string) error {
	_, err := e.sightmapOutput(args...)
	return err
}

// sightmapOutput is sightmap plus the subcommand's stdout, for the few that
// report their result there rather than acting on the page.
func (e *nativeExec) sightmapOutput(args ...string) (string, error) {
	cmd := exec.Command(e.sm, slices.Concat(args, []string{"--sightmap-dir", e.corpusDir})...)
	cmd.Dir = e.appDir
	stdout, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if msg := strings.TrimSpace(string(exitErr.Stderr)); msg != "" {
				return "", errors.New(msg)
			}
		}
		return "", err
	}
	return string(stdout), nil
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
