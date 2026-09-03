package gen

import (
	"fmt"
	"os"
	"path/filepath"

	sm "github.com/sightmap/sightmap/go/sightmap"
)

// ResolveSightkickDir accepts an app directory (containing a .sightkick/ tool
// layer) or a .sightkick directory directly, and returns the .sightkick
// directory path that build/compile consume.
func ResolveSightkickDir(target string) (string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("expected an app dir or a .sightkick dir, got a file: %s", target)
	}
	if filepath.Base(target) == ".sightkick" {
		return target, nil
	}
	sk := filepath.Join(target, ".sightkick")
	if fi, serr := os.Stat(sk); serr == nil && fi.IsDir() {
		return sk, nil
	}
	return "", fmt.Errorf("no .sightkick/ tool layer found in %s", target)
}

// corpusDirFor resolves a manifest's corpus path (relative to the .sightkick
// dir) to a concrete directory.
func corpusDirFor(sightkickDir, corpus string) string {
	if filepath.IsAbs(corpus) {
		return corpus
	}
	return filepath.Join(sightkickDir, corpus)
}

// Build compiles a manifest (its `corpus:` resolved relative to the manifest)
// into the IR, accumulating diagnostics from every stage. A non-nil error is
// only for I/O failures that prevent producing any result.
func Build(target string) (IR, []Diagnostic, error) {
	return build(target, false)
}

// StartURL returns a representative URL to open for the corpus behind target (an
// app dir or .sightkick dir): the home view's URL (route "/"), else the first
// view that declares a URL. Empty string if the corpus declares no view URL.
// Used by `sightkick browser` to auto-derive where to point the session.
func StartURL(target string) (string, error) {
	skDir, err := ResolveSightkickDir(target)
	if err != nil {
		return "", err
	}
	m, _, err := LoadManifest(skDir)
	if err != nil {
		return "", err
	}
	corpus, err := sm.Load(corpusDirFor(skDir, m.Corpus))
	if err != nil {
		return "", err
	}
	var fallback string
	for i := range corpus.Views {
		v := corpus.Views[i]
		if v.URL == "" {
			continue
		}
		if v.Route == "/" {
			return v.URL, nil
		}
		if fallback == "" {
			fallback = v.URL
		}
	}
	return fallback, nil
}

// BuildVerified is Build plus the build-time extractor verification pass, which
// checks each tool's returns extractors against the view's captured snapshots
// (see Verify). Opt-in because it requires captures on disk.
func BuildVerified(target string) (IR, []Diagnostic, error) {
	return build(target, true)
}

func build(target string, verify bool) (IR, []Diagnostic, error) {
	skDir, err := ResolveSightkickDir(target)
	if err != nil {
		return IR{}, nil, err
	}

	m, diags, err := LoadManifest(skDir)
	if err != nil {
		return IR{}, diags, err
	}

	corpusDir := corpusDirFor(skDir, m.Corpus)
	if _, err := os.Stat(corpusDir); err != nil {
		diags = append(diags, errf("build.corpus-missing", skDir, "corpus directory not found: %s", corpusDir))
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
