package gen

import (
	"os"
	"path/filepath"

	sm "github.com/sightmap/sightmap/go/sightmap"
)

// ResolveManifestPath accepts a manifest file or a directory containing
// webmcp.tools.yaml, and returns the manifest file path.
func ResolveManifestPath(target string) string {
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return filepath.Join(target, "webmcp.tools.yaml")
	}
	return target
}

// Build compiles a manifest (its `corpus:` resolved relative to the manifest)
// into the IR, accumulating diagnostics from every stage. A non-nil error is
// only for I/O failures that prevent producing any result.
func Build(target string) (IR, []Diagnostic, error) {
	return build(target, false)
}

// BuildVerified is Build plus the build-time extractor verification pass, which
// checks each tool's returns extractors against the view's captured snapshots
// (see Verify). Opt-in because it requires captures on disk.
func BuildVerified(target string) (IR, []Diagnostic, error) {
	return build(target, true)
}

func build(target string, verify bool) (IR, []Diagnostic, error) {
	manifestPath := ResolveManifestPath(target)

	m, diags, err := LoadManifest(manifestPath)
	if err != nil {
		return IR{}, diags, err
	}

	corpusDir := m.Corpus
	if !filepath.IsAbs(corpusDir) {
		corpusDir = filepath.Join(filepath.Dir(manifestPath), corpusDir)
	}
	if _, err := os.Stat(corpusDir); err != nil {
		diags = append(diags, errf("build.corpus-missing", manifestPath, "corpus directory not found: %s", corpusDir))
		return IR{Version: 1, Name: m.Name, Views: []ViewRef{}, Tools: []Tool{}}, diags, nil
	}

	corpus, err := sm.Load(corpusDir)
	if err != nil {
		diags = append(diags, errf("build.corpus-load", corpusDir, "failed to load corpus: %v", err))
		return IR{Version: 1, Name: m.Name, Views: []ViewRef{}, Tools: []Tool{}}, diags, nil
	}

	ir, cdiags := Compile(m, corpus)
	diags = append(diags, cdiags...)
	if verify {
		diags = append(diags, Verify(m, corpus, corpusDir)...)
	}
	return ir, diags, nil
}
