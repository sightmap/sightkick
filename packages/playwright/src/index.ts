import type { Page, ElementHandle } from "playwright";
import type { IR, Query, Return, Tool } from "../../runtime/src/ir.js";
import type { ToolResult } from "../../runtime/src/executor.js";
import { routeMatches, shouldSkipStep } from "../../runtime/src/executor.js";
import { interpolate } from "../../runtime/src/dom.js";

declare const SIGHTKICK_DOM: string;
type Args = Record<string, unknown>;
export interface Options { timeoutMs?: number; log?: (message: string) => void }

// Source values always enter evaluation through JSON, never string interpolation
// into selectors or executable code. The resolver performs predicate comparison.
function literal(value: unknown): string { return `JSON.parse(${JSON.stringify(JSON.stringify(value))})`; }
function queryExpression(query: Query, args: Args): string {
  return `api.resolveQuery(${literal(query)},${literal(args)})`;
}
function evaluateSource(body: string): string {
  return `(()=>{const api=${SIGHTKICK_DOM};${body}})()`;
}
async function count(page: Page, query: Query, args: Args): Promise<number> {
  return page.evaluate(evaluateSource(`return ${queryExpression(query, args)}.length;`));
}
async function target(page: Page, query: Query, args: Args): Promise<ElementHandle<Element> | null> {
  // Same target preference as WebMCP: first visible match, else first match.
  const handle = await page.evaluateHandle(evaluateSource(`const matches=${queryExpression(query, args)};
    return matches.find(el=>{const style=getComputedStyle(el);const r=el.getBoundingClientRect();return (el.offsetParent!==null||style.position==="fixed")&&r.width>0&&r.height>0;})||matches[0]||null;`));
  const element = handle.asElement();
  if (!element) await handle.dispose();
  return element;
}
async function read(page: Page, ret: Return | undefined, args: Args): Promise<ToolResult> {
  if (!ret) return { ok: true };
  const json = await page.evaluate(evaluateSource(`return JSON.stringify((()=>{const ret=${literal(ret)};
    const matches=ret.query?api.resolveQuery(ret.query,${literal(args)}):[];
    if(ret.kind==="list")return {ok:true,items:matches.map(el=>Object.fromEntries(Object.entries(ret.fields||{}).map(([name,f])=>[name,api.extract(el,f.extractor)])))};
    return matches[0]&&ret.extractor?{ok:true,value:api.extract(matches[0],ret.extractor)}:{ok:true};})());`));
  return JSON.parse(json as string) as ToolResult;
}
async function waitForQuery(page: Page, query: Query, args: Args, timeout: number): Promise<void> {
  const handle = await page.waitForFunction(evaluateSource(`return ${queryExpression(query, args)}.length>0;`), undefined, {timeout});
  await handle.dispose();
}
function waitForView(page: Page, route: string, timeout: number): Promise<void> {
  return page.waitForURL(url => routeMatches(route, url.pathname), {timeout, waitUntil: "commit"});
}
function validateArgs(tool: Tool, args: Args): void {
  for (const key of tool.inputSchema.required ?? []) {
    if (!Object.hasOwn(args, key) || args[key] === undefined) throw new Error(`missing required param ${key}`);
  }
  for (const [key, value] of Object.entries(args)) {
    const prop = Object.hasOwn(tool.inputSchema.properties, key) ? tool.inputSchema.properties[key] : undefined;
    if (!prop) throw new Error(`unknown param ${key}`);
    if (value === undefined && !(tool.inputSchema.required ?? []).includes(key)) continue;
    if (typeof value !== prop.type || (typeof value === "number" && !Number.isFinite(value))) throw new Error(`invalid ${prop.type} param ${key}`);
    if (prop.enum && !prop.enum.includes(value as string)) throw new Error(`invalid enum param ${key}`);
  }
}
async function run(page: Page, tool: Tool, args: Args, timeout: number, log: (message: string) => void): Promise<ToolResult> {
  try {
    validateArgs(tool, args);
    if (tool.mode !== "live") throw new Error(`unsupported tool mode ${tool.mode}`);
    if (tool.ensureView && !routeMatches(tool.ensureView.route, new URL(page.url()).pathname)) {
      log(`ensure_view: "${tool.name}" expects ${tool.ensureView.view} (${tool.ensureView.route}); proceeding best-effort`);
    }
    const skipped = !!tool.guard && ((await count(page, tool.guard.query, args) > 0) === (tool.guard.kind === "present"));
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
          if (url) await page.goto(new URL(url, page.url()).href, {timeout: stepTimeout, waitUntil: "commit"});
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
            if (step.op === "click") await el.click({timeout: stepTimeout});
            else {
              await el.fill("", {timeout: stepTimeout});
              await el.type(interpolate(step.value ?? "", args), {timeout: stepTimeout});
              if (await el.getAttribute("role") === "combobox") await el.press("ArrowDown", {timeout: stepTimeout});
            }
          } finally { await el.dispose(); }
          break;
        }
        default: throw new Error(`unknown step op ${step.op}`);
      }
    }
    const result = await read(page, tool.returns, args);
    if (skipped) { result.skipped = true; result.message = "guard satisfied; steps skipped (already applied)"; }
    if (tool.guidance?.length) result.guidance = tool.guidance;
    return result;
  } catch (error) {
    return {ok: false, message: error instanceof Error ? error.message : String(error)};
  }
}

/** Bind an immutable compiled module to a caller-owned Page. No browser is launched. */
export function bindSightkick(page: Page, ir: IR, options: Options = {}) {
  const timeout = options.timeoutMs ?? 5000;
  if (!Number.isFinite(timeout) || timeout <= 0) throw new Error("timeoutMs must be a positive finite number");
  const log = options.log ?? ((message: string) => console.warn(`[sightkick] ${message}`));
  // Null prototypes allow corpus names such as __proto__ and constructor.
  const tools = Object.create(null) as Record<string, (args?: Args) => Promise<ToolResult>>;
  const views = Object.create(null) as Record<string, {waitFor(options?: {timeoutMs?: number}): Promise<void>}>;
  for (const tool of ir.tools) tools[tool.name] = (args = {}) => run(page, tool, args, timeout, log);
  for (const view of ir.views) views[view.name] = {waitFor: (opts = {}) => {
    const value = opts.timeoutMs ?? timeout;
    if (!Number.isFinite(value) || value <= 0) return Promise.reject(new Error("timeoutMs must be a positive finite number"));
    return waitForView(page, view.route, value);
  }};
  return {tools, views};
}
