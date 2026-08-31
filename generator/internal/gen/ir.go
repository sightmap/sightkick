// Package gen compiles a webmcp.tools.yaml manifest against a sightmap corpus
// into sightkick's self-contained IR. Corpus reading, $ref expansion, hierarchy
// flattening, and route matching are delegated to the sightmap reference library
// (github.com/sightmap/sightmap/go/sightmap); this package owns only the tool
// vocabulary and the IR contract.
package gen

// The IR is a self-contained JSON artifact with every corpus reference resolved
// to concrete CSS locators. It is the stable interface the runtime (standalone
// or extension-mediated) and the exporter consume — they never see sightmap
// constructs. Field order and json tags define the wire format; keep them in
// sync with the JSON Schema published alongside.

// Extractor describes how to pull a value off a matched element. It is the
// compiled form of a sightmap property `extract:` string (SEP-0010 grammar).
type Extractor struct {
	// Kind is one of: text, innerText, textOnly, attr, exists.
	Kind string `json:"kind"`
	// Attr is the attribute name when Kind == "attr".
	Attr string `json:"attr,omitempty"`
	// Within scopes the read to a descendant matching this CSS selector
	// (for a bare-selector extract, or for exists:).
	Within string `json:"within,omitempty"`
}

// Where selects, among elements matching a step's locators, the one whose
// declared property equals a (possibly templated) value.
type Where struct {
	Property  string    `json:"property"`
	Extractor Extractor `json:"extractor"`
	Equals    string    `json:"equals"`
}

// Field is one output column of a collect step.
type Field struct {
	Property  string    `json:"property,omitempty"`
	Extractor Extractor `json:"extractor"`
}

// Step is one imperative action in a tool. Op discriminates the shape; unused
// fields are omitted. Ops: navigate, fill, click, waitFor, collect.
type Step struct {
	Op        string           `json:"op"`
	View      string           `json:"view,omitempty"`
	Route     string           `json:"route,omitempty"`
	Locators  []string         `json:"locators,omitempty"`
	Value     string           `json:"value,omitempty"`
	Where     *Where           `json:"where,omitempty"`
	TimeoutMs int              `json:"timeoutMs,omitempty"`
	Fields    map[string]Field `json:"fields,omitempty"`
}

// Return describes a tool's result value.
type Return struct {
	Description string     `json:"description,omitempty"`
	Kind        string     `json:"kind"` // value | list
	Locators    []string   `json:"locators,omitempty"`
	Where       *Where     `json:"where,omitempty"`
	Extractor   *Extractor `json:"extractor,omitempty"`
}

// SchemaProp is one MCP-style JSON Schema property.
type SchemaProp struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// InputSchema is the MCP tool input schema (minimal subset).
type InputSchema struct {
	Type       string                `json:"type"` // always "object"
	Properties map[string]SchemaProp `json:"properties"`
	Required   []string              `json:"required,omitempty"`
}

// EnsureView is a tool's idempotent self-positioning precondition.
type EnsureView struct {
	View  string `json:"view"`
	Route string `json:"route"`
}

// Suggestion is one guidance breadcrumb attached to a tool's result: "after this,
// consider <tool>". Compiled from the journey graph.
type Suggestion struct {
	Tool   string `json:"tool"`
	Reason string `json:"reason,omitempty"`
	// "now" = callable on the current view; "after_navigation" = becomes available
	// once this tool navigates (see View).
	When string `json:"when"`
	View string `json:"view,omitempty"`
}

// Guard is a per-tool idempotency check. The runtime SKIPS the tool's steps when
// the guard holds: kind "present" holds when an element matches (the effect is
// already applied), kind "absent" when none do.
type Guard struct {
	Kind     string   `json:"kind"` // present | absent
	Locators []string `json:"locators"`
	Where    *Where   `json:"where,omitempty"`
}

// Tool is one compiled, callable action.
type Tool struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Mode        string       `json:"mode"` // live | api
	InputSchema InputSchema  `json:"inputSchema"`
	EnsureView  *EnsureView  `json:"ensureView,omitempty"`
	Guard       *Guard       `json:"guard,omitempty"`
	Steps       []Step       `json:"steps"`
	Returns     *Return      `json:"returns,omitempty"`
	Guidance    []Suggestion `json:"guidance,omitempty"`
}

// ViewRef is the minimal corpus projection the runtime needs for positioning.
type ViewRef struct {
	Name  string `json:"name"`
	Route string `json:"route"`
}

// IR is the whole compiled artifact.
type IR struct {
	Version int       `json:"version"`
	Name    string    `json:"name"`
	Views   []ViewRef `json:"views"`
	Tools   []Tool    `json:"tools"`
	// Guidance (compiled from journeys) will be added in the guidance-graph slice.
}
