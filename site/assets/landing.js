// The landing's live ledger: a small copy of the app's «Идут сейчас»
// (pause, «Только эта», stop) whose minutes flow into the week timesheet
// and the invoice draft below, with the app's money rule: hours rounded
// to 0.01 h (36 s), amount = hours × rate per line.
(function () {
  const RATE = 350000; // 3 500,00 ₽ in kopecks
  const rows = [...document.querySelectorAll("#ledger .row")];
  if (!rows.length) return;
  const state = {};
  rows.forEach((r) => (state[r.dataset.id] = { sec: +r.dataset.sec, run: true, stopped: false }));

  const pad = (n) => String(n).padStart(2, "0");
  const clock = (s) => `${Math.floor(s / 3600)}:${pad(Math.floor(s / 60) % 60)}:${pad(s % 60)}`;
  const dur = (s) => {
    const m = Math.round(s / 60), h = Math.floor(m / 60), r = m % 60;
    return h ? (r ? `${h} ч ${r} мин` : `${h} ч`) : `${r} мин`;
  };
  const hundredths = (s) => Math.round(s / 36); // 0.01 h = 36 s, half up
  const hours = (h) => (h / 100).toFixed(2).replace(".", ",");
  const money = (k) => {
    const [r, c] = (k / 100).toFixed(2).split(".");
    return r.replace(/\B(?=(\d{3})+(?!\d))/g, " ") + "," + c + " ₽";
  };

  // Today's column in the week (Mon..Fri; a weekend lands on Friday).
  const dow = (new Date().getDay() + 6) % 7;
  const todayCol = Math.min(dow, 4) + 1; // cell index after the name
  const week = document.getElementById("week");
  const head = week?.querySelectorAll("thead th")[todayCol];
  if (head) { head.classList.add("is-today"); head.insertAdjacentHTML("beforeend", "<small>сегодня</small>"); }
  // On a phone only the name, today and the total stay visible.
  week?.querySelectorAll("tr").forEach((tr) => {
    [...tr.children].forEach((cell, i) => { if (i >= 1 && i <= 5 && i !== todayCol) cell.classList.add("off"); });
  });
  const NIL = document.querySelector("#week .icon.nil")?.outerHTML || "";
  const cellMin = (td) => (/^\d+$/.test(td.textContent.trim()) ? +td.textContent.trim() : 0);
  const base = {}; // each row's seconds outside the live cell
  week?.querySelectorAll("tbody tr[data-row]").forEach((tr) => {
    const id = tr.dataset.row, tds = tr.querySelectorAll("td");
    let sum = 0;
    for (let i = 1; i <= 5; i++) if (i !== todayCol || !state[id]) sum += cellMin(tds[i]);
    base[id] = sum * 60;
    if (state[id]) tds[todayCol].classList.add("live");
  });

  function render() {
    let total = 0, running = 0, paused = 0;
    rows.forEach((r) => {
      const s = state[r.dataset.id];
      r.querySelector(".clock").textContent = clock(s.sec);
      r.classList.toggle("paused", !s.run && !s.stopped);
      r.classList.toggle("stopped", s.stopped);
      r.querySelector(".st").textContent = s.stopped ? "Остановлена" : s.run ? "Активна" : "Пауза";
      const t = r.querySelector('[data-act="toggle"]');
      t.querySelector("span").textContent = s.run ? "Пауза" : "Продолжить";
      t.querySelector("use").setAttribute("href", t.querySelector("use").getAttribute("href").replace(/#i-\w+/, s.run ? "#i-pause" : "#i-play"));
      total += s.sec;
      if (!s.stopped) s.run ? running++ : paused++;
    });
    document.getElementById("t-total").textContent = clock(total);
    document.getElementById("t-running").textContent = running;
    document.getElementById("t-paused").textContent = paused;

    // Week: whole minutes everywhere (today's live cell rounds to the
    // nearest minute), so rows, columns and the week total add up exactly.
    const colMin = [0, 0, 0, 0, 0, 0];
    let weekMin = 0;
    week?.querySelectorAll("tbody tr[data-row]").forEach((tr) => {
      const id = tr.dataset.row, tds = tr.querySelectorAll("td");
      if (state[id]) tds[todayCol].textContent = Math.round(state[id].sec / 60);
      let rowMin = 0;
      for (let i = 1; i <= 5; i++) { const m = cellMin(tds[i]); colMin[i] += m; rowMin += m; }
      weekMin += rowMin;
      tr.querySelector("[data-sum]").textContent = dur(rowMin * 60);
    });
    const foot = week?.querySelectorAll("tfoot td");
    if (foot) {
      for (let i = 1; i <= 5; i++) foot[i].innerHTML = colMin[i] ? String(colMin[i]) : NIL;
      document.getElementById("week-total").textContent = dur(weekMin * 60);
    }

    // Invoice draft: one line per activity, priced from rounded hours.
    let invH = 0, invK = 0;
    document.querySelectorAll("#invoice tr[data-line]").forEach((tr) => {
      const id = tr.dataset.line;
      const sec = base[id] + (state[id] ? state[id].sec : 0);
      const h = hundredths(sec), k = Math.round((h * RATE) / 100);
      const tds = tr.querySelectorAll("td");
      tds[1].textContent = hours(h);
      tds[2].textContent = money(k);
      invH += h; invK += k;
    });
    document.getElementById("inv-h").textContent = hours(invH);
    document.getElementById("inv-sum").textContent = money(invK);
  }

  document.getElementById("ledger").addEventListener("click", (e) => {
    const b = e.target.closest("button");
    if (!b) return;
    if (b.dataset.all === "pause") {
      Object.values(state).forEach((s) => (s.run = false));
    } else {
      const id = b.closest(".row").dataset.id, s = state[id];
      if (b.dataset.act === "toggle") s.run = !s.run;
      if (b.dataset.act === "stop") { s.run = false; s.stopped = true; }
      if (b.dataset.act === "focus") {
        Object.entries(state).forEach(([k, o]) => { if (!o.stopped) o.run = k === id; });
      }
    }
    render();
  });

  render();
  setInterval(() => {
    Object.values(state).forEach((s) => { if (s.run) s.sec++; });
    render();
  }, 1000);
})();
