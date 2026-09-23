// ../runtime/src/dom.ts
function interpolate(template, args) {
  return template.replace(/\{\{\s*([\w.]+)\s*\}\}/g, (_, key) => {
    const v = args[key];
    return v == null ? "" : String(v);
  });
}

// ../runtime/src/executor.ts
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

// src/index.ts
function literal(value) {
  return `JSON.parse(${JSON.stringify(JSON.stringify(value))})`;
}
function queryExpression(query, args) {
  return `api.resolveQuery(${literal(query)},${literal(args)})`;
}
function evaluateSource(body) {
  return `(()=>{const api=${'(()=>{var dom=(()=>{var a=Object.defineProperty;var p=Object.getOwnPropertyDescriptor;var b=Object.getOwnPropertyNames;var E=Object.prototype.hasOwnProperty;var w=(e,t)=>{for(var n in t)a(e,n,{get:t[n],enumerable:!0})},h=(e,t,n,o)=>{if(t&&typeof t=="object"||typeof t=="function")for(let r of b(t))!E.call(e,r)&&r!==n&&a(e,r,{get:()=>t[r],enumerable:!(o=p(t,r))||o.enumerable});return e};var y=e=>h(a({},"__esModule",{value:!0}),e);var T={};w(T,{extract:()=>d,resolveQuery:()=>f});function v(e,t=document){let n=[],o=new Set,r=i=>{let s=[];try{s=Array.from(i.querySelectorAll(e))}catch{return}for(let u of s)o.has(u)||(o.add(u),n.push(u));let c=i.querySelectorAll("*");for(let u of c){let l=u.shadowRoot;l&&r(l)}};return r(t),n}function g(e){let t=e.getAttribute?.("aria-labelledby");if(t){let s=e.ownerDocument,c=t.split(/\\s+/).map(u=>s?.getElementById(u)?.textContent?.trim()??"").filter(Boolean).join(" ");if(c)return c}let n=e.getAttribute?.("aria-label");if(n!=null&&n.trim()!=="")return n.trim();let o=e.labels;if(o&&o.length){let s=Array.from(o).map(c=>c.textContent?.trim()??"").filter(Boolean).join(" ");if(s)return s}let r=e.getAttribute?.("alt");if(r!=null&&r.trim()!=="")return r.trim();let i=e.innerText;return typeof i=="string"&&i.trim()!==""?i.trim():(e.textContent??"").trim()}function d(e,t){let n=t.within?e.querySelector(t.within):e;if(t.kind==="exists")return e.querySelector(t.within??"*")?"true":"false";if(!n)return"";switch(t.kind){case"attr":return t.attr?n.getAttribute(t.attr)??"":"";case"text":default:return g(n)}}function x(e,t,n){let o=d(e,t.extractor),r=P(t.value,n);switch(t.ci&&(o=o.toLowerCase(),r=r.toLowerCase()),t.op){case"^=":return o.startsWith(r);case"*=":return o.includes(r);default:return o===r}}function k(e,t){let n=[document],o=[];for(let r of e){let i=[],s=new Set;for(let c of n)for(let u of r.locators)for(let l of v(u,c))s.has(l)||(r.preds??[]).every(m=>x(l,m,t))&&(s.add(l),i.push(l));o=i,n=i}return o}function f(e,t){let n=k(e.parts,t);if(e.index==null)return n;let o=n[e.index];return o?[o]:[]}function P(e,t){return e.replace(/\\{\\{\\s*([\\w.]+)\\s*\\}\\}/g,(n,o)=>{let r=t[o];return r==null?"":String(r)})}return y(T);})();\n;return dom;})()'};${body}})()`;
}
async function count(page, query, args) {
  return page.evaluate(evaluateSource(`return ${queryExpression(query, args)}.length;`));
}
async function target(page, query, args) {
  const handle = await page.evaluateHandle(evaluateSource(`const matches=${queryExpression(query, args)};
    return matches.find(el=>{const style=getComputedStyle(el);const r=el.getBoundingClientRect();return (el.offsetParent!==null||style.position==="fixed")&&r.width>0&&r.height>0;})||matches[0]||null;`));
  const element = handle.asElement();
  if (!element) await handle.dispose();
  return element;
}
async function read(page, ret, args) {
  if (!ret) return { ok: true };
  const json = await page.evaluate(evaluateSource(`return JSON.stringify((()=>{const ret=${literal(ret)};
    const matches=ret.query?api.resolveQuery(ret.query,${literal(args)}):[];
    if(ret.kind==="list")return {ok:true,items:matches.map(el=>Object.fromEntries(Object.entries(ret.fields||{}).map(([name,f])=>[name,api.extract(el,f.extractor)])))};
    return matches[0]&&ret.extractor?{ok:true,value:api.extract(matches[0],ret.extractor)}:{ok:true};})());`));
  return JSON.parse(json);
}
async function waitForQuery(page, query, args, timeout) {
  const handle = await page.waitForFunction(evaluateSource(`return ${queryExpression(query, args)}.length>0;`), void 0, { timeout });
  await handle.dispose();
}
function waitForView(page, route, timeout) {
  return page.waitForURL((url) => routeMatches(route, url.pathname), { timeout, waitUntil: "commit" });
}
function validateArgs(tool, args) {
  for (const key of tool.inputSchema.required ?? []) {
    if (!Object.hasOwn(args, key) || args[key] === void 0) throw new Error(`missing required param ${key}`);
  }
  for (const [key, value] of Object.entries(args)) {
    const prop = Object.hasOwn(tool.inputSchema.properties, key) ? tool.inputSchema.properties[key] : void 0;
    if (!prop) throw new Error(`unknown param ${key}`);
    if (value === void 0 && !(tool.inputSchema.required ?? []).includes(key)) continue;
    if (typeof value !== prop.type || typeof value === "number" && !Number.isFinite(value)) throw new Error(`invalid ${prop.type} param ${key}`);
    if (prop.enum && !prop.enum.includes(value)) throw new Error(`invalid enum param ${key}`);
  }
}
async function run(page, tool, args, timeout, log) {
  try {
    validateArgs(tool, args);
    if (tool.mode !== "live") throw new Error(`unsupported tool mode ${tool.mode}`);
    if (tool.ensureView && !routeMatches(tool.ensureView.route, new URL(page.url()).pathname)) {
      log(`ensure_view: "${tool.name}" expects ${tool.ensureView.view} (${tool.ensureView.route}); proceeding best-effort`);
    }
    const skipped = !!tool.guard && await count(page, tool.guard.query, args) > 0 === (tool.guard.kind === "present");
    if (!skipped) for (const step of tool.steps) {
      if (shouldSkipStep(step, args)) continue;
      const stepTimeout = step.timeoutMs ?? timeout;
      if (!Number.isFinite(stepTimeout) || stepTimeout <= 0) throw new Error("step timeoutMs must be a positive finite number");
      switch (step.op) {
        case "navigate":
          if (step.route && !routeMatches(step.route, new URL(page.url()).pathname)) log(`navigate: positioning is advisory (${step.route})`);
          break;
        case "goto": {
          const url = interpolate(step.url ?? "", args);
          if (url) await page.goto(new URL(url, page.url()).href, { timeout: stepTimeout, waitUntil: "commit" });
          break;
        }
        case "keypress":
          if (!step.key) throw new Error("keypress: no key given");
          await page.keyboard.press(step.key);
          break;
        case "waitFor":
          if (step.query) await waitForQuery(page, step.query, args, stepTimeout);
          else if (step.route) await waitForView(page, step.route, stepTimeout);
          else throw new Error("waitFor: missing query or route");
          break;
        case "fill":
        case "click": {
          if (!step.query) throw new Error(`${step.op}: missing query`);
          const el = await target(page, step.query, args);
          if (!el) throw new Error(`${step.op}: no element for query`);
          try {
            if (step.op === "click") await el.click({ timeout: stepTimeout });
            else {
              await el.fill("", { timeout: stepTimeout });
              await el.type(interpolate(step.value ?? "", args), { timeout: stepTimeout });
              if (await el.getAttribute("role") === "combobox") await el.press("ArrowDown", { timeout: stepTimeout });
            }
          } finally {
            await el.dispose();
          }
          break;
        }
        default:
          throw new Error(`unknown step op ${step.op}`);
      }
    }
    const result = await read(page, tool.returns, args);
    if (skipped) {
      result.skipped = true;
      result.message = "guard satisfied; steps skipped (already applied)";
    }
    if (tool.guidance?.length) result.guidance = tool.guidance;
    return result;
  } catch (error) {
    return { ok: false, message: error instanceof Error ? error.message : String(error) };
  }
}
function bindSightkick(page, ir, options = {}) {
  const timeout = options.timeoutMs ?? 5e3;
  if (!Number.isFinite(timeout) || timeout <= 0) throw new Error("timeoutMs must be a positive finite number");
  const log = options.log ?? ((message) => console.warn(`[sightkick] ${message}`));
  const tools = /* @__PURE__ */ Object.create(null);
  const views = /* @__PURE__ */ Object.create(null);
  for (const tool of ir.tools) tools[tool.name] = (args = {}) => run(page, tool, args, timeout, log);
  for (const view of ir.views) views[view.name] = { waitFor: (opts = {}) => {
    const value = opts.timeoutMs ?? timeout;
    if (!Number.isFinite(value) || value <= 0) return Promise.reject(new Error("timeoutMs must be a positive finite number"));
    return waitForView(page, view.route, value);
  } };
  return { tools, views };
}
export {
  bindSightkick
};
