/**
 * Minimal popup: list corpora (bundled + local), enable/disable, add/remove
 * local corpora, inject into the active tab, and show the tools the page
 * currently exposes. This is a thin surface over the background injector + the
 * corpus seam; the full consent / provenance / action-log manager is a separate
 * slice (yak-518a).
 */
interface CorpusMeta {
  id: string;
  name: string;
  description: string;
  match: string[];
  source: string;
  version: string;
  origin: "bundled" | "local";
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
    const on = state.enabled[c.id] ?? false;
    const del = c.origin === "local" ? `<button class="del" data-del="${c.id}" title="remove">✕</button>` : "";
    li.innerHTML =
      `${del}<label><input type="checkbox" ${on ? "checked" : ""} data-id="${c.id}" />` +
      `<span class="name">${c.name}</span>` +
      `<span class="badge ${c.origin}">${c.origin}</span></label>` +
      `<div class="meta">${c.description}</div>` +
      `<div class="meta">${c.match.join(", ")}</div>`;
    list.appendChild(li);
  }
  list.querySelectorAll<HTMLInputElement>("input[type=checkbox]").forEach((cb) =>
    cb.addEventListener("change", async () => {
      await send({ type: "toggle", id: cb.dataset.id, on: cb.checked });
      refresh();
    }),
  );
  list.querySelectorAll<HTMLButtonElement>("button[data-del]").forEach((b) =>
    b.addEventListener("click", async () => {
      await send({ type: "removeCorpus", id: b.dataset.del });
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
  const res = await send<{ ok: boolean; error?: string }>({ type: "injectNow" });
  if (!res.ok) $("#tools").innerHTML = `<span class="none">${res.error}</span>`;
  refresh();
});
$("#refresh").addEventListener("click", refresh);

$("#add-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const err = $("#add-err");
  err.textContent = "";
  let ir: unknown;
  try {
    ir = JSON.parse($<HTMLTextAreaElement>("#c-ir").value);
  } catch {
    err.textContent = "IR is not valid JSON";
    return;
  }
  const res = await send<{ ok: boolean; error?: string }>({
    type: "addCorpus",
    corpus: {
      name: $<HTMLInputElement>("#c-name").value,
      match: $<HTMLInputElement>("#c-match").value.split(",").map((s) => s.trim()).filter(Boolean),
      ir,
    },
  });
  if (!res.ok) {
    err.textContent = res.error ?? "failed to add";
    return;
  }
  $<HTMLFormElement>("#add-form").reset();
  refresh();
});

refresh();
