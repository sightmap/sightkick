"use strict";
(() => {
  var __defProp = Object.defineProperty;
  var __defNormalProp = (obj, key, value) => key in obj ? __defProp(obj, key, { enumerable: true, configurable: true, writable: true, value }) : obj[key] = value;
  var __publicField = (obj, key, value) => __defNormalProp(obj, typeof key !== "symbol" ? key + "" : key, value);

  // src/dom.ts
  function deepQueryAll(selector, root = document) {
    const out = [];
    const seen = /* @__PURE__ */ new Set();
    const collect = (r) => {
      let matches = [];
      try {
        matches = Array.from(r.querySelectorAll(selector));
      } catch {
        return;
      }
      for (const el of matches) {
        if (!seen.has(el)) {
          seen.add(el);
          out.push(el);
        }
      }
      const all = r.querySelectorAll("*");
      for (const el of all) {
        const sr = el.shadowRoot;
        if (sr) collect(sr);
      }
    };
    collect(root);
    return out;
  }
  function accessibleText(el) {
    const labelledby = el.getAttribute?.("aria-labelledby");
    if (labelledby) {
      const doc = el.ownerDocument;
      const text = labelledby.split(/\s+/).map((id) => doc?.getElementById(id)?.textContent?.trim() ?? "").filter(Boolean).join(" ");
      if (text) return text;
    }
    const aria = el.getAttribute?.("aria-label");
    if (aria != null && aria.trim() !== "") return aria.trim();
    const labels = el.labels;
    if (labels && labels.length) {
      const text = Array.from(labels).map((l) => l.textContent?.trim() ?? "").filter(Boolean).join(" ");
      if (text) return text;
    }
    const alt = el.getAttribute?.("alt");
    if (alt != null && alt.trim() !== "") return alt.trim();
    const inner = el.innerText;
    if (typeof inner === "string" && inner.trim() !== "") return inner.trim();
    return (el.textContent ?? "").trim();
  }
  function ownText(el) {
    let s = "";
    const kids = el.childNodes;
    for (let i = 0; i < kids.length; i++) {
      const n = kids[i];
      if (n && n.nodeType === 3) s += n.data;
    }
    return s.slice(0, 100).replace(/\s+/g, " ").trim();
  }
  function extract(el, ex) {
    const target = ex.within ? el.querySelector(ex.within) : el;
    if (ex.kind === "exists") {
      return el.querySelector(ex.within ?? "*") ? "true" : "false";
    }
    if (!target) return "";
    switch (ex.kind) {
      case "attr":
        return ex.attr ? target.getAttribute(ex.attr) ?? "" : "";
      case "raw_text":
        return ownText(target);
      case "text":
      default:
        return accessibleText(target);
    }
  }
  function matchPred(el, pred, args) {
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
  function resolvePath(path, args) {
    let scopes = [document];
    let matched = [];
    for (const part of path) {
      const found = [];
      const seen = /* @__PURE__ */ new Set();
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
  function resolveQuery(query, args) {
    const all = resolvePath(query.parts, args);
    if (query.index == null) return all;
    const el = all[query.index];
    return el ? [el] : [];
  }
  function setElementValue(el, value) {
    const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    const desc = Object.getOwnPropertyDescriptor(proto, "value");
    if (desc?.set) {
      desc.set.call(el, value);
    } else {
      el.value = value;
    }
  }
  function makeInputEvent(inputType, data) {
    if (typeof InputEvent !== "undefined") {
      return new InputEvent("input", { bubbles: true, inputType, data: data ?? void 0 });
    }
    return new Event("input", { bubbles: true });
  }
  function typeInto(el, value) {
    const target = el;
    target.focus?.();
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
  function dispatchPointerClick(target, clientX, clientY) {
    const init = (buttons) => ({
      bubbles: true,
      cancelable: true,
      composed: true,
      clientX,
      clientY,
      button: 0,
      buttons
    });
    const hasPE = typeof PointerEvent !== "undefined";
    const emit = (type, buttons, pointer) => {
      if (pointer && hasPE) {
        target.dispatchEvent(
          new PointerEvent(type, { ...init(buttons), pointerId: 1, pointerType: "mouse", isPrimary: true })
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
  function deepestElementAt(root, x, y) {
    let node = root;
    for (; ; ) {
      let next = null;
      const kids = node.children;
      for (let i = 0; i < kids.length; i++) {
        const child = kids[i];
        const r = child.getBoundingClientRect?.();
        if (r && x >= r.left && x <= r.right && y >= r.top && y <= r.bottom) next = child;
      }
      if (!next) return node;
      node = next;
    }
  }
  async function clickElement(el) {
    const t = el;
    if (typeof t.scrollIntoView === "function" && !isInViewport(t)) {
      t.scrollIntoView({ block: "center", inline: "center" });
      await nextFrame();
    }
    const r = t.getBoundingClientRect?.();
    const clientX = r ? Math.round(r.left + r.width / 2) : 0;
    const clientY = r ? Math.round(r.top + r.height / 2) : 0;
    dispatchPointerClick(deepestElementAt(t, clientX, clientY), clientX, clientY);
  }
  function nextFrame() {
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
  function isInViewport(el) {
    const r = el.getBoundingClientRect?.();
    if (!r) return true;
    const vh = typeof window !== "undefined" && window.innerHeight || 0;
    const vw = typeof window !== "undefined" && window.innerWidth || 0;
    if (!vh || !vw) return true;
    return r.top >= 0 && r.left >= 0 && r.bottom <= vh && r.right <= vw;
  }
  function interpolate(template, args) {
    return template.replace(/\{\{\s*([\w.]+)\s*\}\}/g, (_, key) => {
      const v = args[key];
      return v == null ? "" : String(v);
    });
  }

  // src/executor.ts
  var sleep = (ms) => new Promise((r) => setTimeout(r, ms));
  function resolveOptions(options = {}) {
    return {
      pollMs: options.pollMs ?? 100,
      currentPath: options.currentPath ?? (typeof window !== "undefined" ? window.location.pathname : "/"),
      log: options.log ?? ((m) => console.warn(`[sightkick] ${m}`)),
      signal: options.signal
    };
  }
  function routeMatches(pattern, path) {
    const norm = (p) => {
      const bare = p.split("#")[0].split("?")[0];
      const trimmed = bare.length > 1 && bare.endsWith("/") ? bare.slice(0, -1) : bare;
      return trimmed || "/";
    };
    const pat = norm(pattern);
    const pth = norm(path);
    if (pat === "/") return pth === "/";
    const segs = pat.split("/").filter((s) => s.length > 0);
    const rx = "^" + segs.map((seg) => {
      if (seg === "**") return "(?:/.+)?";
      if (seg === "*" || seg.startsWith(":")) return "/[^/]+";
      return "/" + seg.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    }).join("") + "$";
    return new RegExp(rx).test(pth);
  }
  function guardHolds(guard, args) {
    const matches = resolveQuery(guard.query, args).length;
    return guard.kind === "present" ? matches > 0 : matches === 0;
  }
  function renderPred(p) {
    const prop = p.property ?? "";
    const ci = p.ci ? " i" : "";
    return `[${prop}${p.op}"${p.value}"${ci}]`;
  }
  function renderTarget(step) {
    if (step.target) return step.target;
    const parts = step.query?.parts;
    if (parts && parts.length) {
      return parts.map((p) => (p.locators[0] ?? "*") + (p.preds ?? []).map(renderPred).join("")).join(" ");
    }
    if (step.view) return step.view;
    if (step.route) return `route ${step.route}`;
    if (step.url) return step.url;
    if (step.key) return `key ${step.key}`;
    return step.op;
  }
  function templateParamNames(s, out) {
    if (!s) return;
    for (const m of s.matchAll(/\{\{\s*([\w.]+)\s*\}\}/g)) out.add(m[1]);
  }
  function stepParams(step) {
    const out = /* @__PURE__ */ new Set();
    templateParamNames(step.value, out);
    templateParamNames(step.url, out);
    templateParamNames(step.key, out);
    for (const part of step.query?.parts ?? []) {
      for (const pred of part.preds ?? []) templateParamNames(pred.value, out);
    }
    return [...out];
  }
  function shouldSkipStep(step, args) {
    if (step.when !== void 0) {
      return interpolate(step.when, args).trim() === "";
    }
    for (const p of stepParams(step)) {
      if (!(p in args) || args[p] === void 0) return true;
    }
    return false;
  }
  function isActionable(el) {
    const h = el;
    if (typeof h.getBoundingClientRect !== "function") return false;
    const style = typeof getComputedStyle === "function" ? getComputedStyle(h) : null;
    if (h.offsetParent === null && style?.position !== "fixed") return false;
    const r = h.getBoundingClientRect();
    return r.width > 0 && r.height > 0;
  }
  async function runStep(step, args, opts) {
    const target = () => {
      if (!step.query) return void 0;
      const matches = resolveQuery(step.query, args);
      return matches.find(isActionable) ?? matches[0];
    };
    switch (step.op) {
      case "navigate": {
        const path = opts.currentPath;
        if (step.route && !routeMatches(step.route, path)) {
          opts.log(`navigate: single-page slice cannot leave ${path} for ${step.route} (deferred to journey work)`);
        }
        return;
      }
      case "goto": {
        const url = interpolate(step.url ?? "", args);
        if (url && typeof window !== "undefined") {
          setTimeout(() => window.location.assign(url), 0);
        }
        return;
      }
      case "fill": {
        const el = target();
        if (!el) throw new Error(`fill: no element for ${renderTarget(step)}`);
        typeInto(el, interpolate(step.value ?? "", args));
        return;
      }
      case "click": {
        const el = target();
        if (!el) throw new Error(`click: no element for ${renderTarget(step)}`);
        await clickElement(el);
        return;
      }
      case "keypress": {
        const key = step.key ?? "";
        if (!key) throw new Error("keypress: no key given");
        const el = document.activeElement ?? document.body;
        el.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }));
        el.dispatchEvent(new KeyboardEvent("keyup", { key, bubbles: true }));
        return;
      }
      case "waitFor": {
        const routeSatisfied = () => !!step.route && routeMatches(step.route, typeof window !== "undefined" ? window.location.pathname : opts.currentPath);
        const satisfied = step.query ? () => !!target() : routeSatisfied;
        const deadline = Date.now() + (step.timeoutMs ?? 5e3);
        for (; ; ) {
          if (opts.signal?.aborted) throw new Error("aborted");
          if (satisfied()) return;
          if (Date.now() >= deadline) {
            throw new Error(`waitFor: timed out after ${step.timeoutMs ?? 5e3}ms for ${renderTarget(step)}`);
          }
          await sleep(opts.pollMs);
        }
      }
      default:
        throw new Error(`unknown step op ${step.op}`);
    }
  }
  function extractFields(el, fields) {
    const obj = {};
    for (const [name, f] of Object.entries(fields)) {
      obj[name] = extract(el, f.extractor);
    }
    return obj;
  }
  function computeReturn(ret, args) {
    if (ret.kind === "list") {
      const rows = ret.query ? resolveQuery(ret.query, args) : [];
      const fields = ret.fields ?? {};
      return { ok: true, items: rows.map((el2) => extractFields(el2, fields)) };
    }
    const el = ret.query ? resolveQuery(ret.query, args)[0] : void 0;
    const value = el && ret.extractor ? extract(el, ret.extractor) : void 0;
    const out = { ok: true };
    if (value !== void 0) out.value = value;
    return out;
  }
  async function runTool(tool, args = {}, options = {}) {
    const opts = resolveOptions(options);
    if (tool.ensureView && !routeMatches(tool.ensureView.route, opts.currentPath)) {
      opts.log(`ensure_view: "${tool.name}" expects ${tool.ensureView.view} (${tool.ensureView.route}) but path is ${opts.currentPath}; proceeding best-effort`);
    }
    if (tool.guard && guardHolds(tool.guard, args)) {
      const skipped = tool.returns ? { ...computeReturn(tool.returns, args), skipped: true } : { ok: true, skipped: true };
      skipped.message = "guard satisfied; steps skipped (already applied)";
      if (tool.guidance && tool.guidance.length) skipped.guidance = tool.guidance;
      return skipped;
    }
    try {
      for (const step of tool.steps) {
        if (shouldSkipStep(step, args)) {
          opts.log(`skip ${step.op} step (optional field absent)`);
          continue;
        }
        await runStep(step, args, opts);
      }
    } catch (err) {
      return { ok: false, message: err.message };
    }
    const result = tool.returns ? computeReturn(tool.returns, args) : { ok: true };
    if (tool.guidance && tool.guidance.length) result.guidance = tool.guidance;
    return result;
  }
  function projectAction(step) {
    const view = { op: step.op, target: renderTarget(step) };
    if (step.when !== void 0) view.when = step.when;
    return view;
  }
  function describeAction(step) {
    const target = renderTarget(step);
    return target && target !== step.op ? `${step.op} ${target}` : step.op;
  }
  function fragmentUses(step) {
    const out = new Set(stepParams(step));
    templateParamNames(step.when, out);
    return [...out].sort();
  }
  function projectFragments(ir) {
    const out = [];
    for (const tool of ir.tools) {
      tool.steps.forEach((step, index) => {
        out.push({
          id: `${tool.name}.${index}`,
          tool: tool.name,
          index,
          op: step.op,
          label: describeAction(step),
          uses: fragmentUses(step)
        });
      });
    }
    return out;
  }
  async function execActions(actions, args = {}, options = {}) {
    const opts = resolveOptions(options);
    const livePath = () => typeof window !== "undefined" ? window.location.pathname : opts.currentPath;
    for (let i = 0; i < actions.length; i++) {
      const step = actions[i];
      if (shouldSkipStep(step, args)) {
        opts.log(`skip ${step.op} action (optional field absent)`);
        continue;
      }
      try {
        await runStep(step, args, opts);
      } catch (err) {
        return {
          completedThrough: i,
          total: actions.length,
          done: false,
          interrupt: {
            at: i,
            action: describeAction(step),
            reason: err.message,
            observed: { path: livePath() }
          },
          remaining: actions.slice(i),
          remainingView: actions.slice(i).map(projectAction)
        };
      }
    }
    return { completedThrough: actions.length, total: actions.length, done: true, remaining: [], remainingView: [] };
  }

  // src/webmcp.ts
  var POLYFILL_FLAG = "__sightkickPolyfill";
  var _a, _b;
  var ModelContextPolyfill = class extends (_b = EventTarget, _a = POLYFILL_FLAG, _b) {
    constructor() {
      super(...arguments);
      __publicField(this, _a, true);
      __publicField(this, "tools", /* @__PURE__ */ new Map());
    }
    registerTool(def, options) {
      this.tools.set(def.name, def);
      if (options?.signal) {
        options.signal.addEventListener(
          "abort",
          () => {
            if (this.tools.get(def.name) === def) {
              this.tools.delete(def.name);
              this.dispatchEvent(new Event("toolchange"));
            }
          },
          { once: true }
        );
      }
      this.dispatchEvent(new Event("toolchange"));
      return Promise.resolve();
    }
    getTools() {
      const origin = typeof location !== "undefined" ? location.origin : "null";
      return Promise.resolve(
        [...this.tools.values()].map((t) => ({
          name: t.name,
          description: t.description,
          inputSchema: t.inputSchema,
          origin
        }))
      );
    }
    executeTool(tool, args = {}, options) {
      const def = this.tools.get(tool.name);
      if (!def) {
        return Promise.reject(new Error(`unknown tool "${tool.name}"`));
      }
      return Promise.resolve(def.execute(args, options));
    }
  };
  function ensureModelContext() {
    if (typeof document === "undefined") return void 0;
    const doc = document;
    if (doc.modelContext) return doc.modelContext;
    const poly = new ModelContextPolyfill();
    Object.defineProperty(doc, "modelContext", { value: poly, configurable: true, writable: true });
    return poly;
  }
  function isPolyfilled(ctx) {
    return !!ctx && ctx[POLYFILL_FLAG] === true;
  }

  // src/errors.ts
  function describeError(e) {
    if (e instanceof Error) return `${e.name}: ${e.message}`;
    if (typeof e === "object" && e !== null) {
      const anyE = e;
      if (anyE.message != null || anyE.name != null) {
        return `${String(anyE.name ?? "Error")}: ${String(anyE.message ?? "")}`.trim();
      }
      try {
        const s = JSON.stringify(e);
        if (s && s !== "{}") return s;
      } catch {
      }
      return Object.prototype.toString.call(e);
    }
    return String(e);
  }

  // src/boot.ts
  function detectMode() {
    return typeof window !== "undefined" && window.__sightkick_host != null ? "injected" : "direct";
  }
  function findTool(ir, name) {
    return ir?.tools.find((t) => t.name === name);
  }
  function toEnvelope(result) {
    return { content: [{ type: "text", text: JSON.stringify(result) }], isError: !result.ok };
  }
  function metaEnvelope(payload, isError = false) {
    return { content: [{ type: "text", text: JSON.stringify(payload) }], isError };
  }
  function fragmentStep(ir, ref) {
    if (!ir) return void 0;
    const dot = ref.lastIndexOf(".");
    if (dot < 0) return void 0;
    const idx = Number(ref.slice(dot + 1));
    if (!Number.isInteger(idx) || idx < 0) return void 0;
    const tool = ir.tools.find((t) => t.name === ref.slice(0, dot));
    return tool?.steps[idx];
  }
  function resolveFragmentRefs(ir, refs) {
    const steps = [];
    const unknown = [];
    for (const ref of refs) {
      const step = fragmentStep(ir, ref);
      if (step) steps.push(step);
      else unknown.push(ref);
    }
    return { steps, unknown };
  }
  function projectExecStatus(status, refs) {
    return {
      done: status.done,
      completedThrough: status.completedThrough,
      total: status.total,
      interrupt: status.interrupt,
      remaining: refs.slice(status.completedThrough),
      remainingView: status.remainingView
    };
  }
  var historyPatched = false;
  function patchHistory() {
    if (historyPatched || typeof history === "undefined" || typeof window === "undefined") return;
    historyPatched = true;
    const wrap = (orig) => function(data, unused, url) {
      const r = orig.call(this, data, unused, url);
      window.dispatchEvent(new Event("sightkick:navigate"));
      return r;
    };
    history.pushState = wrap(history.pushState.bind(history));
    history.replaceState = wrap(history.replaceState.bind(history));
  }
  function boot(initial, opts = {}) {
    const ctx = ensureModelContext();
    const currentPath = () => opts.currentPath ?? (typeof window !== "undefined" ? window.location.pathname : "/");
    let registrations = [];
    let registered = [];
    const unregisterAll = () => {
      for (const c of registrations) c.abort();
      registrations = [];
      registered = [];
    };
    const refresh = () => {
      unregisterAll();
      const ir = api.ir;
      if (!ir || !ctx) return;
      const path = currentPath();
      for (const tool of ir.tools) {
        if (tool.ensureView && !routeMatches(tool.ensureView.route, path)) continue;
        const controller = new AbortController();
        registrations.push(controller);
        registered.push({ name: tool.name, description: tool.description });
        Promise.resolve(
          ctx.registerTool(
            {
              name: tool.name,
              description: tool.description ?? "",
              inputSchema: tool.inputSchema,
              execute: async (args, options) => toEnvelope(await runTool(tool, args, { signal: options?.signal, currentPath: path }))
            },
            { signal: controller.signal }
          )
        ).catch((e) => console.warn(`[sightkick] registerTool "${tool.name}" rejected: ${describeError(e)}`));
      }
    };
    const currentFragments = () => {
      const ir = api.ir;
      if (!ir) return [];
      const path = currentPath();
      return projectFragments(ir).filter((f) => {
        const tool = ir.tools.find((t) => t.name === f.tool);
        return !tool?.ensureView || routeMatches(tool.ensureView.route, path);
      });
    };
    const metaControllers = [];
    let metaRegistered = false;
    const registerMetaTool = (def) => {
      const controller = new AbortController();
      metaControllers.push(controller);
      Promise.resolve(ctx.registerTool(def, { signal: controller.signal })).catch(
        (e) => console.warn(`[sightkick] registerTool "${def.name}" rejected: ${describeError(e)}`)
      );
    };
    const registerMetaTools = async () => {
      if (metaRegistered || !ctx) return;
      metaRegistered = true;
      let present = /* @__PURE__ */ new Set();
      try {
        present = new Set((await ctx.getTools()).map((t) => t.name));
      } catch {
      }
      if (!present.has("exec_actions")) {
        registerMetaTool({
          name: "exec_actions",
          description: "Run an ordered list of action fragments resumably. Pass fragment ids (from get_fragments) in `refs` and any parameter values in `args`. Returns how far it got; if an action is interrupted (e.g. an unexpected modal, a missing field), it STOPS instead of hanging and returns the reason plus the remaining fragment refs. Handle the interruption (dismiss the modal, call another tool), then call exec_actions again with the returned `remaining` refs to resume where it left off.",
          inputSchema: {
            type: "object",
            properties: {
              refs: {
                type: "array",
                items: { type: "string" },
                description: 'Fragment ids to run in order, e.g. ["set_contact.0", "set_contact.1"].'
              },
              args: {
                type: "object",
                description: "Parameter values the fragments interpolate (see each fragment's `uses`)."
              }
            },
            required: ["refs"]
          },
          execute: async (rawArgs, options) => {
            const refs = Array.isArray(rawArgs?.refs) ? rawArgs.refs.map(String) : [];
            const callArgs = rawArgs?.args ?? {};
            if (!api.ir) return metaEnvelope({ error: "no IR loaded" }, true);
            if (!refs.length) return metaEnvelope({ error: "exec_actions needs a non-empty `refs` array" }, true);
            const { steps, unknown } = resolveFragmentRefs(api.ir, refs);
            if (unknown.length) {
              return metaEnvelope(
                { error: `unknown fragment ref(s): ${unknown.join(", ")}`, hint: "call get_fragments for valid ids" },
                true
              );
            }
            const status = await execActions(steps, callArgs, { signal: options?.signal, currentPath: currentPath() });
            return metaEnvelope(projectExecStatus(status, refs), !status.done);
          }
        });
      }
      if (!present.has("get_fragments")) {
        registerMetaTool({
          name: "get_fragments",
          description: "List the action fragments available on the current view. Each is a pluckable step {id, tool, op, label, uses}: `label` reads in component/view vocabulary, `uses` names the parameters it needs. Compose an ordered list of `id`s and pass them to exec_actions.",
          inputSchema: { type: "object", properties: {} },
          execute: async () => metaEnvelope({ fragments: currentFragments() })
        });
      }
    };
    const api = {
      mode: detectMode(),
      ir: null,
      modelContext: ctx,
      polyfilled: isPolyfilled(ctx),
      load(ir) {
        this.ir = ir;
        refresh();
        registerMetaTools();
        console.info(
          `[sightkick] loaded IR "${ir.name}" (${ir.tools.length} tools, ${this.mode}, ${this.polyfilled ? "polyfilled" : "native"} modelContext)`
        );
      },
      tools() {
        return registered.slice();
      },
      refresh,
      call(name, args = {}, options) {
        const tool = findTool(this.ir, name);
        if (!tool) return Promise.resolve({ ok: false, message: `unknown tool "${name}"` });
        return runTool(tool, args, options);
      },
      execActions(actions, args = {}, options) {
        return execActions(actions, args, options);
      },
      fragments() {
        return this.ir ? projectFragments(this.ir) : [];
      }
    };
    if (typeof window !== "undefined" && opts.currentPath === void 0) {
      let lastPath = currentPath();
      const onNav = () => {
        const p = currentPath();
        if (p !== lastPath) {
          lastPath = p;
          refresh();
        }
      };
      window.addEventListener("popstate", onNav);
      window.addEventListener("sightkick:navigate", onNav);
      patchHistory();
    }
    if (initial) api.load(initial);
    return api;
  }

  // src/channel.ts
  var IR_ATTR = "data-sightkick-ir";
  var HOST_ATTR = "data-sightkick-host";
  var IR_EVENT = "sightkick:ir";
  function loadFromDom(api) {
    if (typeof document === "undefined" || !document.documentElement) return false;
    const de = document.documentElement;
    const raw = de.getAttribute(IR_ATTR);
    if (!raw) return false;
    const host = de.getAttribute(HOST_ATTR);
    de.removeAttribute(IR_ATTR);
    de.removeAttribute(HOST_ATTR);
    try {
      const ir = JSON.parse(raw);
      if (host) {
        try {
          window.__sightkick_host = JSON.parse(host);
          api.mode = "injected";
        } catch {
        }
      }
      api.load(ir);
      return true;
    } catch (e) {
      console.warn("[sightkick] IR channel: bad payload", e);
      return false;
    }
  }
  function installIrChannel(api) {
    if (typeof document === "undefined") return;
    if (loadFromDom(api)) return;
    let tries = 0;
    const poll = setInterval(() => {
      if (loadFromDom(api) || ++tries > 40) clearInterval(poll);
    }, 50);
    document.addEventListener(
      IR_EVENT,
      () => {
        if (loadFromDom(api)) clearInterval(poll);
      },
      { once: true }
    );
  }

  // src/client.ts
  function createClient(ctx = ensureModelContext()) {
    if (!ctx) throw new Error("createClient: no document.modelContext available");
    return {
      async listTools() {
        try {
          return await ctx.getTools();
        } catch (e) {
          throw new Error(`getTools failed: ${describeError(e)}`);
        }
      },
      async callTool(name, args = {}, options) {
        const tools = await ctx.getTools();
        const tool = tools.find((t) => t.name === name);
        if (!tool) throw new Error(`unknown tool "${name}"`);
        let raw;
        try {
          raw = await ctx.executeTool(tool, args, options);
        } catch (e) {
          throw new Error(`executeTool "${name}" failed: ${describeError(e)}`);
        }
        return typeof raw === "string" ? JSON.parse(raw) : raw;
      }
    };
  }

  // src/index.ts
  if (typeof window !== "undefined") {
    const api = boot(window.__sightkick_ir);
    window.__sightkick = api;
    if (!window.__sightkick_ir) installIrChannel(api);
  }
})();
