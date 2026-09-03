// Package webmcpinspector embeds the vendored WebMCP inspector extension so
// `sightkick browser --webmcp` can auto-load it into the Chrome-for-Testing
// session without a repo checkout. The inspector
// (github.com/beaufortfrancois/model-context-tool-inspector, Apache-2.0) is an
// in-browser WebMCP client: it reads the tools sightkick registers on
// document.modelContext and lets a human inspect/run them (manually or via
// Gemini) from Chrome's side panel.
//
// The canonical copy lives at <repo>/vendor/webmcp-tool/unpacked/ and is
// refreshed from upstream by scripts/vendor-webmcp-inspector.mjs. Because
// go:embed cannot reach outside the Go module (rooted at <repo>/generator) or
// follow symlinks, the unpacked/ directory below is a committed copy
// regenerated from that canonical source. Do not edit it by hand — run
// `go generate ./webmcpinspector/...` from the generator/ directory.
package webmcpinspector

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

//go:generate go run ./internal/gen

// files holds the embedded unpacked extension (a flat directory of regular
// files). The manifest sits at unpacked/manifest.json.
//
//go:embed unpacked
var files embed.FS

const embedRoot = "unpacked"

// Version returns the vendored inspector's manifest version (best-effort; "" if
// it can't be read).
func Version() string {
	data, err := files.ReadFile(embedRoot + "/manifest.json")
	if err != nil {
		return ""
	}
	var m struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	return m.Version
}

// EnsureExtracted materializes the embedded inspector into a stable per-user
// cache directory and returns that directory — a path suitable for Chrome
// --load-extension / sightmap --extensions. It is idempotent and keyed on a
// content hash of the embedded files, so it re-extracts only when the vendored
// extension changes and is safe to call on every run.
func EnsureExtracted() (string, error) {
	sum, err := digest()
	if err != nil {
		return "", err
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache dir: %w", err)
	}
	dir := filepath.Join(base, "sightkick", "webmcp-inspector", sum[:16])
	done := filepath.Join(dir, ".complete")
	if _, err := os.Stat(done); err == nil {
		return dir, nil // already fully extracted at this content hash
	}

	// Extract into a fresh dir, then drop the completion marker last so a partial
	// extraction (interrupted mid-write) is never mistaken for a good one.
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("reset %s: %w", dir, err)
	}
	if err := extractTo(dir); err != nil {
		return "", err
	}
	if err := os.WriteFile(done, []byte(sum+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("write completion marker: %w", err)
	}
	return dir, nil
}

// extractTo writes every embedded file (minus the embedRoot prefix) under dir.
func extractTo(dir string) error {
	return fs.WalkDir(files, embedRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(embedRoot, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := files.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// digest is a stable content hash over the embedded files (name + bytes, in
// sorted order), used as the cache key so a re-vendored extension extracts to a
// new directory.
func digest() (string, error) {
	var names []string
	err := fs.WalkDir(files, embedRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			names = append(names, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		data, err := files.ReadFile(name)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", name, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
