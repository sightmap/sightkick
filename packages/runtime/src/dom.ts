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

/**
 * The element's OWN literal text: the concatenation of its direct text-node
 * children, whitespace-normalized. This is `extract: raw_text` (SEP-0013) — NOT
 * innerText (layout-dependent) and NOT a subtree textContent (which would pull in
 * descendant element text and <style>/<script> bleed); CSS pseudo content
 * (::before/::after) is excluded because pseudo-elements are not child nodes. It
 * mirrors the sightmap lib's offline node.RawText (probe own-text -> normalizeText),
 * so a raw_text predicate authored against the corpus matches at runtime.
 */
function ownText(el: Element): string {
  let s = "";
  const kids = el.childNodes;
  for (let i = 0; i < kids.length; i++) {
    const n = kids[i];
    if (n && n.nodeType === 3 /* TEXT_NODE */) s += (n as Text).data;
  }
  // Match the offline pipeline: probe truncates to 100 chars, then normalizeText
  // collapses whitespace runs to a single space and trims the ends.
  return s.slice(0, 100).replace(/\s+/g, " ").trim();
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
    case "raw_text":
      // The node's own literal text — the deterministic escape when the
      // accessible name welds in CSS/aria text (SEP-0013).
      return ownText(target);
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
 * Dispatch the full press sequence — pointerdown/mousedown/pointerup/mouseup then
 * a real click — on `target`, at `clientX`/`clientY`. This is the shared effector
 * for both click paths; it works from main-world JS (isTrusted is false, but
 * interaction libraries read the events, not the trust flag), and the trailing
 * .click() covers plain onclick handlers while usePress de-dups it after a press
 * it already handled, so it doesn't double-fire.
 */
function dispatchPointerClick(target: HTMLElement, clientX: number, clientY: number): void {
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

/**
 * The deepest descendant of `root` whose box contains (x, y), by each node's OWN
 * getBoundingClientRect. This is the offscreen-safe replacement for
 * document.elementFromPoint: elementFromPoint returns null for a point outside
 * the viewport (the sites-02d5 far-scroll failure), but an element's own rect is
 * valid wherever it sits. Dispatching on this leaf lets the event bubble UP
 * through every inner node, so a handler bound to an inner child fires even
 * though we resolved the outer element — e.g. a custom option whose click handler
 * lives on `<jb-select-option> > div.body`, confirmed live on jetblue: a click on
 * the outer <jb-select-option> does nothing, one on its inner div.body commits.
 */
function deepestElementAt(root: HTMLElement, x: number, y: number): HTMLElement {
  let node = root;
  for (;;) {
    let next: HTMLElement | null = null;
    const kids = node.children;
    // Last match wins: later siblings paint on top, so prefer the topmost.
    for (let i = 0; i < kids.length; i++) {
      const child = kids[i] as HTMLElement;
      const r = child.getBoundingClientRect?.();
      if (r && x >= r.left && x <= r.right && y >= r.top && y <= r.bottom) next = child;
    }
    if (!next) return node;
    node = next;
  }
}

/**
 * Simulate a user click faithfully enough for pointer-driven UI libraries
 * (React Aria's usePress binds to pointerdown/pointerup and ignores a lone
 * click). Two problems it must handle at once, both seen on jetblue's selects:
 * a handler on an INNER child (dispatching on the outer resolved node bubbles
 * past it and no-ops), and an OFFSCREEN target (a far-scrolled option).
 *
 * So we bring the node on-screen (some libraries hit-test their own pointerup and
 * read an off-screen release as a cancelled press), then descend the element's
 * OWN subtree to the deepest node at its centre and dispatch there — the event
 * bubbles up to whichever inner node holds the handler. We do NOT use
 * document.elementFromPoint (offscreen -> null -> the click is dropped, which was
 * sites-02d5); deepestElementAt uses own-rects and works wherever the node sits.
 * The elementFromPoint variant is preserved as clickElementAtPoint for the narrow
 * "hit whatever paints on top at this point" case.
 *
 * (Behaviors gated on real user activation — native <select> popups,
 * showPopover(), clipboard, file/opener dialogs — still require a host-driven
 * trusted click; no main-world path can forge that.)
 */
export async function clickElement(el: Element): Promise<void> {
  const t = el as HTMLElement;
  if (typeof t.scrollIntoView === "function" && !isInViewport(t)) {
    t.scrollIntoView({ block: "center", inline: "center" });
    // Let the scroll paint so a usePress pointerup hit-test lands on the
    // now-onscreen element. Bounded even when rAF is starved (see nextFrame).
    await nextFrame();
  }
  const r = t.getBoundingClientRect?.();
  const clientX = r ? Math.round(r.left + r.width / 2) : 0;
  const clientY = r ? Math.round(r.top + r.height / 2) : 0;
  dispatchPointerClick(deepestElementAt(t, clientX, clientY), clientX, clientY);
}

/**
 * PRECISE, COORDINATE-TARGETED click — currently UNUSED; preserved (not deleted)
 * for the narrow cases the default clickElement can't serve, and wired in only
 * when one actually appears (gate it per-call, e.g. an IR step flag — do NOT make
 * it the default; that is exactly what sites-02d5 undid).
 *
 * It hit-tests document.elementFromPoint at the element's centre and dispatches
 * on the TOPMOST node there, retrying across animation frames until that node
 * resolves into the target's own subtree (or a 300ms budget expires, falling back
 * to the node). Reach for it when a click must land on an inner CHILD that
 * carries the handler — a custom element whose listener is on
 * `<jb-select-option> > div.body`, where an event on the outer node would bubble
 * PAST it — or on a portal-rendered item reachable only by coordinate. The
 * tradeoff is the sites-02d5 failure mode: for an offscreen/mid-scroll target the
 * point resolves to null or a wrong node, so it no-ops. That is why it is not the
 * default and must be opted into deliberately.
 */
export async function clickElementAtPoint(el: Element): Promise<void> {
  const t = el as HTMLElement;
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
  dispatchPointerClick(target, clientX, clientY);
}

/**
 * Resolve on the next animation frame — but NEVER hang if frames stop coming.
 *
 * clickElement's settle loop awaits this and relies on its own 300ms wall-clock
 * deadline to fall back to node dispatch; that deadline is only checked BETWEEN
 * frames, so a bare `requestAnimationFrame` await would hang the whole tool when
 * rAF is STARVED — a backgrounded/throttled tab, or a wedged SPA renderer that
 * has stopped painting (observed on jetblue: rAF fired 0x in 2s while a tool hung
 * in the option click). We race rAF against a short timer so the loop keeps
 * turning (and honors its deadline -> node-dispatch fallback) even when no frame
 * ever fires. When rAF is healthy it wins the race (~16ms < the fallback), so the
 * settle fast-path is unchanged; where rAF is absent (a test DOM) the timer is
 * the only arm. Whichever fires first wins; the loser is a harmless no-op.
 */
function nextFrame(): Promise<void> {
  return new Promise((resolve) => {
    let settled = false;
    const done = () => {
      if (settled) return;
      settled = true;
      resolve();
    };
    if (typeof requestAnimationFrame === "function") requestAnimationFrame(done);
    setTimeout(done, 50);
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
