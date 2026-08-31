import type { Extractor, Where } from "./ir.js";

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

/** Try each locator in order; return all elements matched by the first that hits. */
export function queryLocators(locators: string[]): Element[] {
  for (const loc of locators) {
    const found = deepQueryAll(loc);
    if (found.length > 0) return found;
  }
  return [];
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
      return (target.textContent ?? "").trim();
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

/** Filter candidates by a where-clause (already-interpolated). */
export function filterWhere(candidates: Element[], where: Where): Element[] {
  return candidates.filter((el) => extract(el, where.extractor) === where.equals);
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
