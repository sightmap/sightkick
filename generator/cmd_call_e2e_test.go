//go:build e2e

// End-to-end coverage for `sightkick call`: it builds the real binary and
// drives the search example through a real browser session, asserting what
// each tool returns. Nothing here is stubbed — the assertions only hold if a
// click actually reached the page and changed it.
//
// Behind the `e2e` build tag because it needs a browser and a served app,
// neither of which CI has. `go vet -tags e2e ./...` still compiles it, so it
// cannot rot unnoticed.
//
// Prerequisites:
//   - the sightmap CLI on PATH:  npm i -g @sightmap/sightmap && sightmap browser install
//   - the demo server on :5174:  pnpm --filter @sightkick/runtime demo
//
// Run:  go test -tags e2e -v -run TestCallE2E .
package main

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"sightkick/generator/internal/gen"
)

const (
	e2eDemoURL    = "http://localhost:5174/"
	e2eResultsURL = "http://localhost:5174/results"
	e2eAppDir     = "../examples/search"
)

// callResult is the JSON `sightkick call` prints. Pointer and slice fields
// distinguish "the tool read nothing" from "it read an empty value".
type callResult struct {
	OK       bool                `json:"ok"`
	Value    *string             `json:"value"`
	Items    []map[string]string `json:"items"`
	Message  string              `json:"message"`
	Skipped  bool                `json:"skipped"`
	Guidance []gen.Suggestion    `json:"guidance"`
}

// itemIDs pulls the `id` field out of each returned row, which is what the
// list assertions compare — the demo's sort order is the observable effect.
func (r callResult) itemIDs() []string {
	var ids []string
	for _, row := range r.Items {
		ids = append(ids, row["id"])
	}
	return ids
}

// TestCallE2E walks the search example's booking journey through both
// execution paths, asserting the same results from each. The steps share page
// state and must run in order: each one's precondition is the previous one's
// effect.
//
// It starts on the results view rather than searching its way there, because
// the tool that navigates cannot report a result through the WebMCP path — see
// TestCallE2ENavigation.
//
// The demo page boots the sightkick runtime itself, so --via webmcp has
// registered tools to call without anything being injected here.
func TestCallE2E(t *testing.T) {
	for _, via := range []string{"webmcp", "cli"} {
		t.Run("via "+via, func(t *testing.T) { testCallJourney(t, via) })
	}
}

func testCallJourney(t *testing.T, via string) {
	env := newE2E(t, e2eResultsURL, ".result")

	tests := []struct {
		name   string
		tool   string
		params []string
		// waitFor is a CSS selector to settle on before the next step, for a
		// tool whose last act navigates without waiting for the destination.
		waitFor string
		check   func(t *testing.T, got callResult)
	}{
		{
			name: "list_results returns one row per result, price-ascending",
			tool: "list_results",
			check: func(t *testing.T, got callResult) {
				if ids := got.itemIDs(); !slices.Equal(ids, []string{"f2", "f1", "f3"}) {
					t.Errorf("result ids = %v, want [f2 f1 f3] (price ascending)", ids)
				}
				if len(got.Items) > 0 && got.Items[0]["price"] != "$180" {
					t.Errorf("first row price = %q, want $180", got.Items[0]["price"])
				}
			},
		},
		{
			name: "set_sort reorders the page and returns the new order in one call",
			tool: "set_sort",
			check: func(t *testing.T, got callResult) {
				if ids := got.itemIDs(); !slices.Equal(ids, []string{"f3", "f1", "f2"}) {
					t.Errorf("result ids = %v, want [f3 f1 f2] (price descending)", ids)
				}
			},
		},
		{
			name: "select_flight interpolates the id into its click target and reads the selection back",
			tool: "select_flight", params: []string{"flight_id=f1"},
			check: func(t *testing.T, got callResult) {
				if got.Skipped {
					t.Error("first select was skipped; its guard should not hold yet")
				}
				wantValueContains(t, got, "Alpha Air")
			},
		},
		{
			name: "selecting the same flight again skips its steps instead of re-clicking",
			tool: "select_flight", params: []string{"flight_id=f1"},
			check: func(t *testing.T, got callResult) {
				if !got.Skipped {
					t.Error("second select ran its steps; its guard should hold now")
				}
				wantValueContains(t, got, "Alpha Air")
			},
		},
		{
			name: "book clicks through and reads the reference the page rendered",
			tool: "book",
			check: func(t *testing.T, got callResult) {
				if got.Skipped {
					t.Error("first book was skipped; its guard should not hold yet")
				}
				wantValue(t, got, "BK-F1")
			},
		},
		{
			name: "booking again skips its steps and reports the same reference",
			tool: "book",
			check: func(t *testing.T, got callResult) {
				if !got.Skipped {
					t.Error("second book ran its steps; its guard should hold now")
				}
				wantValue(t, got, "BK-F1")
			},
		},
	}

	for _, tc := range tests {
		if !t.Run(tc.name, func(t *testing.T) {
			got := env.call(t, via, tc.tool, tc.params...)
			if !got.OK {
				t.Fatalf("tool %q failed: %s", tc.tool, got.Message)
			}
			tc.check(t, got)
			if tc.waitFor != "" {
				env.sightmap(t, "browser", "wait-for", "--selector", tc.waitFor, "--timeout-ms", "10000")
			}
		}) {
			// Later steps depend on this one's effect, so a failure here would
			// only produce noise downstream.
			t.Fatal("aborting: the remaining steps build on the one that just failed")
		}
	}
}

// TestCallE2ENavigation covers a tool whose last act navigates, where the two
// execution paths genuinely differ.
//
// Through the CLI the tool reports its result normally. Through WebMCP it acts
// on the page but reports a failure: the click changes the route, the runtime
// re-registers the tool set for the new view, and that aborts the registration
// of the tool still running — which Chrome's built-in modelContext surfaces as
// a failed call. The runtime's polyfill does not tie a result to the
// registration this way, so this only shows up against the built-in surface.
//
// That is a runtime limitation, not a reporting quirk: an agent driving over
// WebMCP cannot tell this apart from a tool that really failed. This test pins
// the current behaviour so that fixing it is visible here.
func TestCallE2ENavigation(t *testing.T) {
	t.Run("via cli the navigating tool reports its result", func(t *testing.T) {
		env := newE2E(t, e2eDemoURL, "#go")
		got := env.call(t, "cli", "search", "query=ATL to LHR")
		if !got.OK {
			t.Fatalf("search failed: %s", got.Message)
		}
		wantGuidance(t, got, gen.Suggestion{
			Tool:   "list_results",
			Reason: "read the results the search produced",
			When:   "after_navigation",
			View:   "Results",
		})
		env.sightmap(t, "browser", "wait-for", "--selector", ".result", "--timeout-ms", "10000")
	})

	t.Run("via webmcp the navigating tool acts but cannot report", func(t *testing.T) {
		env := newE2E(t, e2eDemoURL, "#go")
		got, err := env.callRaw(t, "webmcp", "search", "query=ATL to LHR")
		if err == nil || got.OK {
			t.Fatal("search reported success; if the runtime no longer aborts a navigating " +
				"tool's registration, fold this tool back into TestCallE2E and delete this case")
		}
		// The click still landed: the page really did move to the results view.
		env.sightmap(t, "browser", "wait-for", "--selector", ".result", "--timeout-ms", "10000")
		if path := env.evalString(t, "location.pathname"); path != "/results" {
			t.Errorf("path = %q, want /results — the click should have navigated even though the call reported failure", path)
		}
	})
}

// TestCallE2EErrors covers what `call` reports when a tool cannot do its job:
// the failure has to name the step and carry sightmap's own explanation, and
// the process has to exit non-zero so a script notices.
func TestCallE2EErrors(t *testing.T) {
	env := newE2E(t, e2eDemoURL, "#go")

	t.Run("a tool whose target is not on the page fails with the step and the reason", func(t *testing.T) {
		// The demo opens on the search view, where no result rows exist, so
		// select_flight's click has nothing to resolve against.
		got, err := env.callRaw(t, "cli", "select_flight", "flight_id=f1")
		if err == nil {
			t.Error("call exited 0; a failed tool must exit non-zero")
		}
		if got.OK {
			t.Fatal("result says ok, want a failure")
		}
		if !strings.Contains(got.Message, "click:") {
			t.Errorf("message = %q, want it to name the failing step", got.Message)
		}
		if !strings.Contains(got.Message, "no component matches") {
			t.Errorf("message = %q, want sightmap's own explanation", got.Message)
		}
	})

	t.Run("an unknown tool name fails before touching the browser", func(t *testing.T) {
		cmd := exec.Command(env.bin, "call", e2eAppDir, "no_such_tool")
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Error("call exited 0 for an unknown tool")
		}
		if !strings.Contains(string(out), `tool "no_such_tool" not found`) {
			t.Errorf("output = %q, want it to name the missing tool", out)
		}
	})
}

// ---- harness ----------------------------------------------------------------

type e2eEnv struct {
	bin       string // the sightkick binary under test
	corpusDir string // absolute; keys both the corpus and the browser session
}

// newE2E builds the binary and starts a browser session on the demo app,
// stopping it when the test ends. It fails rather than skips when a
// prerequisite is missing: this test only runs when explicitly asked for, so a
// silent skip would just look like a pass.
func newE2E(t *testing.T, startURL, settleSelector string) *e2eEnv {
	t.Helper()

	if _, err := exec.LookPath("sightmap"); err != nil {
		t.Fatal("the sightmap CLI is not on PATH — install it:\n" +
			"  npm i -g @sightmap/sightmap && sightmap browser install")
	}
	if !demoServerUp() {
		t.Fatalf("no demo server at %s — start it:\n"+
			"  pnpm --filter @sightkick/runtime demo", e2eDemoURL)
	}

	corpusDir, err := filepath.Abs(filepath.Join(e2eAppDir, ".sightmap"))
	if err != nil {
		t.Fatalf("resolve corpus dir: %v", err)
	}
	env := &e2eEnv{bin: filepath.Join(t.TempDir(), "sightkick"), corpusDir: corpusDir}

	build := exec.Command("go", "build", "-o", env.bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build sightkick: %v\n%s", err, out)
	}

	// A throwaway Chrome profile, so a previous run's cookies or storage can't
	// change what this one sees.
	profile := filepath.Join(t.TempDir(), "profile")
	env.trySightmap("browser", "stop") // clear any session left on this corpus
	env.sightmap(t, "browser", "start", "--detach", "--url", startURL, "--profile", profile)
	t.Cleanup(func() { env.trySightmap("browser", "stop") })

	// --detach returns once the daemon is serving, but the content tab opens a
	// beat later. Until it does, every page command fails with "no content tab
	// open", so wait for `status` to list the tab before issuing any.
	env.waitForTab(t, startURL)
	env.sightmap(t, "browser", "wait-for", "--selector", settleSelector, "--timeout-ms", "15000")
	return env
}

// waitForTab polls until the session reports a tab on the demo server.
func (e *e2eEnv) waitForTab(t *testing.T, startURL string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		out, err := e.sightmapCmd("browser", "status").CombinedOutput()
		if err == nil && strings.Contains(string(out), startURL) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("no content tab opened at %s within 30s", startURL)
}

// call runs one tool and requires the command itself to be well-formed. A tool
// that reports ok:false is still returned, for the caller to assert on.
func (e *e2eEnv) call(t *testing.T, via, tool string, params ...string) callResult {
	t.Helper()
	got, _ := e.callRaw(t, via, tool, params...)
	return got
}

// callRaw runs one tool and also returns whether the process exited non-zero,
// which is how `call` signals a failed tool to a script.
func (e *e2eEnv) callRaw(t *testing.T, via, tool string, params ...string) (callResult, error) {
	t.Helper()
	args := []string{"call", e2eAppDir, tool, "--via", via}
	for _, p := range params {
		args = append(args, "--param", p)
	}
	cmd := exec.Command(e.bin, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, runErr := cmd.Output()

	var got callResult
	if err := json.Unmarshal(stdout, &got); err != nil {
		t.Fatalf("call %s: cannot parse result %q: %v\nstderr: %s", tool, stdout, err, stderr.String())
	}
	return got, runErr
}

// sightmap runs a sightmap subcommand against this test's session and fails
// the test if it errors.
func (e *e2eEnv) sightmap(t *testing.T, args ...string) {
	t.Helper()
	if out, err := e.sightmapCmd(args...).CombinedOutput(); err != nil {
		t.Fatalf("sightmap %v: %v\n%s", args, err, out)
	}
}

// trySightmap runs a sightmap subcommand whose failure is not interesting,
// like stopping a session that isn't running.
func (e *e2eEnv) trySightmap(args ...string) {
	_ = e.sightmapCmd(args...).Run()
}

// evalString reads a JS expression's string value out of the page, undoing the
// one layer of JSON encoding `browser eval` prints it with.
func (e *e2eEnv) evalString(t *testing.T, expr string) string {
	t.Helper()
	out, err := e.sightmapCmd("browser", "eval", expr).Output()
	if err != nil {
		t.Fatalf("eval %q: %v", expr, err)
	}
	lines := strings.FieldsFunc(string(out), func(r rune) bool { return r == '\n' })
	if len(lines) == 0 {
		t.Fatalf("eval %q returned nothing", expr)
	}
	var value string
	if err := json.Unmarshal([]byte(strings.TrimSpace(lines[len(lines)-1])), &value); err != nil {
		t.Fatalf("eval %q: unexpected output %q: %v", expr, out, err)
	}
	return value
}

func (e *e2eEnv) sightmapCmd(args ...string) *exec.Cmd {
	cmd := exec.Command("sightmap", append(args, "--sightmap-dir", e.corpusDir)...)
	cmd.Dir = e2eAppDir
	return cmd
}

func demoServerUp() bool {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(e2eDemoURL)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ---- assertions -------------------------------------------------------------

func wantValue(t *testing.T, got callResult, want string) {
	t.Helper()
	if got.Value == nil {
		t.Fatalf("no value returned, want %q", want)
	}
	if *got.Value != want {
		t.Errorf("value = %q, want %q", *got.Value, want)
	}
}

func wantValueContains(t *testing.T, got callResult, want string) {
	t.Helper()
	if got.Value == nil {
		t.Fatalf("no value returned, want one containing %q", want)
	}
	if !strings.Contains(*got.Value, want) {
		t.Errorf("value = %q, want it to contain %q", *got.Value, want)
	}
}

func wantGuidance(t *testing.T, got callResult, want ...gen.Suggestion) {
	t.Helper()
	if !slices.Equal(got.Guidance, want) {
		t.Errorf("guidance = %+v, want %+v", got.Guidance, want)
	}
}
