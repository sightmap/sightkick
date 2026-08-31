import type { Extractor, PathPart, Pred, Query } from "./ir.js";

/**
 * Shadow-piercing querySelectorAll: matches within the document and recurses
 * into every open shadow root. Returns a flat, document-order-ish list.
 *
 * (Closed shadow roots are unreachable by design — same limit a user's own
 * clicks have. That's the sightkick bet: only touch what the UI exposes.)
 */
export function deepQueryAll(selector: string, root: ParentNode = document): Element[] {
  const out: Element[] = [];
  const seen = new Set<Element>();
  const collect = (r: ParentNode) => {
    let matches: Element[] = [];
    try {
      matches = Array.from(r.querySelectorAll(selector));
    } catch {
      // Invalid selector — skip rather than throw.
      return;
    }
    for (const el of matches) {
      if (!seen.has(el)) {
        seen.add(el);
        out.push(el);
      }
    }
    // Recurse into shadow roots of all elements under r.
    const all = r.querySelectorAll("*");
    for (const el of all) {
      const sr = (el as Element & { shadowRoot?: ShadowRoot | null }).shadowRoot;
      if (sr) collect(sr);
    }
  };
  collect(root);
  return out;
}

/**
 * Approximate an element's accessible name — the value the sightmap lib's `text`
 * extractor returns (match/component_props.go returns node.Name, the a11y name,
 * NOT raw textContent). The accessible name reflects `aria-label`/`alt` and, via
 * innerText, CSS `text-transform` + visibility. We approximate rather than run
 * the full accname algorithm: aria-label/alt win, then rendered innerText, then
 * textContent as a last resort. The textContent fallback is what keeps layout-less
 * DOMs (happy-dom, where innerText is empty) matching authored snapshot values.
 */
function accessibleText(el: Element): string {
  const aria = el.getAttribute?.("aria-label");
  if (aria != null && aria.trim() !== "") return aria.trim();
  const alt = el.getAttribute?.("alt");
  if (alt != null && alt.trim() !== "") return alt.trim();
  const inner = (el as HTMLElement).innerText;
  if (typeof inner === "string" && inner.trim() !== "") return inner.trim();
  return (el.textContent ?? "").trim();
}

/** Pull a value off an element per an IR extractor. Returns a string, or "" / boolean-as-string. */
export function extract(el: Element, ex: Extractor): string {
  const target = ex.within ? el.querySelector(ex.within) : el;
  if (ex.kind === "exists") {
    return el.querySelector(ex.within ?? "*") ? "true" : "false";
  }
  if (!target) return "";
  switch (ex.kind) {
    case "attr":
      return ex.attr ? (target.getAttribute(ex.attr) ?? "") : "";
    case "innerText":
      return ((target as HTMLElement).innerText ?? target.textContent ?? "").trim();
    case "textOnly":
      return directText(target).trim();
    case "text":
    default:
      // Mirror the lib's a11y-name semantics so predicates authored against
      // snapshot/corpus values match at runtime.
      return accessibleText(target);
  }
}

/** Text from an element's direct text-node children only (excludes descendants). */
function directText(el: Element): string {
  let s = "";
  for (const node of Array.from(el.childNodes)) {
    if (node.nodeType === 3 /* TEXT_NODE */) s += node.textContent ?? "";
  }
  return s;
}

/** Does a single path-part predicate hold for an element? (op/ci per compquery.) */
function matchPred(el: Element, pred: Pred, args: Record<string, unknown>): boolean {
  let a = extract(el, pred.extractor);
  let b = interpolate(pred.value, args);
  if (pred.ci) {
    a = a.toLowerCase();
    b = b.toLowerCase();
  }
  switch (pred.op) {
    case "^=":
      return a.startsWith(b);
    case "*=":
      return a.includes(b);
    default:
      return a === b;
  }
}

/**
 * Resolve a compquery path against the live DOM by containment scoping: each
 * part's matches are found *within* the previous part's matches (the last part is
 * the target). This is the runtime half of the sightmap-free path the generator
 * compiles — it addresses "the OptionButton labelled X within the OptionGroup
 * named Y" without any matcher, using plain DOM descendant containment.
 */
export function resolvePath(path: PathPart[], args: Record<string, unknown>): Element[] {
  let scopes: ParentNode[] = [document];
  let matched: Element[] = [];
  for (const part of path) {
    const found: Element[] = [];
    const seen = new Set<Element>();
    for (const root of scopes) {
      for (const loc of part.locators) {
        for (const el of deepQueryAll(loc, root)) {
          if (seen.has(el)) continue;
          if ((part.preds ?? []).every((p) => matchPred(el, p, args))) {
            seen.add(el);
            found.push(el);
          }
        }
      }
    }
    matched = found;
    scopes = found;
  }
  return matched;
}

/**
 * Resolve a full compquery: the descendant chain (parts) plus an optional 0-based
 * occurrence index. With no index the whole candidate set is returned (the caller
 * decides: single-target ops take [0], collect consumes all). With an index only
 * that occurrence is returned (a single-element result), mirroring compquery `#N`.
 */
export function resolveQuery(query: Query, args: Record<string, unknown>): Element[] {
  const all = resolvePath(query.parts, args);
  if (query.index == null) return all;
  const el = all[query.index];
  return el ? [el] : [];
}

/**
 * Set an input/textarea value in a way frameworks (React/Preact) notice: use the
 * native value setter, then dispatch a bubbling input + change event.
 */
export function setNativeValue(el: Element, value: string): void {
  const proto =
    el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  const desc = Object.getOwnPropertyDescriptor(proto, "value");
  if (desc?.set) {
    desc.set.call(el, value);
  } else {
    (el as HTMLInputElement).value = value;
  }
  el.dispatchEvent(new Event("input", { bubbles: true }));
  el.dispatchEvent(new Event("change", { bubbles: true }));
}

/** Interpolate {{param}} placeholders in a string from an args object. */
export function interpolate(template: string, args: Record<string, unknown>): string {
  return template.replace(/\{\{\s*([\w.]+)\s*\}\}/g, (_, key: string) => {
    const v = args[key];
    return v == null ? "" : String(v);
  });
}
