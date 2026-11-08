// Keeps a page's live regions current. A page that shows live state names
// its stream in a data-live-stream attribute; each region marks itself with
// hx-trigger="live-doors from:body" and reloads when the stream says what
// it shows of doors and enrollments has changed. Navigating to a page
// without a stream closes it.
(() => {
  if (window.doorcodeLive) {
    window.doorcodeLive();
    return;
  }
  let source = null;
  let url = null;
  const connect = () => {
    const marker = document.querySelector("[data-live-stream]");
    const wanted = marker ? marker.dataset.liveStream : null;
    if (wanted === url) {
      return;
    }
    if (source) {
      source.close();
    }
    source = null;
    url = wanted;
    if (wanted) {
      source = new EventSource(wanted);
      source.addEventListener("doors", () => htmx.trigger(document.body, "live-doors"));
    }
  };
  window.doorcodeLive = connect;
  document.addEventListener("htmx:afterSettle", connect);
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", connect);
  } else {
    connect();
  }
})();
