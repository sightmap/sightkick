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

// Pred is one property constraint on a path part: extract a declared property and
// compare it to a (possibly templated) value. Op is "=", "^=", or "*="; CI makes
// the comparison case-insensitive. Compiled from a compquery predicate.
type Pred struct {
	Property  string    `json:"property,omitempty"`
	Extractor Extractor `json:"extractor"`
	Op        string    `json:"op"`
	Value     string    `json:"value"`
	CI        bool      `json:"ci,omitempty"`
}

// PathPart matches a single component in a descendant chain: elements matching
// any of Locators for which every Pred holds.
type PathPart struct {
	Locators []string `json:"locators"`
	Preds    []Pred   `json:"preds,omitempty"`
}

// A Query is a compiled compquery: an ordered descendant chain (Parts) whose LAST
// part is the target, plus an optional 0-based occurrence Index (compquery `#N`)
// selecting among the matches. The runtime resolves it by DOM containment (each
// part scoped within the previous part's matches), so it addresses "the
// OptionButton labelled X within the OptionGroup named Y" without any sightmap
// constructs. It is the single DOM-addressing form: steps, returns, and guards
// all carry a Query (there is deliberately no flat locators/where shorthand).
type Query struct {
	Parts []PathPart `json:"parts"`
	// Index is the 0-based occurrence to select; nil selects the whole match set
	// (single-target ops take the first, collect consumes all).
	Index *int `json:"index,omitempty"`
}

// Field is one output column of a list return (one extracted property per row).
type Field struct {
	Property  string    `json:"property,omitempty"`
	Extractor Extractor `json:"extractor"`
}

// Step is one imperative action in a tool. Op discriminates the shape; unused
// fields are omitted. Ops: navigate, fill, click, waitFor. Every DOM-addressing
// op (fill/click/waitFor) carries a Query; navigate does not. Reads are not
// steps — a tool's result is declared by Returns.
type Step struct {
	Op        string `json:"op"`
	View      string `json:"view,omitempty"`
	Route     string `json:"route,omitempty"`
	Query     *Query `json:"query,omitempty"`
	Value     string `json:"value,omitempty"`
	TimeoutMs int    `json:"timeoutMs,omitempty"`
}

// Return describes a tool's result, computed after the steps run. Kind "value"
// reads one property (Query -> first match, Extractor); kind "list" maps Query
// over every match, emitting one Fields-shaped object per row. This is the
// single result mechanism (there is no collect step).
type Return struct {
	Description string           `json:"description,omitempty"`
	Kind        string           `json:"kind"` // value | list
	Query       *Query           `json:"query,omitempty"`
	Extractor   *Extractor       `json:"extractor,omitempty"`
	Fields      map[string]Field `json:"fields,omitempty"`
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
	Kind  string `json:"kind"` // present | absent
	Query *Query `json:"query"`
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
