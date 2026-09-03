package gen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// exampleDir is the todo example, relative to this test file.
const exampleDir = "../../../examples/todo"
const searchDir = "../../../examples/search"

const goldenPath = "testdata/todo.ir.json"
const searchGoldenPath = "testdata/search.ir.json"

// TestBuildTodoGolden compiles the todo example and compares against the golden
// IR. Regenerate with: UPDATE_GOLDEN=1 go test ./internal/gen/...
func TestBuildTodoGolden(t *testing.T) {
	ir, diags, err := Build(exampleDir)
	if err != nil {
		t.Fatalf("build error: %v", err)
	}
	if HasErrors(diags) {
		t.Fatalf("unexpected diagnostics:\n%s", Format(diags))
	}

	got, err := json.MarshalIndent(ir, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated golden %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with UPDATE_GOLDEN=1 to create): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("IR does not match golden %s.\n--- got ---\n%s", goldenPath, got)
	}
}

// TestBuildTodoShape asserts the load-bearing resolutions independently of the
// golden's exact bytes, so a golden update can't silently mask a regression.
func TestBuildTodoShape(t *testing.T) {
	ir, diags, err := Build(exampleDir)
	if err != nil || HasErrors(diags) {
		t.Fatalf("build failed: err=%v diags=%s", err, Format(diags))
	}
	if ir.Name != "todo" {
		t.Errorf("name = %q, want todo", ir.Name)
	}
	if len(ir.Tools) != 4 {
		t.Fatalf("got %d tools, want 4", len(ir.Tools))
	}

	add := findTool(t, ir, "add_todo")
	if add.EnsureView == nil || add.EnsureView.View != "TodoList" || add.EnsureView.Route != "/" {
		t.Errorf("add_todo ensureView = %+v", add.EnsureView)
	}
	// A compquery addresses the field; its single part carries the lib-flattened
	// compound locator.
	fill := add.Steps[0]
	if fill.Op != "fill" || fill.Query == nil || len(fill.Query.Parts) != 1 ||
		len(fill.Query.Parts[0].Locators) != 1 || fill.Query.Parts[0].Locators[0] != ".todo-app .todo-input .todo-input-field" {
		t.Errorf("add_todo fill step = %+v", fill)
	}
	// The verify step scopes TodoItem by its text property (compiled predicate:
	// TodoItem.text -> descendant-scoped text extractor).
	wf := add.Steps[2]
	if wf.Op != "waitFor" || wf.Query == nil || len(wf.Query.Parts) != 1 || len(wf.Query.Parts[0].Preds) != 1 {
		t.Fatalf("add_todo waitFor step = %+v", wf)
	}
	if p := wf.Query.Parts[0].Preds[0]; p.Extractor.Within != ".todo-item-text" || p.Value != "{{text}}" || p.Op != "=" {
		t.Errorf("add_todo waitFor pred = %+v", wf.Query.Parts[0].Preds[0])
	}

	setFilter := findTool(t, ir, "set_filter")
	if got := setFilter.InputSchema.Properties["filter"].Enum; len(got) != 3 || got[0] != "All" {
		t.Errorf("set_filter enum = %v", got)
	}
}

func TestJourneyGuidance(t *testing.T) {
	ir, diags, err := Build(exampleDir)
	if err != nil || HasErrors(diags) {
		t.Fatalf("build failed: err=%v diags=%s", err, Format(diags))
	}
	add := findTool(t, ir, "add_todo")
	if len(add.Guidance) != 1 {
		t.Fatalf("add_todo guidance = %+v", add.Guidance)
	}
	if add.Guidance[0].Tool != "list_todos" || add.Guidance[0].When != "now" || add.Guidance[0].Reason != "see the todo you just added" {
		t.Errorf("add_todo guidance[0] = %+v", add.Guidance[0])
	}
	// Terminal + unlisted tools carry no guidance.
	if len(findTool(t, ir, "list_todos").Guidance) != 0 {
		t.Errorf("list_todos should have no guidance")
	}
	if len(findTool(t, ir, "clear_completed").Guidance) != 0 {
		t.Errorf("clear_completed should have no guidance")
	}
}

func TestSearchGolden(t *testing.T) {
	ir, diags, err := Build(searchDir)
	if err != nil || HasErrors(diags) {
		t.Fatalf("build failed: err=%v diags=%s", err, Format(diags))
	}
	got, err := json.MarshalIndent(ir, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(searchGoldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(searchGoldenPath)
	if err != nil {
		t.Fatalf("read golden (run with UPDATE_GOLDEN=1 to create): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("search IR does not match golden.\n--- got ---\n%s", got)
	}
}

func TestCrossViewGuidance(t *testing.T) {
	ir, diags, err := Build(searchDir)
	if err != nil || HasErrors(diags) {
		t.Fatalf("build failed: err=%v diags=%s", err, Format(diags))
	}
	// search -> list_results is a cross-view edge => after_navigation.
	search := findTool(t, ir, "search")
	if len(search.Guidance) != 1 {
		t.Fatalf("search guidance = %+v", search.Guidance)
	}
	g := search.Guidance[0]
	if g.Tool != "list_results" || g.When != "after_navigation" || g.View != "Results" {
		t.Errorf("search guidance[0] = %+v", g)
	}
	// list_results -> select_flight is same-view => now.
	list := findTool(t, ir, "list_results")
	if len(list.Guidance) != 1 || list.Guidance[0].Tool != "select_flight" || list.Guidance[0].When != "now" {
		t.Errorf("list_results guidance = %+v", list.Guidance)
	}
	// Rich return: list_results is a read tool (no steps) whose list return maps a
	// machine id + human fields over every row.
	if len(list.Steps) != 0 {
		t.Errorf("list_results should have no steps (read-only), got %d", len(list.Steps))
	}
	if list.Returns == nil || list.Returns.Kind != "list" {
		t.Fatalf("list_results returns = %+v, want kind=list", list.Returns)
	}
	if _, ok := list.Returns.Fields["id"]; !ok {
		t.Errorf("list_results list return should include an id; fields=%v", list.Returns.Fields)
	}
	// set_sort folds the read into the mutation via a list return, not guidance.
	sort := findTool(t, ir, "set_sort")
	if len(sort.Guidance) != 0 {
		t.Errorf("set_sort should have no guidance (fold-in), got %+v", sort.Guidance)
	}
	if sort.Steps[len(sort.Steps)-1].Op != "click" {
		t.Errorf("set_sort should end with the click action")
	}
	if sort.Returns == nil || sort.Returns.Kind != "list" {
		t.Errorf("set_sort should return a list (rich return), got %+v", sort.Returns)
	}
}

func TestToolGuards(t *testing.T) {
	ir, diags, err := Build(searchDir)
	if err != nil || HasErrors(diags) {
		t.Fatalf("build failed: err=%v diags=%s", err, Format(diags))
	}
	// select_flight: idempotency guard keyed on the flight id.
	sel := findTool(t, ir, "select_flight")
	if sel.Guard == nil || sel.Guard.Kind != "present" || sel.Guard.Query == nil ||
		len(sel.Guard.Query.Parts) != 1 || len(sel.Guard.Query.Parts[0].Locators) != 1 ||
		sel.Guard.Query.Parts[0].Locators[0] != ".selection" {
		t.Fatalf("select_flight guard = %+v", sel.Guard)
	}
	if len(sel.Guard.Query.Parts[0].Preds) != 1 || sel.Guard.Query.Parts[0].Preds[0].Value != "{{flight_id}}" {
		t.Errorf("select_flight guard pred = %+v", sel.Guard.Query.Parts[0].Preds)
	}
	// book: guard present the confirmation (no predicate).
	book := findTool(t, ir, "book")
	if book.Guard == nil || book.Guard.Kind != "present" || book.Guard.Query == nil ||
		book.Guard.Query.Parts[0].Locators[0] != ".booking-confirmation" {
		t.Errorf("book guard = %+v", book.Guard)
	}
}

func TestJourneyUnknownToolReported(t *testing.T) {
	dir := t.TempDir()
	corpus := filepath.Join(dir, ".sightmap")
	os.MkdirAll(corpus, 0o755)
	writeFile(t, filepath.Join(corpus, "views.yaml"), `version: 1
views:
  - name: Home
    route: /
    components:
      - name: Field
        selector: ".field"
`)
	writeFile(t, filepath.Join(dir, "webmcp.tools.yaml"), `version: 1
corpus: ./.sightmap
tools:
  - name: do_thing
    mode: live
    ensure_view: Home
    steps:
      - click:
          query: Field
journeys:
  - name: flow
    steps:
      - do_thing
      - nonexistent_tool
`)
	_, diags, _ := Build(dir)
	d := findDiag(diags, "compile.journey-ref")
	if d == nil {
		t.Fatalf("expected compile.journey-ref, got:\n%s", Format(diags))
	}
	if !strings.Contains(d.Message, "nonexistent_tool") || !strings.Contains(d.Message, "do_thing") {
		t.Errorf("diagnostic missing name or candidate: %q", d.Message)
	}
}

func TestUnresolvedComponentReportsCandidates(t *testing.T) {
	dir := t.TempDir()
	corpus := filepath.Join(dir, ".sightmap")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(corpus, "config.yaml"), "name: mini\n")
	writeFile(t, filepath.Join(corpus, "views.yaml"), `version: 1
views:
  - name: Home
    route: /
    components:
      - name: Widget
        selector: ".widget"
`)
	writeFile(t, filepath.Join(dir, "webmcp.tools.yaml"), `version: 1
name: mini
corpus: ./.sightmap
tools:
  - name: bad
    mode: live
    ensure_view: Home
    steps:
      - click:
          query: NoSuchThing
`)

	_, diags, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := findDiag(diags, "compile.query-ref")
	if d == nil {
		t.Fatalf("expected compile.query-ref, got:\n%s", Format(diags))
	}
	if !strings.Contains(d.Message, "NoSuchThing") || !strings.Contains(d.Message, "Widget") {
		t.Errorf("diagnostic missing name or candidate: %q", d.Message)
	}
}

func TestUnknownParamReported(t *testing.T) {
	dir := t.TempDir()
	corpus := filepath.Join(dir, ".sightmap")
	os.MkdirAll(corpus, 0o755)
	writeFile(t, filepath.Join(corpus, "views.yaml"), `version: 1
views:
  - name: Home
    route: /
    components:
      - name: Field
        selector: ".field"
`)
	writeFile(t, filepath.Join(dir, "webmcp.tools.yaml"), `version: 1
corpus: ./.sightmap
tools:
  - name: bad
    mode: live
    ensure_view: Home
    steps:
      - fill:
          query: Field
          value: "{{missing}}"
`)
	_, diags, _ := Build(dir)
	if findDiag(diags, "compile.param") == nil {
		t.Fatalf("expected compile.param, got:\n%s", Format(diags))
	}
}

// TestFieldDefShorthand: a collect field parses from the scalar shorthand
// (`item: itemName`) and the mapping form (`item: {property: itemName}`) to the
// same declared property.
func TestFieldDefShorthand(t *testing.T) {
	var scalar FieldDef
	if err := yaml.Unmarshal([]byte("itemName\n"), &scalar); err != nil {
		t.Fatal(err)
	}
	if scalar.Property != "itemName" {
		t.Errorf("scalar field = %+v, want Property=itemName", scalar)
	}
	var mapping FieldDef
	if err := yaml.Unmarshal([]byte("property: itemName\n"), &mapping); err != nil {
		t.Fatal(err)
	}
	if mapping.Property != "itemName" {
		t.Errorf("mapping field = %+v, want Property=itemName", mapping)
	}
}

// TestReturnHint: the generator bakes a self-describing result shape into the
// tool description so agents don't guess the envelope key/field names.
func TestReturnHint(t *testing.T) {
	ir, diags, err := Build(searchDir)
	if err != nil || HasErrors(diags) {
		t.Fatalf("build failed: err=%v diags=%s", err, Format(diags))
	}
	list := findTool(t, ir, "list_results")
	for _, want := range []string{"`items`", "id", "title", "price"} {
		if !strings.Contains(list.Description, want) {
			t.Errorf("list_results description %q missing %q", list.Description, want)
		}
	}
	book := findTool(t, ir, "book")
	if !strings.Contains(book.Description, "`value`") {
		t.Errorf("book description %q should name the `value` result", book.Description)
	}
}

// TestJourneySelfLoopReported: a journey listing a tool twice in a row is a
// self-edge (yields no guidance) and is reported.
func TestJourneySelfLoopReported(t *testing.T) {
	dir := t.TempDir()
	corpus := filepath.Join(dir, ".sightmap")
	os.MkdirAll(corpus, 0o755)
	writeFile(t, filepath.Join(corpus, "views.yaml"), `version: 1
views:
  - name: Home
    route: /
    components:
      - name: Field
        selector: ".field"
`)
	writeFile(t, filepath.Join(dir, "webmcp.tools.yaml"), `version: 1
corpus: ./.sightmap
tools:
  - name: do_thing
    mode: live
    ensure_view: Home
    steps:
      - click:
          query: Field
journeys:
  - name: flow
    steps:
      - do_thing
      - do_thing
`)
	_, diags, _ := Build(dir)
	if findDiag(diags, "compile.journey-self-loop") == nil {
		t.Fatalf("expected compile.journey-self-loop, got:\n%s", Format(diags))
	}
}

func findTool(t *testing.T, ir IR, name string) Tool {
	t.Helper()
	for _, tool := range ir.Tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return Tool{}
}

func findDiag(diags []Diagnostic, code string) *Diagnostic {
	for i := range diags {
		if diags[i].Code == code {
			return &diags[i]
		}
	}
	return nil
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
