// Duration units matching Go fmtDurL: [hour, minute, second].
import { durationUnits } from '/static/js/app-time-format.js';

// Same ladder as Go fmtDurL: "0m" / "<1m" / "Xm" / "Xh" / "Xh Ym".
// The user's duration format (settings), mirrored from Go fmtDurF.
function fmtDurJS(seconds) {
  const [hu, mu] = durationUnits();
  const sec = Math.max(0, seconds);
  const f = document.documentElement.dataset.durfmt;
  if (f === 'decimal') {
    const h = Math.floor((sec * 100 + 1800) / 3600); // hundredths, half up
    const ru = document.documentElement.lang === 'ru';
    return Math.floor(h / 100) + (ru ? ',' : '.') + String(h % 100).padStart(2, '0') + (ru ? '\u00a0ч' : ' h');
  }
  if (f === 'clock') return Math.floor(sec / 3600) + ':' + String(Math.floor(sec / 60) % 60).padStart(2, '0');
  if (sec <= 0) return '0' + mu;
  if (sec < 60) return '<1' + mu;
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (h > 0 && m > 0) return h + hu + ' ' + m + mu;
  if (h > 0) return h + hu;
  return m + mu;
}

document.addEventListener('alpine:init', () => {
  // Live-ticking session row. Updates once per second without any
  // server round-trip. The anchor is the LAST RESUME (not the session
  // start): tracked = accumulated + (now - last resume), exactly as
  // model.Session.DurationSeconds. Anchoring on the start counted every
  // pause again after a resume.
  Alpine.data('liveDuration', (anchorISO, accumulated, paused) => ({
    start: new Date(anchorISO),
    accumulated: Number(accumulated) || 0,
    paused: paused === true || paused === 'true' || paused === 1,
    now: Date.now(),
    init() {
      this.interval = setInterval(() => { this.now = Date.now(); }, 1000);
    },
    destroy() { clearInterval(this.interval); },
    get seconds() {
      if (this.paused) return this.accumulated;
      return this.accumulated + Math.floor((this.now - this.start.getTime()) / 1000);
    },
    // Stopwatch reading for the running list: "1:02:05", ticks visibly.
    get clock() {
      const s = Math.max(0, this.seconds);
      const p2 = (n) => String(n).padStart(2, '0');
      return Math.floor(s / 3600) + ':' + p2(Math.floor(s / 60) % 60) + ':' + p2(s % 60);
    },
    get formatted() { return fmtDurJS(this.seconds); }
  }));

  // «учтено» under the ledger: the render-time total plus one second per
  // running timer, so the sum moves with the rows above it. Re-seeded by
  // the server on every timer change and the 30 s poll.
  Alpine.data('liveTotal', (base, running) => ({
    t0: Date.now(),
    now: Date.now(),
    init() { if (Number(running) > 0) this.interval = setInterval(() => { this.now = Date.now(); }, 1000); },
    destroy() { clearInterval(this.interval); },
    get formatted() {
      return fmtDurJS((Number(base) || 0) + Number(running) * Math.floor((this.now - this.t0) / 1000));
    }
  }));
});
