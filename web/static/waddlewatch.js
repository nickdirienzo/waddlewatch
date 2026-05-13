(function () {
  // Map of preset label to lookback duration in ms.
  const RANGES = {
    "5m": 5 * 60_000,
    "15m": 15 * 60_000,
    "1h": 60 * 60_000,
    "24h": 24 * 60 * 60_000,
    "7d": 7 * 24 * 60 * 60_000,
  };

  function setRange(form, key) {
    const ms = RANGES[key];
    if (!ms) return;
    const now = new Date();
    const start = new Date(now.getTime() - ms);
    const from = form.querySelector('input[name="from"]');
    const to = form.querySelector('input[name="to"]');
    if (!from || !to) return;
    from.value = start.toISOString();
    to.value = now.toISOString();
    submit(form);
  }

  // Submit a form so htmx picks it up. requestSubmit fires a real submit event,
  // which htmx's hx-trigger="submit" hook listens to.
  function submit(form) {
    if (typeof form.requestSubmit === "function") {
      form.requestSubmit();
    } else {
      form.dispatchEvent(new Event("submit", { cancelable: true, bubbles: true }));
    }
  }

  document.body.addEventListener("click", function (e) {
    const preset = e.target.closest(".preset-btn");
    if (preset) {
      e.preventDefault();
      const form = preset.closest("form");
      if (form) setRange(form, preset.dataset.range);
      return;
    }

    const link = e.target.closest(".filter-link");
    if (link) {
      e.preventDefault();
      // The time-range filter form is always class="filters" on each page.
      const form = document.querySelector("form.filters");
      if (!form) return;
      const fieldName = link.dataset.field;
      const value = link.dataset.value || "";
      const field = form.elements.namedItem(fieldName);
      if (!field) return;
      field.value = value;
      submit(form);
    }
  });
})();
