package webmcpinspector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestVersion(t *testing.T) {
	if Version() == "" {
		t.Fatal("Version() is empty; embedded manifest.json missing or unreadable")
	}
}

func TestEnsureExtracted(t *testing.T) {
	// Redirect the user cache dir to a temp location so the test doesn't touch
	// the real one. os.UserCacheDir consults XDG_CACHE_HOME (Linux) or
	// $HOME/Library/Caches (macOS); set both to cover either.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))

	dir, err := EnsureExtracted()
	if err != nil {
		t.Fatalf("EnsureExtracted: %v", err)
	}

	// The extracted dir must be a loadable extension: manifest.json at its root,
	// with the version matching Version().
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("read extracted manifest: %v", err)
	}
	var m struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse extracted manifest: %v", err)
	}
	if m.Version != Version() {
		t.Errorf("extracted manifest version %q != Version() %q", m.Version, Version())
	}
	if m.Name == "" {
		t.Error("extracted manifest has no name")
	}

	// The bundled genai payload must be present (it's what makes the inspector
	// non-trivial to vendor); a missing one would mean a broken extraction.
	if _, err := os.Stat(filepath.Join(dir, "js-genai.js")); err != nil {
		t.Errorf("bundled js-genai.js missing from extraction: %v", err)
	}

	// Idempotent: a second call returns the same dir and doesn't error.
	dir2, err := EnsureExtracted()
	if err != nil {
		t.Fatalf("second EnsureExtracted: %v", err)
	}
	if dir2 != dir {
		t.Errorf("EnsureExtracted not stable: %q then %q", dir, dir2)
	}
}
