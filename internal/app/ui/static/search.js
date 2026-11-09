// Narrowing as you type, in two kinds of list.
//
// A search input names the element holding a list's rows in data-filter,
// and the message to show when none is left in data-filter-none; each row
// marked data-filter-row stays shown while the text of its element marked
// data-filter-text, or its own, holds what was typed, ignoring case and
// runs of spaces. Without this script the list shows every row, and
// submitting the search form narrows it on the server.
//
// A select marked data-combobox becomes a text input that narrows its
// options as you type, in a list below it: arrow keys move through what is
// left, Enter or a click chooses, Escape puts back what was chosen. The
// select stays in the form, hidden, and is what submits. A fieldset of
// checkboxes marked data-combobox gets a search input above it that
// narrows its checkboxes as above. Without this script both stay as they
// are.
//
// A text input marked data-user-search asks the address it names for the
// users matching what is typed (at most a page of them, as JSON: users,
// each with id, name, and email, and total) and lists them below it as a
// combobox does. Choosing one puts their id in the hidden input of the
// form named in data-user-field and their name in the element marked
// data-user-name inside the chip whose id is in data-user-chip, shows the
// chip, and hides the input; the chip's × clears the choice and brings the
// input back. Without this script the input stays a text box whose value
// the server matches as an email or user id, and the chip's × is a link.
//
// A fieldset of checkboxes marked data-picker, whose labels hold each
// choice's name in data-choice-name and detail in data-choice-detail,
// shows the checked ones as chips in the list whose id data-picker names,
// each with a × that unchecks it, and gets a search input above that list
// offering at most ten unchecked choices whose name or detail holds what
// is typed; choosing one checks it. The note whose id is the list's and
// "-none" shows while nothing is checked, made from the list's
// data-picker-none when the page has none. Without this script the chips
// are plain and the checkboxes are what a person changes.
(() => {
  if (window.doorcodeSearch) {
    return;
  }
  window.doorcodeSearch = true;
  const words = (text) => text.toLowerCase().replace(/\s+/g, " ").trim();

  function narrow(input) {
    const list = document.getElementById(input.dataset.filter);
    if (!list) {
      return;
    }
    const wanted = words(input.value);
    let shown = 0;
    for (const row of list.querySelectorAll("[data-filter-row]")) {
      const text = row.querySelector("[data-filter-text]") || row;
      const keep = words(text.textContent).includes(wanted);
      row.hidden = !keep;
      if (keep) {
        shown++;
      }
    }
    const none = document.getElementById(input.dataset.filterNone);
    if (none) {
      none.hidden = shown > 0 || list.querySelector("[data-filter-row]") === null;
    }
  }

  document.addEventListener("input", (e) => {
    if (e.target.matches && e.target.matches("[data-filter]")) {
      narrow(e.target);
    }
  });

  let made = 0;

  const element = (tag, props) => Object.assign(document.createElement(tag), props);

  const none = () => element("p", { className: "combobox-none", hidden: true, textContent: "Nothing matches what you typed." });

  function selectBox(select) {
    const id = select.id || `combobox-${++made}`;
    const list = element("ul", { id: `${id}-options`, className: "combobox-list", hidden: true });
    list.setAttribute("role", "listbox");
    const input = element("input", { type: "text", id, className: select.className, autocomplete: "off", spellcheck: false });
    input.setAttribute("role", "combobox");
    input.setAttribute("aria-autocomplete", "list");
    input.setAttribute("aria-expanded", "false");
    input.setAttribute("aria-controls", list.id);
    for (const name of ["aria-invalid", "aria-describedby"]) {
      if (select.hasAttribute(name)) {
        input.setAttribute(name, select.getAttribute(name));
      }
    }
    select.id = `${id}-select`;
    select.hidden = true;
    select.tabIndex = -1;
    const empty = none();
    const box = element("div", { className: "combobox" });
    select.replaceWith(box);
    box.append(select, input, list, empty);

    const items = [...select.options].map((option, i) => {
      const item = element("li", { id: `${id}-option-${i}`, className: "combobox-option", textContent: option.text });
      item.setAttribute("role", "option");
      item.addEventListener("mousedown", (e) => {
        e.preventDefault();
        choose(i);
      });
      list.append(item);
      return item;
    });
    let active = -1;

    const shown = () => items.filter((item) => !item.hidden);
    const chosen = () => select.options[select.selectedIndex];
    const placeholder = () => {
      const first = select.options[0];
      return first && first.value === "" ? first.text : "";
    };
    const restore = () => {
      const option = chosen();
      input.value = option && option.value !== "" ? option.text : "";
      input.placeholder = placeholder();
    };
    const mark = (item) => {
      items.forEach((other) => other.setAttribute("aria-selected", String(other === item)));
      active = items.indexOf(item);
      if (item) {
        input.setAttribute("aria-activedescendant", item.id);
        item.scrollIntoView({ block: "nearest" });
      } else {
        input.removeAttribute("aria-activedescendant");
      }
    };
    const open = (wanted) => {
      for (const item of items) {
        item.hidden = !words(item.textContent).includes(wanted);
      }
      const left = shown();
      list.hidden = left.length === 0;
      empty.hidden = left.length > 0;
      input.setAttribute("aria-expanded", String(!list.hidden));
      mark(left.includes(items[select.selectedIndex]) ? items[select.selectedIndex] : left[0]);
    };
    const close = () => {
      list.hidden = true;
      empty.hidden = true;
      input.setAttribute("aria-expanded", "false");
      mark(null);
    };
    function choose(i) {
      if (select.selectedIndex !== i) {
        select.selectedIndex = i;
        select.dispatchEvent(new Event("change", { bubbles: true }));
      }
      restore();
      close();
    }
    const step = (by) => {
      const left = shown();
      if (list.hidden) {
        open("");
        return;
      }
      const at = left.indexOf(items[active]);
      mark(left[Math.min(Math.max(at + by, 0), left.length - 1)]);
    };

    input.addEventListener("input", () => open(words(input.value)));
    input.addEventListener("focus", () => input.select());
    input.addEventListener("click", () => list.hidden && open(""));
    input.addEventListener("blur", () => {
      restore();
      close();
    });
    input.addEventListener("keydown", (e) => {
      const keys = {
        ArrowDown: () => step(1),
        ArrowUp: () => step(-1),
        Enter: () => !list.hidden && active >= 0 && choose(active),
        Escape: () => {
          restore();
          close();
        },
      };
      if (keys[e.key] && (e.key !== "Enter" || !list.hidden)) {
        e.preventDefault();
        keys[e.key]();
      }
    });
    restore();
  }

  function checkboxes(fieldset) {
    const legend = fieldset.querySelector("legend");
    const rows = [...fieldset.querySelectorAll("li")].filter((row) => row.querySelector("input"));
    if (rows.length === 0) {
      return;
    }
    const list = rows[0].parentElement;
    list.id ||= `combobox-${++made}`;
    rows.forEach((row) => row.setAttribute("data-filter-row", ""));
    const label = `Search ${legend ? legend.textContent.trim().toLowerCase() : "choices"}`;
    const input = element("input", { type: "search", placeholder: label, autocomplete: "off", spellcheck: false, className: "combobox-search" });
    input.setAttribute("aria-label", label);
    const empty = none();
    empty.id = `${list.id}-none`;
    input.dataset.filter = list.id;
    input.dataset.filterNone = empty.id;
    input.addEventListener("keydown", (e) => e.key === "Enter" && e.preventDefault());
    list.before(input);
    list.after(empty);
  }

  function userSearch(input) {
    const field = input.form.elements[input.dataset.userField];
    const chip = document.getElementById(input.dataset.userChip);
    const name = chip.querySelector("[data-user-name]");
    const list = element("ul", { id: `${input.id}-options`, className: "combobox-list", hidden: true });
    list.setAttribute("role", "listbox");
    const note = none();
    const box = element("div", { className: "combobox" });
    input.replaceWith(box);
    box.append(input, list, note);
    input.placeholder = "Search by name, email, or user id";
    input.setAttribute("role", "combobox");
    input.setAttribute("aria-autocomplete", "list");
    input.setAttribute("aria-expanded", "false");
    input.setAttribute("aria-controls", list.id);
    let found = [];
    let active = -1;
    let asking = null;
    let wait = 0;

    const mark = (i) => {
      active = i;
      [...list.children].forEach((item, at) => item.setAttribute("aria-selected", String(at === i)));
      if (i >= 0) {
        input.setAttribute("aria-activedescendant", list.children[i].id);
        list.children[i].scrollIntoView({ block: "nearest" });
      } else {
        input.removeAttribute("aria-activedescendant");
      }
    };
    const close = () => {
      if (asking) {
        asking.abort();
      }
      clearTimeout(wait);
      list.hidden = true;
      note.hidden = true;
      input.setAttribute("aria-expanded", "false");
      mark(-1);
    };
    const show = (chosen) => {
      field.value = chosen ? chosen.id : "";
      name.textContent = chosen ? chosen.name : "";
      chip.hidden = !chosen;
      box.hidden = Boolean(chosen);
      input.value = "";
      close();
    };
    const option = (user, i) => {
      const item = element("li", { id: `${input.id}-option-${i}`, className: "combobox-option", textContent: user.name });
      item.setAttribute("role", "option");
      item.append(element("span", { className: "combobox-detail", textContent: user.email }));
      item.addEventListener("mousedown", (e) => {
        e.preventDefault();
        show(user);
      });
      return item;
    };
    const answered = (answer) => {
      found = answer.users;
      list.replaceChildren(...found.map(option));
      const more = answer.total - found.length;
      list.hidden = found.length === 0;
      note.hidden = found.length > 0 && more === 0;
      note.textContent = found.length === 0 ? "Nobody matches what you typed." : `${more} more match; keep typing to narrow them.`;
      input.setAttribute("aria-expanded", String(!list.hidden));
      mark(found.length > 0 ? 0 : -1);
    };
    const ask = () => {
      if (asking) {
        asking.abort();
      }
      asking = new AbortController();
      const url = `${input.dataset.userSearch}?q=${encodeURIComponent(input.value.trim())}`;
      fetch(url, { headers: { Accept: "application/json" }, signal: asking.signal })
        .then((response) => (response.ok ? response.json() : Promise.reject(response.status)))
        .then(answered)
        .catch(() => {});
    };
    const later = () => {
      clearTimeout(wait);
      wait = setTimeout(ask, 150);
    };

    input.addEventListener("input", later);
    input.addEventListener("focus", later);
    input.addEventListener("blur", close);
    input.addEventListener("keydown", (e) => {
      const keys = {
        ArrowDown: () => (list.hidden ? ask() : mark(Math.min(active + 1, found.length - 1))),
        ArrowUp: () => mark(Math.max(active - 1, 0)),
        Enter: () => show(found[active]),
        Escape: close,
      };
      const open = !list.hidden && active >= 0;
      if (keys[e.key] && (open || e.key === "ArrowDown")) {
        e.preventDefault();
        keys[e.key]();
      }
    });
    chip.querySelector("a").addEventListener("click", (e) => {
      e.preventDefault();
      show(null);
      input.focus();
    });
    box.hidden = !chip.hidden;
  }

  const pickerCap = 10;

  function picker(fieldset) {
    const chosen = document.getElementById(fieldset.dataset.picker);
    const choices = [...fieldset.querySelectorAll("input[type=checkbox]")].map((box) => {
      const label = box.closest("label");
      const part = (name) => (label.querySelector(`[data-choice-${name}]`) || { textContent: "" }).textContent.trim();
      return { box, name: part("name"), detail: part("detail") };
    });
    if (!chosen || choices.length === 0) {
      return;
    }
    let empty = document.getElementById(`${chosen.id}-none`);
    if (!empty) {
      empty = element("p", { id: `${chosen.id}-none`, className: "text-sm text-zinc-500 dark:text-zinc-400", textContent: chosen.dataset.pickerNone });
      empty.setAttribute("role", "note");
      chosen.after(empty);
    }
    const id = `${chosen.id}-search`;
    const things = fieldset.querySelector("legend").textContent.trim().toLowerCase();
    const label = `Add ${things}`;
    const input = element("input", { type: "search", id, placeholder: `Search ${things} to add`, autocomplete: "off", spellcheck: false, className: "combobox-search" });
    input.setAttribute("aria-label", label);
    input.setAttribute("role", "combobox");
    input.setAttribute("aria-autocomplete", "list");
    input.setAttribute("aria-expanded", "false");
    const list = element("ul", { id: `${id}-options`, className: "combobox-list", hidden: true });
    list.setAttribute("role", "listbox");
    input.setAttribute("aria-controls", list.id);
    const note = none();
    note.className = "combobox-none mb-3";
    const box = element("div", { className: "combobox" });
    box.append(input, list, note);
    chosen.before(box);
    let found = [];
    let active = -1;

    const chip = (choice) => {
      const item = element("li", { className: "filter-chip" });
      item.append(element("span", { className: "truncate", textContent: choice.name }));
      if (choice.detail) {
        item.append(element("span", { className: "truncate font-normal opacity-75", textContent: choice.detail }));
      }
      const remove = element("button", { type: "button", className: "filter-chip-clear", textContent: "×" });
      remove.setAttribute("aria-label", `Remove ${choice.name}`);
      remove.addEventListener("click", () => {
        choice.box.checked = false;
        render();
        input.focus();
      });
      item.append(remove);
      return item;
    };
    function render() {
      const checked = choices.filter((choice) => choice.box.checked);
      chosen.replaceChildren(...checked.map(chip));
      empty.hidden = checked.length > 0;
    }
    const mark = (i) => {
      active = i;
      [...list.children].forEach((item, at) => item.setAttribute("aria-selected", String(at === i)));
      if (i >= 0) {
        input.setAttribute("aria-activedescendant", list.children[i].id);
        list.children[i].scrollIntoView({ block: "nearest" });
      } else {
        input.removeAttribute("aria-activedescendant");
      }
    };
    const close = () => {
      list.hidden = true;
      note.hidden = true;
      input.setAttribute("aria-expanded", "false");
      mark(-1);
    };
    const add = (choice) => {
      choice.box.checked = true;
      render();
      input.value = "";
      close();
    };
    const option = (choice, i) => {
      const item = element("li", { id: `${id}-option-${i}`, className: "combobox-option", textContent: choice.name });
      item.setAttribute("role", "option");
      if (choice.detail) {
        item.append(element("span", { className: "combobox-detail", textContent: choice.detail }));
      }
      item.addEventListener("mousedown", (e) => {
        e.preventDefault();
        add(choice);
      });
      return item;
    };
    const open = () => {
      const wanted = words(input.value);
      const matching = choices.filter((choice) => !choice.box.checked && words(`${choice.name} ${choice.detail}`).includes(wanted));
      found = matching.slice(0, pickerCap);
      list.replaceChildren(...found.map(option));
      const more = matching.length - found.length;
      list.hidden = found.length === 0;
      note.hidden = found.length > 0 && more === 0;
      note.textContent = found.length === 0 ? "Nothing left matches what you typed." : `${more} more match; keep typing to narrow them.`;
      input.setAttribute("aria-expanded", String(!list.hidden));
      mark(found.length > 0 ? 0 : -1);
    };

    input.addEventListener("input", open);
    input.addEventListener("focus", open);
    input.addEventListener("blur", close);
    input.addEventListener("keydown", (e) => {
      const keys = {
        ArrowDown: () => (list.hidden ? open() : mark(Math.min(active + 1, found.length - 1))),
        ArrowUp: () => mark(Math.max(active - 1, 0)),
        Enter: () => add(found[active]),
        Escape: close,
      };
      if (e.key === "Enter") {
        e.preventDefault();
      }
      if (keys[e.key] && ((!list.hidden && active >= 0) || e.key === "ArrowDown")) {
        e.preventDefault();
        keys[e.key]();
      }
    });
    fieldset.addEventListener("change", render);
    render();
  }

  function enhance(root) {
    for (const el of root.querySelectorAll("[data-combobox]")) {
      if (el.dataset.combobox === "done") {
        continue;
      }
      el.dataset.combobox = "done";
      (el.tagName === "SELECT" ? selectBox : checkboxes)(el);
    }
    for (const el of root.querySelectorAll("[data-picker]")) {
      if (el.dataset.pickerDone) {
        continue;
      }
      el.dataset.pickerDone = "true";
      picker(el);
    }
    for (const el of root.querySelectorAll("[data-user-search]")) {
      if (el.dataset.userSearchDone) {
        continue;
      }
      el.dataset.userSearchDone = "true";
      userSearch(el);
    }
  }

  enhance(document);
  document.addEventListener("htmx:load", (e) => enhance(e.detail.elt));
})();
