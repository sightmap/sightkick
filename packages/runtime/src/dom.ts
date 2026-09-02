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
 * innerText, CSS `text-transform` + visibility. We approximate the accname
 * algorithm's order: aria-labelledby, then aria-label, then a native associated
 * <label> (or alt), then rendered innerText, then textContent as a last resort.
 * The labelledby/label steps matter for form controls (an <input> has no
 * innerText, so its name comes from its label) and match what the lib's offline
 * node.Name resolves. The textContent fallback keeps layout-less DOMs (happy-dom,
 * where innerText is empty) matching authored snapshot values.
 */
function accessibleText(el: Element): string {
  // aria-labelledby: join the referenced elements' text (accname step 2B).
  const labelledby = el.getAttribute?.("aria-labelledby");
  if (labelledby) {
    const doc = el.ownerDocument;
    const text = labelledby
      .split(/\s+/)
      .map((id) => doc?.getElementById(id)?.textContent?.trim() ?? "")
      .filter(Boolean)
      .join(" ");
    if (text) return text;
  }
  const aria = el.getAttribute?.("aria-label");
  if (aria != null && aria.trim() !== "") return aria.trim();
  // Native associated <label>(s) for a labelable control (input/select/textarea…):
  // an input has no innerText, so this is where its accessible name comes from.
  const labels = (el as HTMLInputElement).labels;
  if (labels && labels.length) {
    const text = Array.from(labels)
      .map((l) => l.textContent?.trim() ?? "")
      .filter(Boolean)
      .join(" ");
    if (text) return text;
  }
  const alt = el.getAttribute?.("alt");
  if (alt != null && alt.trim() !== "") return alt.trim();
  const inner = (el as HTMLElement).innerText;
  if (typeof inner === "string" && inner.trim() !== "") return inner.trim();
  return (el.textContent ?? "").trim();
}

/** Pull a value off an element per an IR extractor. Returns a string, or "" / boolean-as-string. */
export function extract(el: Element, ex: Extractor): string {
  // `within` is resolved with a plain querySelector rather than full sightmap
  // component matching. This is a deliberate approximation (the runtime is
  // IR-only — no sightmap matcher in TS): the browser's CSS engine already
  // makes descendant scoping + first-in-document-order faithful. The one
  // bounded gap is cross-component OWNERSHIP / first-match-wins — a generic
  // `within` could hit a descendant the lib would attribute to a different
  // component. It's author-avoidable with a specific child selector, and a
  // faithful mode would mean porting the Go matcher into the runtime (rejected).
  const target = ex.within ? el.querySelector(ex.within) : el;
  if (ex.kind === "exists") {
    return el.querySelector(ex.within ?? "*") ? "true" : "false";
  }
  if (!target) return "";
  switch (ex.kind) {
    case "attr":
      return ex.attr ? (target.getAttribute(ex.attr) ?? "") : "";
    case "text":
    default:
      // Mirror the lib's a11y-name semantics so predicates authored against
      // snapshot/corpus values match at runtime.
      return accessibleText(target);
  }
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

/**
 * Simulate a user click faithfully enough for pointer-driven UI libraries.
 *
 * A bare el.click() dispatches only a `click` event; interaction libraries like
 * React Aria's usePress bind to pointerdown/pointerup and ignore a lone click,
 * so pickers/menus never open. We dispatch the full pointer+mouse sequence
 * (which works from main-world JS even though isTrusted is false — usePress reads
 * the events, not the trust flag) and finish with el.click() for plain onclick
 * handlers. usePress de-dups the trailing click after a press it already handled,
 * so this doesn't double-fire. (Behaviors gated on real user activation — native
 * <select> popups, showPopover(), clipboard, file/opener dialogs — still require
 * a host-driven trusted click; no main-world path can forge that.)
 */
export function clickElement(el: Element): void {
  const t = el as HTMLElement;
  const r = t.getBoundingClientRect?.();
  const clientX = r ? Math.round(r.left + r.width / 2) : 0;
  const clientY = r ? Math.round(r.top + r.height / 2) : 0;
  const init = (buttons: number): MouseEventInit => ({
    bubbles: true,
    cancelable: true,
    composed: true,
    clientX,
    clientY,
    button: 0,
    buttons,
  });
  const hasPE = typeof PointerEvent !== "undefined";
  const emit = (type: string, buttons: number, pointer: boolean) => {
    if (pointer && hasPE) {
      t.dispatchEvent(
        new PointerEvent(type, { ...init(buttons), pointerId: 1, pointerType: "mouse", isPrimary: true }),
      );
    } else {
      t.dispatchEvent(new MouseEvent(type, init(buttons)));
    }
  };
  emit("pointerdown", 1, true);
  emit("mousedown", 1, false);
  emit("pointerup", 0, true);
  emit("mouseup", 0, false);
  t.click();
}

/** Interpolate {{param}} placeholders in a string from an args object. */
export function interpolate(template: string, args: Record<string, unknown>): string {
  return template.replace(/\{\{\s*([\w.]+)\s*\}\}/g, (_, key: string) => {
    const v = args[key];
    return v == null ? "" : String(v);
  });
}
