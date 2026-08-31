/**
 * TypeScript mirror of the sightkick IR contract emitted by the Go generator
 * (generator/internal/gen/ir.go). This is the firewall: the runtime consumes
 * IR only and never sees a sightmap construct.
 *
 * Keep in sync with the Go structs. (A generated JSON Schema shared by both
 * sides is a planned follow-up; until then these are hand-mirrored.)
 */

export interface Extractor {
  kind: "text" | "innerText" | "textOnly" | "attr" | "exists";
  attr?: string;
  within?: string;
}

export interface Where {
  property: string;
  extractor: Extractor;
  /** Value to match; may contain {{param}} placeholders. */
  equals: string;
}

export interface Field {
  property?: string;
  extractor: Extractor;
}

/** One property constraint on a path part (compiled from a compquery predicate). */
export interface Pred {
  property?: string;
  extractor: Extractor;
  op: "=" | "^=" | "*=";
  value: string;
  ci?: boolean;
}

/** One component in a descendant chain: elements matching a locator for which every pred holds. */
export interface PathPart {
  locators: string[];
  preds?: Pred[];
}

export interface Step {
  op: "navigate" | "fill" | "click" | "waitFor" | "collect";
  view?: string;
  route?: string;
  locators?: string[];
  /** A compquery path (descendant chain); when present it replaces locators+where. */
  path?: PathPart[];
  value?: string;
  where?: Where;
  timeoutMs?: number;
  fields?: Record<string, Field>;
}

export interface Return {
  description?: string;
  kind: "value" | "list";
  locators?: string[];
  where?: Where;
  extractor?: Extractor;
}

export interface SchemaProp {
  type: string;
  description?: string;
  enum?: string[];
}

export interface InputSchema {
  type: "object";
  properties: Record<string, SchemaProp>;
  required?: string[];
}

export interface EnsureView {
  view: string;
  route: string;
}

/**
 * A guidance breadcrumb attached to a tool's result: "after this, consider
 * <tool>". Compiled from the journey graph. `when: "now"` means it's callable on
 * the current view; `"after_navigation"` means it becomes available once this
 * tool navigates (see `view`).
 */
export interface Suggestion {
  tool: string;
  reason?: string;
  when: "now" | "after_navigation";
  view?: string;
}

/**
 * A per-tool idempotency guard. The runtime SKIPS the tool's steps when the
 * guard holds: `present` holds when an element matches (effect already applied),
 * `absent` when none do.
 */
export interface Guard {
  kind: "present" | "absent";
  locators: string[];
  where?: Where;
}

export interface Tool {
  name: string;
  description?: string;
  mode: "live" | "api";
  inputSchema: InputSchema;
  ensureView?: EnsureView;
  guard?: Guard;
  steps: Step[];
  returns?: Return;
  guidance?: Suggestion[];
}

export interface ViewRef {
  name: string;
  route: string;
}

export interface IR {
  version: number;
  name: string;
  views: ViewRef[];
  tools: Tool[];
  // Guidance (compiled from journeys) will be added in the guidance-graph slice.
}
