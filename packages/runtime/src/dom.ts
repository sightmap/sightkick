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
 * Set an input/textarea value via the native setter so a framework's value
 * tracker (React/Preact) sees the change. Dispatches no events on its own.
 */
function setElementValue(el: Element, value: string): void {
  const proto =
    el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  const desc = Object.getOwnPropertyDescriptor(proto, "value");
  if (desc?.set) {
    desc.set.call(el, value);
  } else {
    (el as HTMLInputElement).value = value;
  }
}

/** Construct an InputEvent (with inputType/data) where supported, else a plain input Event. */
function makeInputEvent(inputType: string, data: string | null): Event {
  if (typeof InputEvent !== "undefined") {
    return new InputEvent("input", { bubbles: true, inputType, data: data ?? undefined });
  }
  return new Event("input", { bubbles: true });
}

/**
 * Set an input/textarea value in a way frameworks (React/Preact) notice: use the
 * native value setter, then dispatch a bubbling input + change event. This is the
 * one-shot form; for controlled combobox inputs that ignore/revert a one-shot set
 * (e.g. React Aria), use typeInto, which types character by character.
 */
export function setNativeValue(el: Element, value: string): void {
  setElementValue(el, value);
  el.dispatchEvent(new Event("input", { bubbles: true }));
  el.dispatchEvent(new Event("change", { bubbles: true }));
}

/**
 * Enter text the way a user would: focus the field, then type CHARACTER BY
 * CHARACTER — keydown + native value set + InputEvent(insertText) + keyup, from
 * an empty field.
 *
 * Why per-character rather than a one-shot setNativeValue: controlled combobox
 * inputs (e.g. React Aria's useComboBox) silently REVERT a one-shot programmatic
 * value set and only open/filter their suggestion listbox on genuine, focused,
 * keystroke-shaped input. Typing per character (while focused) is what makes the
 * value stick and the options render for the follow-up option click. For a
 * role=combobox we finish with an ArrowDown — React Aria's canonical open-menu
 * key — so the listbox is reliably present even when it didn't open on input.
 *
 * Everything here is main-world (isTrusted=false) and works because these
 * libraries read the events, not the trust flag. It deliberately does NOT attempt
 * user-activation-gated affordances (a date-picker calendar that opens only on a
 * trusted press, native <select>, showPopover, clipboard) — no page-JS path can
 * forge those; they need a host-driven trusted click.
 */
export function typeInto(el: Element, value: string): void {
  const target = el as HTMLElement;
  target.focus?.();
  // Start from empty so a repeated fill doesn't accumulate and the framework sees
  // a clean edit sequence.
  setElementValue(el, "");
  target.dispatchEvent(makeInputEvent("deleteContentBackward", null));
  let acc = "";
  for (const ch of value) {
    target.dispatchEvent(new KeyboardEvent("keydown", { key: ch, bubbles: true, cancelable: true }));
    acc += ch;
    setElementValue(el, acc);
    target.dispatchEvent(makeInputEvent("insertText", ch));
    target.dispatchEvent(new KeyboardEvent("keyup", { key: ch, bubbles: true }));
  }
  target.dispatchEvent(new Event("change", { bubbles: true }));
  if (el.getAttribute("role") === "combobox") {
    target.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true, cancelable: true }));
    target.dispatchEvent(new KeyboardEvent("keyup", { key: "ArrowDown", bubbles: true }));
  }
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
export async function clickElement(el: Element): Promise<void> {
  const t = el as HTMLElement;
  // Click the element a USER's click would hit. A real click hit-tests at
  // coordinates and lands on the TOPMOST element there, which for a custom
  // element is often an inner child (e.g. <jb-select-option> > div.body) where
  // the handler lives; an event dispatched on the resolved node bubbles UP and
  // never reaches that child, so the click silently no-ops. So bring the node
  // on-screen (coordinate hit-testing only works in the viewport), then dispatch
  // at document.elementFromPoint(center). Coordinate-based, so it also reaches
  // portal-rendered menu items rendered outside the app's own subtree.
  //
  // Settle race: a JUST-opened flyout/menu is still laying out for a few frames —
  // getBoundingClientRect already reports the intended (post-scroll) position, but
  // elementFromPoint returns the PRE-settle paint (a *different* option). An
  // immediate hit-test would target the wrong node and no-op (below-fold dropdown
  // options: State, DOB Month/Day/Year, phone country). So retry across animation
  // frames — recomputing the centre each frame — until elementFromPoint resolves
  // into the target's own subtree; fall back to the node if a short budget expires
  // (an occluded/undecided point, dispatched on the node as before). In-viewport
  // targets resolve on frame 1, so the fast path stays instant.
  if (typeof t.scrollIntoView === "function" && !isInViewport(t)) {
    t.scrollIntoView({ block: "center", inline: "center" });
  }
  const canHitTest = typeof document !== "undefined" && typeof document.elementFromPoint === "function";
  let clientX = 0;
  let clientY = 0;
  let target: HTMLElement = t;
  const deadline = Date.now() + 300;
  for (;;) {
    const r = t.getBoundingClientRect?.();
    clientX = r ? Math.round(r.left + r.width / 2) : 0;
    clientY = r ? Math.round(r.top + r.height / 2) : 0;
    const hit = canHitTest ? document.elementFromPoint(clientX, clientY) : null;
    if (hit && (hit === t || t.contains(hit))) {
      target = hit as HTMLElement;
      break;
    }
    if (!canHitTest || Date.now() >= deadline) {
      target = t;
      break;
    }
    await nextFrame();
  }
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
      target.dispatchEvent(
        new PointerEvent(type, { ...init(buttons), pointerId: 1, pointerType: "mouse", isPrimary: true }),
      );
    } else {
      target.dispatchEvent(new MouseEvent(type, init(buttons)));
    }
  };
  emit("pointerdown", 1, true);
  emit("mousedown", 1, false);
  emit("pointerup", 0, true);
  emit("mouseup", 0, false);
  target.click();
}

/** Resolve on the next animation frame (or a ~16ms timer where rAF is absent). */
function nextFrame(): Promise<void> {
  return new Promise((resolve) => {
    if (typeof requestAnimationFrame === "function") requestAnimationFrame(() => resolve());
    else setTimeout(resolve, 16);
  });
}

/**
 * Is the element's box fully within the current viewport? Used to decide whether
 * clickElement must scroll first so coordinate hit-testing lands on it. Returns
 * true when there's no layout info (a test DOM) so we neither scroll nor mis-skip.
 */
function isInViewport(el: HTMLElement): boolean {
  const r = el.getBoundingClientRect?.();
  if (!r) return true;
  const vh = (typeof window !== "undefined" && window.innerHeight) || 0;
  const vw = (typeof window !== "undefined" && window.innerWidth) || 0;
  if (!vh || !vw) return true;
  return r.top >= 0 && r.left >= 0 && r.bottom <= vh && r.right <= vw;
}

/** Interpolate {{param}} placeholders in a string from an args object. */
export function interpolate(template: string, args: Record<string, unknown>): string {
  return template.replace(/\{\{\s*([\w.]+)\s*\}\}/g, (_, key: string) => {
    const v = args[key];
    return v == null ? "" : String(v);
  });
}
