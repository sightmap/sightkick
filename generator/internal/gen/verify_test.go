package gen

import (
	"os"
	"path/filepath"
	"testing"

	sm "github.com/sightmap/sightmap/go/sightmap"
)

// rowCorpus is a one-view corpus whose Row carries a `code` property extracted
// as `text` — the role-less-node case that only resolves via the offline text
// fallback (sightmap a3c7).
func rowCorpus() *sm.Corpus {
	return &sm.Corpus{Views: []sm.ViewDef{{
		Name: "V", Route: "/", URL: "https://x/",
		Components: []sm.ComponentDef{{
			Name:      "Row",
			Selectors: []string{".row"},
			Properties: []sm.ComponentPropertyDef{
				{Name: "code", Extract: "text"},
			},
		}},
	}}}
}

func writeTree(t *testing.T, dir, view, name, json string) {
	t.Helper()
	snapDir := filepath.Join(dir, "snapshots", view)
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapDir, name+".snap.tree.json"), []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestVerifyFieldEmpty exercises the build-time verifier end to end: a captured
// row whose text is present passes; one whose text is absent trips
// verify.field-empty. It also proves the pass path rides on the offline text
// fallback (the Row node is role-less, so `code` only resolves from Text).
func TestVerifyFieldEmpty(t *testing.T) {
	corpus := rowCorpus()
	m := listOverRow("Row", "code")
	slug := corpus.Views[0].SnapBasename() // "v"

	// Capture WITH rendered text on the role-less Row node -> code resolves.
	withText := t.TempDir()
	writeTree(t, withText, slug, "a",
		`{"id":"1","role":"none","text":"B6 123","isVisible":true,"element":{"tag":"div","classes":["row"],"attrs":{"class":"row"}}}`)
	if d := Verify(m, corpus, withText); hasDiag(d, "verify.field-empty") || hasDiag(d, "verify.no-rows") {
		t.Errorf("text present: expected clean verify, got:\n%s", Format(d))
	}

	// Capture WITHOUT text -> code resolves empty on the only row -> warns.
	noText := t.TempDir()
	writeTree(t, noText, slug, "a",
		`{"id":"1","role":"none","isVisible":true,"element":{"tag":"div","classes":["row"],"attrs":{"class":"row"}}}`)
	if d := Verify(m, corpus, noText); !hasDiag(d, "verify.field-empty") {
		t.Errorf("text absent: expected verify.field-empty, got:\n%s", Format(d))
	}
}

// TestVerifyNoRows: when the row component matches nothing in the capture, the
// verifier reports no-rows (can't check fields) rather than field-empty.
func TestVerifyNoRows(t *testing.T) {
	corpus := rowCorpus()
	m := listOverRow("Row", "code")
	slug := corpus.Views[0].SnapBasename()

	dir := t.TempDir()
	writeTree(t, dir, slug, "a",
		`{"id":"1","role":"none","isVisible":true,"element":{"tag":"div","classes":["other"],"attrs":{"class":"other"}}}`)
	d := Verify(m, corpus, dir)
	if !hasDiag(d, "verify.no-rows") {
		t.Errorf("expected verify.no-rows, got:\n%s", Format(d))
	}
	if hasDiag(d, "verify.field-empty") {
		t.Errorf("no-rows should preempt field-empty, got:\n%s", Format(d))
	}
}

// TestVerifyNoCapture: a view with no captured snapshot yields no-capture.
func TestVerifyNoCapture(t *testing.T) {
	corpus := rowCorpus()
	m := listOverRow("Row", "code")
	d := Verify(m, corpus, t.TempDir()) // empty dir, no snapshots
	if !hasDiag(d, "verify.no-capture") {
		t.Errorf("expected verify.no-capture, got:\n%s", Format(d))
	}
}
