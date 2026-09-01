package gen

import (
	"os"

	"gopkg.in/yaml.v3"
)

// webmcp.tools.yaml — the skill layer. A consumer format versioned independently
// of the sightmap spec. Live DOM flows are the spine; mode:api is opt-in
// reads-only; there is no mode:fetch.

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
// actions (fill/click/wait_for/navigate); reads are declared by `returns`, not
// steps. There is no flat component/where shorthand.
type StepBody struct {
	Query     string `yaml:"query"`
	Value     string `yaml:"value"`
	TimeoutMs int    `yaml:"timeout_ms"`
	View      string `yaml:"view"` // navigate target
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

// Manifest is a parsed webmcp.tools.yaml.
type Manifest struct {
	Version  int          `yaml:"version"`
	Name     string       `yaml:"name"`
	Corpus   string       `yaml:"corpus"`
	Tools    []ToolDef    `yaml:"tools"`
	Journeys []JourneyDef `yaml:"journeys"`
}

// LoadManifest reads + shallow-validates a manifest. Deep validation (component
// refs, params, view names) happens in Compile where the corpus is available.
func LoadManifest(path string) (*Manifest, []Diagnostic, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, []Diagnostic{errf("manifest.parse-error", path, "YAML parse failed: %v", err)}, nil
	}

	var diags []Diagnostic
	if m.Version != 1 {
		diags = append(diags, warnf("manifest.version", path, "expected version 1, got %d", m.Version))
	}
	if m.Corpus == "" {
		diags = append(diags, errf("manifest.corpus", path, "manifest is missing required `corpus` path"))
	}
	if len(m.Tools) == 0 {
		diags = append(diags, errf("manifest.tools", path, "manifest has no `tools`"))
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
	return &m, diags, nil
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
