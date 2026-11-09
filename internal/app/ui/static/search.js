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

  function enhance(root) {
    for (const el of root.querySelectorAll("[data-combobox]")) {
      if (el.dataset.combobox === "done") {
        continue;
      }
      el.dataset.combobox = "done";
      (el.tagName === "SELECT" ? selectBox : checkboxes)(el);
    }
  }

  enhance(document);
  document.addEventListener("htmx:load", (e) => enhance(e.detail.elt));
})();
