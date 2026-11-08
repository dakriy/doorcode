// Draws a schedule's week with the vendored FullCalendar. The page lists the
// week's occurrences as local times in the site timezone, without an
// offset; the calendar runs in UTC so it shows them exactly as given,
// whatever the browser's own timezone.
(function () {
  var root = document.getElementById("schedule-calendar");
  if (!root) {
    return;
  }
  var clock = { hour: "2-digit", minute: "2-digit", hour12: false };

  function draw() {
    var events = JSON.parse(root.dataset.events).map(function (o) {
      return { start: o.start, end: o.end, title: root.dataset.title };
    });
    root.textContent = "";
    new FullCalendar.Calendar(root, {
      initialView: "timeGridWeek",
      initialDate: root.dataset.week,
      now: root.dataset.today,
      timeZone: "UTC",
      firstDay: 1,
      headerToolbar: false,
      allDaySlot: false,
      height: "auto",
      expandRows: true,
      slotDuration: "01:00:00",
      slotLabelFormat: clock,
      eventTimeFormat: clock,
      dayHeaderFormat: { weekday: "short", day: "numeric", month: "short" },
      editable: false,
      selectable: false,
      events: events,
    }).render();
  }

  if (window.FullCalendar) {
    draw();
  } else {
    document.getElementById("fullcalendar-script").addEventListener("load", draw);
  }
})();
