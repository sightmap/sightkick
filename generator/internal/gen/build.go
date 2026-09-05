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

// loaded is everything one load() pass produces: the manifest and corpus
// alongside the compiled IR, so a second consumer (BuildOutline) can read the
// manifest's authored fields — param order, journey names/descriptions — that
// compiling into IR discards, without a second parse/compile pass. compiled is
// false on the two corpus-failure paths below, mirroring build's early return
// of a named-empty IR with no Compile call.
type loaded struct {
	manifest  *Manifest
	corpus    *sm.Corpus
	corpusDir string
	ir        IR
	compiled  bool
}

// load resolves and compiles target (an app dir or .sightkick dir) once,
// carrying both the raw manifest/corpus and the compiled IR forward so
// Build and BuildOutline can share a single parse/compile pass instead of
// each reading the manifest and corpus on their own.
func load(target string) (*loaded, []Diagnostic, error) {
	skDir, err := ResolveSightkickDir(target)
	if err != nil {
		return nil, nil, err
	}

	m, diags, err := LoadManifest(skDir)
	if err != nil {
		return nil, diags, err
	}

	corpusDir := corpusDirFor(skDir, m.Corpus)
	if _, err := os.Stat(corpusDir); err != nil {
		diags = append(diags, errf("build.corpus-missing", skDir, "corpus directory not found: %s", corpusDir))
		emptyIR := IR{Version: 1, Name: m.Name, Views: []ViewRef{}, Tools: []Tool{}}
		return &loaded{manifest: m, corpusDir: corpusDir, ir: emptyIR}, diags, nil
	}

	corpus, err := sm.Load(corpusDir)
	if err != nil {
		diags = append(diags, errf("build.corpus-load", corpusDir, "failed to load corpus: %v", err))
		emptyIR := IR{Version: 1, Name: m.Name, Views: []ViewRef{}, Tools: []Tool{}}
		return &loaded{manifest: m, corpusDir: corpusDir, ir: emptyIR}, diags, nil
	}

	ir, cdiags := Compile(m, corpus)
	diags = append(diags, cdiags...)
	return &loaded{manifest: m, corpus: corpus, corpusDir: corpusDir, ir: ir, compiled: true}, diags, nil
}

func build(target string, verify bool) (IR, []Diagnostic, error) {
	l, diags, err := load(target)
	if err != nil {
		return IR{}, diags, err
	}
	if !l.compiled {
		return l.ir, diags, nil
	}
	if verify {
		diags = append(diags, Verify(l.manifest, l.corpus, l.corpusDir)...)
	}
	return l.ir, diags, nil
}
