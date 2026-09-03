// Command gen syncs the vendored WebMCP inspector extension
// (<repo>/vendor/webmcp-tool/unpacked/) into this Go package
// (<repo>/generator/webmcpinspector/unpacked/) so it can be embedded via
// go:embed.
//
// go:embed cannot reach outside the Go module (the module root is
// <repo>/generator) or follow symlinks, so the canonical vendored copy at the
// repository root cannot be embedded in place. This generator copies it in as a
// committed copy — the same "generate and commit" pattern used for the embedded
// skills and runtime bundle. CI reruns this and fails on any drift.
//
// The canonical copy is refreshed from upstream by
// scripts/vendor-webmcp-inspector.mjs; do not edit either copy by hand. Run via
// `go generate ./webmcpinspector/...` from the generator/ module directory.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gen-webmcpinspector: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Derive paths from this source file's location (not the working directory),
	// so the generator is correct regardless of where `go generate` runs.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("cannot resolve source file location")
	}
	genDir := filepath.Dir(thisFile)              // .../generator/webmcpinspector/internal/gen
	pkgDir := filepath.Join(genDir, "..", "..")   // .../generator/webmcpinspector
	repoRoot := filepath.Join(pkgDir, "..", "..") // .../ (webmcpinspector -> generator -> repo)
	src := filepath.Join(repoRoot, "vendor", "webmcp-tool", "unpacked")
	dst := filepath.Join(pkgDir, "unpacked")

	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf(
			"vendored inspector not found: %s\n"+
				"       vendor it first: npm run vendor-inspector", src)
	}

	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("clean %s: %w", dst, err)
	}
	n, err := copyTree(src, dst)
	if err != nil {
		return err
	}
	fmt.Printf("gen-webmcpinspector: synced %d file(s) into %s\n", n, pkgDir)
	return nil
}

// copyTree copies the flat file tree rooted at src into dst, returning the file
// count. Regular files only — the unpacked extension is a flat directory of
// regular files (which is all go:embed would accept anyway).
func copyTree(src, dst string) (int, error) {
	n := 0
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("refusing to copy irregular file %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		n++
		return os.WriteFile(target, data, 0o644)
	})
	return n, err
}
