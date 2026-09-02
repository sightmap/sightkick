package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"sightkick/generator/skills"
)

func runSkills(args []string) error {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: sightkick skills install [--target DIR]\n")
		return nil
	}
	switch args[0] {
	case "install":
		return runSkillsInstall(args[1:])
	default:
		return fmt.Errorf("skills: unknown subcommand %q", args[0])
	}
}

// runSkillsInstall extracts the embedded skills into a target directory (default
// ~/.agents/skills), one subdirectory per skill. Mirrors `sightmap skills
// install` so an agent installs sightkick's authoring/debug skills the same way.
func runSkillsInstall(args []string) error {
	fset := flag.NewFlagSet("skills install", flag.ContinueOnError)
	home, _ := os.UserHomeDir()
	defaultTarget := filepath.Join(home, ".agents", "skills")
	targetFlag := fset.String("target", defaultTarget, "Directory to install skills into (each skill becomes a subdirectory)")
	if err := fset.Parse(args); err != nil {
		return err
	}
	baseTarget := *targetFlag

	entries, err := fs.ReadDir(skills.FS, ".")
	if err != nil {
		return fmt.Errorf("skills install: read embedded skills: %w", err)
	}

	var installed []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillName := e.Name()
		target := filepath.Join(baseTarget, skillName)

		// Remove any prior install, then re-extract.
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("skills install: remove old %s: %w", skillName, err)
		}

		if err := fs.WalkDir(skills.FS, skillName, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, relErr := filepath.Rel(skillName, path)
			if relErr != nil {
				return relErr
			}
			dest := filepath.Join(target, rel)
			if d.IsDir() {
				return os.MkdirAll(dest, 0o755)
			}
			data, readErr := skills.FS.ReadFile(path)
			if readErr != nil {
				return fmt.Errorf("read embedded %s: %w", path, readErr)
			}
			if mkErr := os.MkdirAll(filepath.Dir(dest), 0o755); mkErr != nil {
				return mkErr
			}
			return os.WriteFile(dest, data, 0o644)
		}); err != nil {
			return fmt.Errorf("skills install: extract %s: %w", skillName, err)
		}
		installed = append(installed, skillName)
	}

	fmt.Printf("installed %d sightkick skill(s) → %s\n", len(installed), baseTarget)
	for _, s := range installed {
		fmt.Printf("  %s\n", s)
	}
	fmt.Printf("  version: %s\n", Version)

	// sightkick's skills build on the sightmap skills (sightmap-browser for driving
	// a live session, sightmap-authoring for the corpus), so install those too —
	// best-effort, so a hiccup here never fails sightkick's own install.
	fmt.Fprintln(os.Stderr, "\ninstalling the supporting sightmap skills (npx @sightmap/sightmap)…")
	if err := installSightmapSkills(baseTarget); err != nil {
		fmt.Fprintf(os.Stderr,
			"note: couldn't install the sightmap skills automatically (%v)\n"+
				"      run it yourself: npx @sightmap/sightmap skills install --target %s\n",
			err, baseTarget)
	}
	return nil
}

// installSightmapSkills runs the sightmap CLI's own installer via npx, dropping
// the sightmap-browser + sightmap-authoring skills next to sightkick's. Using
// npx (rather than a PATH lookup) means it works even when the sightmap CLI
// isn't installed globally — npx fetches it on demand; --yes skips its prompt.
func installSightmapSkills(target string) error {
	cmd := exec.Command("npx", "--yes", "@sightmap/sightmap", "skills", "install", "--target", target)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
