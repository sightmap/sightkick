package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"sightkick/generator/internal/gen"
	"sightkick/generator/runtimebundle"
)

// stringList is a repeatable string flag (e.g. --chrome-flag=A --chrome-flag=B).
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// runBrowser is the one-command debug/drive setup: it compiles the corpus to IR,
// emits the embedded runtime bundle, starts a sightmap browser session with the
// right flags, and persist-injects runtime+IR so the tools register on the page
// (and survive navigations). It shells out to the sightmap CLI, which owns the
// browser session; sightkick only supplies the artifacts + orchestration.
func runBrowser(args []string) error {
	fset := flag.NewFlagSet("browser", flag.ContinueOnError)
	fset.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: sightkick browser <corpus-dir> [flags]")
		fset.PrintDefaults()
	}
	urlFlag := fset.String("url", "", "URL to open (default: the corpus home view's URL)")
	profile := fset.String("profile", "", "Chrome user data dir (passed through to sightmap)")
	cdpPort := fset.Int("cdp-port", 0, "Chrome remote debugging port (passed through to sightmap)")
	headless := fset.Bool("headless", false, "Run headless (passed through to sightmap)")
	webmcp := fset.Bool("webmcp", false, "Expose the native document.modelContext (adds the WebMCP blink flags)")
	extensions := fset.String("extensions", "", "Comma-separated unpacked extension paths (passed through to sightmap; e.g. a WebMCP inspector). NOTE: sightmap replaces its auto-loaded overlay when this is set — include it explicitly if you need it.")
	noStart := fset.Bool("no-start", false, "Skip 'sightmap browser start'; inject into an already-running session")
	var chromeFlags stringList
	fset.Var(&chromeFlags, "chrome-flag", "Extra Chrome flag, e.g. --chrome-flag=--no-sandbox (repeatable; passed through to sightmap)")
	// The corpus dir is the first arg; flags follow it (Go's flag package stops
	// at the first positional, so we split them explicitly rather than require
	// flags-before-dir).
	if len(args) >= 1 && (args[0] == "-h" || args[0] == "--help") {
		fset.Usage()
		return nil
	}
	if len(args) < 1 {
		fset.Usage()
		return fmt.Errorf("missing <corpus-dir>")
	}
	target := args[0]
	if strings.HasPrefix(target, "-") {
		fset.Usage()
		return fmt.Errorf("first argument must be the corpus dir, got flag %q", target)
	}
	if err := fset.Parse(args[1:]); err == flag.ErrHelp {
		return nil
	} else if err != nil {
		return err
	}

	// The sightmap CLI owns the browser session; it's a hard prerequisite.
	sm, err := exec.LookPath("sightmap")
	if err != nil {
		return fmt.Errorf("the 'sightmap' CLI is required but not on PATH — install it: npm i -g @sightmap/sightmap")
	}

	// Run every sightmap subcommand from the app dir so its .sightmap/.session
	// lands next to the corpus — the session is then discoverable by any
	// `sightmap browser` command run from that dir, and start won't go sessionless
	// for lack of a .sightmap/ (the corpus is right there).
	appDir := target
	if info, statErr := os.Stat(target); statErr == nil && !info.IsDir() {
		appDir = filepath.Dir(target)
	}

	// 1) Compile the corpus + manifest to IR.
	ir, diags, err := gen.Build(target)
	if err != nil {
		return err
	}
	if len(diags) > 0 {
		fmt.Fprintln(os.Stderr, gen.Format(diags))
	}
	if gen.HasErrors(diags) {
		return fmt.Errorf("build failed: %d error(s)", gen.CountErrors(diags))
	}
	irJSON, err := json.Marshal(ir)
	if err != nil {
		return err
	}

	// 2) Compose the combined runtime+IR script (the bundle auto-boots and sets
	//    window.__sightkick; then we load the IR to register its view-scoped
	//    tools). Written to a stable temp path so a re-inject overwrites it and
	//    it works whether sightmap persists the content or the path.
	var b strings.Builder
	b.Write(runtimebundle.JS)
	b.WriteString("\n;try{window.__sightkick.load(")
	b.Write(irJSON)
	b.WriteString(");}catch(e){console.warn('[sightkick] IR load failed',e);}\n")
	scriptPath := filepath.Join(os.TempDir(), "sightkick-inject.js")
	if err := os.WriteFile(scriptPath, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write inject script: %w", err)
	}

	// 3) Start the session (unless injecting into an existing one).
	if !*noStart {
		startURL := *urlFlag
		if startURL == "" {
			u, uerr := gen.StartURL(target)
			if uerr != nil || u == "" {
				return fmt.Errorf("no --url given and the corpus declares no home-view URL; pass --url")
			}
			startURL = u
		}
		startArgs := []string{"browser", "start", "--detach", "--url", startURL}
		if *profile != "" {
			startArgs = append(startArgs, "--profile", *profile)
		}
		if *cdpPort != 0 {
			startArgs = append(startArgs, "--cdp-port", strconv.Itoa(*cdpPort))
		}
		if *headless {
			startArgs = append(startArgs, "--headless")
		}
		if *extensions != "" {
			startArgs = append(startArgs, "--extensions", *extensions)
		}
		if *webmcp {
			startArgs = append(startArgs,
				"--chrome-flag=--enable-blink-features=ModelContext,ModelContextTesting",
				"--chrome-flag=--enable-features=DevToolsWebMCPSupport")
		}
		for _, f := range chromeFlags {
			startArgs = append(startArgs, "--chrome-flag="+f)
		}
		fmt.Fprintf(os.Stderr, "→ starting sightmap browser at %s …\n", startURL)
		if err := runSightmap(sm, appDir, startArgs); err != nil {
			return fmt.Errorf("sightmap browser start: %w", err)
		}
		waitForTab(sm, appDir, startURL) // best-effort: the content tab opens a beat after --detach returns
	}

	// 4) Persist-inject runtime+IR so the tools register now and re-register on
	//    every new document (surviving full navigations).
	fmt.Fprintf(os.Stderr, "→ injecting runtime + IR (%d tool(s), persisted) …\n", len(ir.Tools))
	if err := runSightmap(sm, appDir, []string{"browser", "inject", "--file", scriptPath, "--persist"}); err != nil {
		return fmt.Errorf("sightmap browser inject: %w", err)
	}

	// The session lives in appDir/.sightmap, so drive commands must run there.
	cd := ""
	if appDir != "." && appDir != "" {
		cd = "cd " + appDir + " && "
	}
	fmt.Fprintf(os.Stderr, "\n✓ sightkick tools are live on the page. Drive them from the app dir:\n")
	fmt.Fprintf(os.Stderr, "    %ssightmap browser eval \"window.__sightkick.call('<tool>', { ... })\"\n", cd)
	fmt.Fprintf(os.Stderr, "  list registered tools:\n")
	fmt.Fprintf(os.Stderr, "    %ssightmap browser eval \"document.modelContext.getTools().then(t=>console.log(t.map(x=>x.name)))\"\n", cd)
	fmt.Fprintf(os.Stderr, "  stop the session:  %ssightmap browser stop\n", cd)
	return nil
}

// runSightmap runs a sightmap subcommand from dir (so .sightmap/.session
// resolves against the corpus), routing its human output to stderr so
// sightkick's own stdout stays clean.
func runSightmap(sm, dir string, args []string) error {
	cmd := exec.Command(sm, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// waitForTab polls `sightmap browser status` until it lists the started URL's
// host (the content tab opens a beat after --detach returns). Best-effort: it
// returns after a short timeout regardless, since inject reports its own error
// if no session is up.
func waitForTab(sm, dir, startURL string) {
	host := ""
	if u, err := url.Parse(startURL); err == nil {
		host = u.Host
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		status := exec.Command(sm, "browser", "status")
		status.Dir = dir
		out, _ := status.CombinedOutput()
		s := string(out)
		if strings.Contains(s, "running") && (host == "" || strings.Contains(s, host)) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}
