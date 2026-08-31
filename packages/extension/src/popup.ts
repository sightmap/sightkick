/**
 * Minimal popup: list bundled corpora, enable/disable them, inject into the
 * active tab, and show the tools the page currently exposes. This is a thin
 * visibility surface over the background injector; the full consent /
 * provenance / action-log manager is a separate slice (yak-518a).
 */
interface CorpusMeta {
  id: string;
  name: string;
  description: string;
  match: string[];
  source: string;
  version: string;
}
interface State {
  corpora: CorpusMeta[];
  enabled: Record<string, boolean>;
  tab: { id: number; url: string } | null;
  tools: string[] | null;
}

const send = <T>(msg: unknown): Promise<T> => chrome.runtime.sendMessage(msg) as Promise<T>;
const $ = <T extends HTMLElement>(sel: string) => document.querySelector(sel) as T;

function render(state: State): void {
  $("#site").textContent = state.tab?.url ?? "(no active tab)";

  const list = $("#corpora");
  list.innerHTML = "";
  for (const c of state.corpora) {
    const li = document.createElement("li");
    const on = !!state.enabled[c.id];
    li.innerHTML =
      `<label><input type="checkbox" ${on ? "checked" : ""} data-id="${c.id}" />` +
      `<span class="name">${c.name}</span></label>` +
      `<div class="meta">${c.description}</div>` +
      `<div class="meta">${c.source} · v${c.version}</div>`;
    list.appendChild(li);
  }
  list.querySelectorAll<HTMLInputElement>("input[type=checkbox]").forEach((cb) =>
    cb.addEventListener("change", async () => {
      await send({ type: "toggle", id: cb.dataset.id, on: cb.checked });
      refresh();
    }),
  );

  const tools = $("#tools");
  tools.innerHTML = state.tools?.length
    ? `Tools on page: ${state.tools.map((t) => `<code>${t}</code>`).join(" ")}`
    : `<span class="none">No sightkick tools on this page yet.</span>`;
}

async function refresh(): Promise<void> {
  render(await send<State>({ type: "getState" }));
}

$("#inject").addEventListener("click", async () => {
  const res = await send<{ ok: boolean; error?: string; how?: string; tools?: string[] }>({ type: "injectNow" });
  if (!res.ok) $("#tools").innerHTML = `<span class="none">${res.error}</span>`;
  refresh();
});
$("#refresh").addEventListener("click", refresh);

refresh();
