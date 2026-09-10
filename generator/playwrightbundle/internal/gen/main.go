// Command gen syncs the compiled Playwright executor
// (<repo>/packages/playwright/dist/executor.mjs) into this Go package
// (<repo>/generator/playwrightbundle/) so it can be embedded via go:embed.
//
// go:embed cannot reach outside the Go module (the module root is
// <repo>/generator) or follow symlinks, so the built bundle — which lives under
// the gitignored packages/playwright/dist/ — cannot be embedded in place. This
// generator copies it in as a committed file, the same "generate and commit"
// pattern used for the embedded skills. Because the bundle is a build output
// (not a committed source), build it first, then regenerate:
//
//	pnpm --filter @sightkick/playwright build      # -> packages/playwright/dist/executor.mjs
//	go generate ./playwrightbundle/...             # copies it here
//
// CI rebuilds the bundle and reruns this in the (node-capable) runtime job, then
// fails on any drift. Do not edit the generated copy by hand.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const bundleName = "executor.mjs"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gen-playwright: %v\n", err)
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
	genDir := filepath.Dir(thisFile)              // .../generator/playwrightbundle/internal/gen
	pkgDir := filepath.Join(genDir, "..", "..")   // .../generator/playwrightbundle
	repoRoot := filepath.Join(pkgDir, "..", "..") // .../ (playwrightbundle -> generator -> repo)
	src := filepath.Join(repoRoot, "packages", "playwright", "dist", bundleName)
	dst := filepath.Join(pkgDir, bundleName)

	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf(
				"Playwright bundle not built: %s\n"+
					"       build it first: pnpm --filter @sightkick/playwright build", src)
		}
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	fmt.Printf("gen-playwright: synced %s (%d bytes) into %s\n", bundleName, len(data), pkgDir)
	return nil
}
