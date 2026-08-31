// A tiny, framework-free todo app whose DOM matches the sightmap corpus in
// examples/todo/.sightmap (selectors: .todo-app, .todo-input-field,
// .todo-input-add, .todo-item, .todo-item-text, .filter-bar-filter, etc.).
//
// Shared by the executor test (imported) and the manual demo page (script src),
// so both exercise the exact structure the generator compiled locators for.

export function mountTodo(root, seed = ["Write the runtime", "Test on the todo app", "Ship it"]) {
  root.innerHTML = `
    <div class="todo-app">
      <div class="todo-input">
        <input class="todo-input-field" placeholder="What needs doing?" />
        <button class="todo-input-add" disabled>Add</button>
      </div>
      <ul class="todo-app-list"></ul>
      <div class="filter-bar">
        <span class="filter-bar-count"></span>
        <button class="filter-bar-filter active">All</button>
        <button class="filter-bar-filter">Active</button>
        <button class="filter-bar-filter">Completed</button>
        <button class="filter-bar-clear">Clear completed</button>
      </div>
    </div>`;

  const app = root.querySelector(".todo-app");
  const field = app.querySelector(".todo-input-field");
  const addBtn = app.querySelector(".todo-input-add");
  const list = app.querySelector(".todo-app-list");
  const countEl = app.querySelector(".filter-bar-count");
  const filters = Array.from(app.querySelectorAll(".filter-bar-filter"));
  const clearBtn = app.querySelector(".filter-bar-clear");

  const state = { todos: seed.map((text, i) => ({ text, done: i === 1 })), filter: "All" };

  function render() {
    list.innerHTML = "";
    for (const todo of state.todos) {
      if (state.filter === "Active" && todo.done) continue;
      if (state.filter === "Completed" && !todo.done) continue;
      const li = document.createElement("li");
      li.className = "todo-item" + (todo.done ? " is-completed" : "");
      const cb = document.createElement("input");
      cb.type = "checkbox";
      cb.checked = todo.done;
      cb.addEventListener("change", () => {
        todo.done = cb.checked;
        render();
      });
      const span = document.createElement("span");
      span.className = "todo-item-text";
      span.textContent = todo.text;
      if (todo.done) span.style.textDecoration = "line-through";
      const del = document.createElement("button");
      del.className = "todo-item-delete";
      del.setAttribute("aria-label", `Delete ${todo.text}`);
      del.textContent = "×";
      del.addEventListener("click", () => {
        state.todos = state.todos.filter((t) => t !== todo);
        render();
      });
      li.append(cb, span, del);
      list.appendChild(li);
    }
    const left = state.todos.filter((t) => !t.done).length;
    countEl.textContent = `${left} items left`;
    for (const f of filters) f.classList.toggle("active", f.textContent === state.filter);
  }

  function add() {
    const text = field.value.trim();
    if (!text) return;
    state.todos.push({ text, done: false });
    field.value = "";
    addBtn.disabled = true;
    render();
  }

  field.addEventListener("input", () => {
    addBtn.disabled = field.value.trim().length === 0;
  });
  field.addEventListener("keydown", (e) => {
    if (e.key === "Enter") add();
  });
  addBtn.addEventListener("click", add);
  for (const f of filters) {
    f.addEventListener("click", () => {
      state.filter = f.textContent;
      render();
    });
  }
  clearBtn.addEventListener("click", () => {
    state.todos = state.todos.filter((t) => !t.done);
    render();
  });

  render();
  return state;
}
