// Narrows a list as you type. A search input names the element holding the
// list's rows in data-filter, and the message to show when none is left in
// data-filter-none; each row marked data-filter-row stays shown while the
// text of its element marked data-filter-text holds what was typed,
// ignoring case and runs of spaces. Without this
// script the list shows every row, and submitting the search form narrows it
// on the server.
(() => {
  if (window.doorcodeFilter) {
    return;
  }
  window.doorcodeFilter = true;
  const words = (text) => text.toLowerCase().replace(/\s+/g, " ").trim();
  document.addEventListener("input", (e) => {
    const input = e.target;
    if (!input.matches || !input.matches("[data-filter]")) {
      return;
    }
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
  });
})();
