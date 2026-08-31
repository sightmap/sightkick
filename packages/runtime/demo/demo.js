// Self-contained demo entry: mounts the todo fixture, boots the sightkick
// runtime in standalone mode (which installs a polyfilled document.modelContext
// and registers the IR tools), and drives them through a real WebMCP client
// (getTools / executeTool) — the same surface a browser agent would use.
// Import modules directly (not ../src/index.js) to avoid the entry auto-boot.
import { boot } from "../src/boot.js";
import { createClient } from "../src/client.js";
import { mountTodo } from "./todo-app.js";
import ir from "../../../generator/internal/gen/testdata/todo.ir.json";

mountTodo(document.querySelector("#app"));

const api = boot(ir);
window.__sightkick = api;
window.__sightkick_ir = ir;

// A WebMCP consumer, exactly like a built-in browser agent would use.
const client = createClient();

const out = document.querySelector("#out");
const show = async (label, promise) => {
  out.textContent = `${label} …`;
  try {
    const env = await promise;
    out.textContent = `${label} →\n${Array.isArray(env?.content) ? env.content.map((c) => c.text).join("\n") : JSON.stringify(env, null, 2)}`;
  } catch (err) {
    out.textContent = `${label} ✗ ${err}`;
  }
};

document.querySelector("#add").addEventListener("click", () => {
  const text = document.querySelector("#add-text").value || "a new task";
  show(`executeTool add_todo({text:"${text}"})`, client.callTool("add_todo", { text }));
});
document.querySelector("#list").addEventListener("click", () => {
  show("executeTool list_todos()", client.callTool("list_todos"));
});
document.querySelector("#filter").addEventListener("click", () => {
  show('executeTool set_filter({filter:"Completed"})', client.callTool("set_filter", { filter: "Completed" }));
});
document.querySelector("#clear").addEventListener("click", () => {
  show("executeTool clear_completed()", client.callTool("clear_completed"));
});

console.log(
  `[sightkick demo] ${api.polyfilled ? "polyfilled" : "native"} document.modelContext ready. Try:\n` +
    "  (await document.modelContext.getTools()).map(t => t.name)\n" +
    "  await __sightkick.call('add_todo', { text: 'buy milk' })",
);
