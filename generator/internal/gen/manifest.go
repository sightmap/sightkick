package gen

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// The tool layer lives in a `.sightkick/` directory (a sibling of the corpus's
// `.sightmap/`). Every *.yaml file inside is merged into one Manifest — tools
// and journeys concatenated — so a large tool layer can be split across files
// with no explicit dependencies (the whole directory is the manifest). It's a
// consumer format versioned independently of the sightmap spec: live DOM flows
// are the spine; mode:api is opt-in reads-only; there is no mode:fetch.

// ParamDef is one tool input parameter.
type ParamDef struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"` // string | number | boolean | enum
	Required    bool     `yaml:"required"`
	Description string   `yaml:"description"`
	Values      []string `yaml:"values"` // for type: enum
}

// FieldDef maps a collect output field to a declared property. Authored either
// as a scalar shorthand (`item: itemName`) or a mapping (`item: {property:
// itemName}`); the mapping form leaves room for future per-field richness.
type FieldDef struct {
	Property string `yaml:"property"`
}

func (f *FieldDef) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		f.Property = value.Value
		return nil
	}
	// Alias to shed the custom unmarshaler and avoid infinite recursion.
	type raw FieldDef
	var r raw
	if err := value.Decode(&r); err != nil {
		return err
	}
	*f = FieldDef(r)
	return nil
}

// StepBody is the (single) value under a step's op key. Each step map has
// exactly one key — the op name — and this struct is its body. Only the fields
// relevant to a given op are populated.
//
// The target is addressed by a compquery (component identity + descendant scope),
// e.g. `OptionGroup[name={{group}}] OptionButton[label={{option}}]`. Steps are
// actions (fill/click/wait_for/navigate/goto/keypress); reads are declared by
// `returns`, not steps. There is no flat component/where shorthand.
//
// keypress dispatches a key event at document.activeElement (whatever a
// preceding fill/click left focused) -- it has no query of its own, matching
// the CLI's own `browser keypress KEY` semantics. It exists for form gates that
// require a real discrete key event a fill's own dispatched per-character
// keydown/keyup can't stand in for -- e.g. a field that only reveals a
// dependent control once Enter is pressed, confirmed live in Fullstory's
// metric-condition form (see fullstory's .sightkick tool layer).
//
// wait_for takes exactly one of query or view: `query` waits for a DOM match
// (the existing form); `view` waits for the current route to match a named
// corpus view instead, for the "the click navigated, but has the destination
// actually rendered yet" gap a route match alone can't close (see Step.View's
// doc in ir.go). A tool that ends by navigating away should almost always end
// in a wait_for, not stop at the click.
type StepBody struct {
	Query     string `yaml:"query"`
	Value     string `yaml:"value"`
	TimeoutMs int    `yaml:"timeout_ms"`
	View      string `yaml:"view"` // navigate target, or a wait_for's route postcondition
	URL       string `yaml:"url"`  // goto target (URL template with {{param}} interpolation)
	Key       string `yaml:"key"`  // keypress target key, e.g. "Enter", "Tab", "Escape"
}

// ValueRef is a returns.value reference: a compquery addressing the element, and
// the declared property to read off it. Yields a single scalar value.
type ValueRef struct {
	Query    string `yaml:"query"`
	Property string `yaml:"property"`
}

// ListRef is a returns.list reference: a compquery whose every match is a row,
// and named output fields (each a declared property of the row component).
// Yields an array of field-shaped objects.
type ListRef struct {
	Rows   string              `yaml:"rows"`
	Fields map[string]FieldDef `yaml:"fields"`
}

// ReturnDef declares a tool's result: exactly one of value (scalar) or list
// (array). A description-only returns yields no value.
type ReturnDef struct {
	Description string    `yaml:"description"`
	Value       *ValueRef `yaml:"value"`
	List        *ListRef  `yaml:"list"`
}

// GuardRef is the compquery a guard checks for presence/absence.
type GuardRef struct {
	Query string `yaml:"query"`
}

// GuardBody is a per-tool idempotency guard: the tool SKIPS its steps when the
// guard holds (the effect is already applied). Exactly one of present/absent.
// `present` skips when the referenced element exists (the common idempotent
// case: "already done"); `absent` skips when it does not.
type GuardBody struct {
	Present *GuardRef `yaml:"present"`
	Absent  *GuardRef `yaml:"absent"`
}

// ToolDef is one tool as authored.
type ToolDef struct {
	Name        string                `yaml:"name"`
	Description string                `yaml:"description"`
	Mode        string                `yaml:"mode"`
	Params      []ParamDef            `yaml:"params"`
	EnsureView  string                `yaml:"ensure_view"`
	Guard       *GuardBody            `yaml:"guard"`
	Steps       []map[string]StepBody `yaml:"steps"`
	Returns     *ReturnDef            `yaml:"returns"`
}

// JourneyStepDef references a tool by name, optionally with a reason surfaced in
// guidance. Authored either as a bare string (`- add_todo`) or a mapping
// (`- {tool: add_todo, reason: "..."}`).
type JourneyStepDef struct {
	Tool   string
	Reason string
}

func (s *JourneyStepDef) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		s.Tool = value.Value
		return nil
	}
	var obj struct {
		Tool   string `yaml:"tool"`
		Reason string `yaml:"reason"`
	}
	if err := value.Decode(&obj); err != nil {
		return err
	}
	s.Tool = obj.Tool
	s.Reason = obj.Reason
	return nil
}

// JourneyDef is a named flow: an ordered path over atomic tools. It is NOT
// executed; it compiles into per-tool guidance breadcrumbs (successor -> "do this
// next"). Tools shared across journeys accumulate the union of their successors.
type JourneyDef struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Steps       []JourneyStepDef `yaml:"steps"`
}

// Manifest is the merged tool layer from a .sightkick/ directory.
type Manifest struct {
	Version  int          `yaml:"version"`
	Name     string       `yaml:"name"`
	Corpus   string       `yaml:"corpus"`
	Tools    []ToolDef    `yaml:"tools"`
	Journeys []JourneyDef `yaml:"journeys"`
}

// DefaultCorpus is the tool layer's corpus when `corpus:` is unspecified: the
// sibling .sightmap/ directory, relative to the .sightkick/ dir.
const DefaultCorpus = "../.sightmap"

// manifestFiles returns the sorted *.yaml/*.yml files under a .sightkick/ dir,
// walked recursively so a tool layer can be organized into subdirectories.
func manifestFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".yaml", ".yml":
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// LoadManifest merges every *.yaml file under a .sightkick/ directory into one
// Manifest and shallow-validates it. Tools and journeys are concatenated across
// files; the singular fields (version/name/corpus) are taken from wherever
// they're set, and a conflict warns. Unset `name` defaults to the app dir's
// name and `corpus` to the sibling .sightmap/. Deep validation (component refs,
// params, view names) happens in Compile where the corpus is available.
func LoadManifest(sightkickDir string) (*Manifest, []Diagnostic, error) {
	files, err := manifestFiles(sightkickDir)
	if err != nil {
		return nil, nil, err
	}
	var diags []Diagnostic
	if len(files) == 0 {
		diags = append(diags, errf("manifest.empty", sightkickDir, ".sightkick/ contains no .yaml files"))
		return &Manifest{Version: 1}, diags, nil
	}

	m := &Manifest{}
	setStr := func(file, field string, cur *string, val string) {
		if val == "" {
			return
		}
		if *cur != "" && *cur != val {
			diags = append(diags, warnf("manifest."+field+"-conflict", file, "%s %q conflicts with %q set earlier; keeping the first", field, val, *cur))
			return
		}
		*cur = val
	}
	for _, f := range files {
		data, rerr := os.ReadFile(f)
		if rerr != nil {
			return nil, diags, rerr
		}
		var part Manifest
		if uerr := yaml.Unmarshal(data, &part); uerr != nil {
			diags = append(diags, errf("manifest.parse-error", f, "YAML parse failed: %v", uerr))
			continue
		}
		if part.Version != 0 {
			if m.Version != 0 && m.Version != part.Version {
				diags = append(diags, warnf("manifest.version-conflict", f, "version %d conflicts with %d set earlier; keeping the first", part.Version, m.Version))
			} else {
				m.Version = part.Version
			}
		}
		setStr(f, "name", &m.Name, part.Name)
		setStr(f, "corpus", &m.Corpus, part.Corpus)
		m.Tools = append(m.Tools, part.Tools...)
		m.Journeys = append(m.Journeys, part.Journeys...)
	}

	// Defaults.
	if m.Version == 0 {
		m.Version = 1
	} else if m.Version != 1 {
		diags = append(diags, warnf("manifest.version", sightkickDir, "expected version 1, got %d", m.Version))
	}
	if m.Name == "" {
		if abs, aerr := filepath.Abs(sightkickDir); aerr == nil {
			m.Name = filepath.Base(filepath.Dir(abs))
		}
	}
	if m.Corpus == "" {
		m.Corpus = DefaultCorpus
	}

	path := sightkickDir
	if len(m.Tools) == 0 {
		diags = append(diags, errf("manifest.tools", path, ".sightkick/ defines no `tools`"))
	}

	seen := map[string]bool{}
	for _, t := range m.Tools {
		if t.Name == "" {
			diags = append(diags, errf("manifest.tool-name", path, "a tool is missing its required `name`"))
			continue
		}
		if seen[t.Name] {
			diags = append(diags, errf("manifest.tool-dup", t.Name, "duplicate tool name %q", t.Name))
		}
		seen[t.Name] = true
		mode := t.Mode
		if mode == "" {
			mode = "live"
		}
		if mode != "live" && mode != "api" {
			diags = append(diags, errf("manifest.tool-mode", t.Name, "tool %q has unknown mode %q (expected live|api)", t.Name, mode))
		}
		if mode == "live" && len(t.Steps) == 0 && t.Returns == nil {
			diags = append(diags, errf("manifest.tool-steps", t.Name, "live tool %q needs at least one step or a returns", t.Name))
		}
	}

	seenJourney := map[string]bool{}
	for _, j := range m.Journeys {
		if j.Name == "" {
			diags = append(diags, errf("manifest.journey-name", path, "a journey is missing its required `name`"))
			continue
		}
		if seenJourney[j.Name] {
			diags = append(diags, errf("manifest.journey-dup", j.Name, "duplicate journey name %q", j.Name))
		}
		seenJourney[j.Name] = true
		if len(j.Steps) < 2 {
			diags = append(diags, warnf("manifest.journey-short", j.Name, "journey %q has fewer than 2 steps; it yields no guidance edges", j.Name))
		}
	}
	return m, diags, nil
}

// stepOp returns the single op key + body of a step map, or ("", body, false)
// if the map is not a single-key mapping.
func stepOp(step map[string]StepBody) (string, StepBody, bool) {
	if len(step) != 1 {
		return "", StepBody{}, false
	}
	for k, v := range step {
		return k, v, true
	}
	return "", StepBody{}, false
}
