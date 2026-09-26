// paratrack frontend helpers — registered as Alpine.data components
// and exposed as globals for templates that want lightweight inline JS.

// X-Toast is percent-encoded by the server (headers are Latin-1).
window.paratrackDecode = (v) => {
  if (!v) return v;
  try { return decodeURIComponent(v); } catch (_) { return v; }
};

// Duration units matching Go fmtDurL: [hour, minute, second].
const U = () => document.documentElement.lang === 'ru'
  ? ['\u00a0ч', '\u00a0мин', '\u00a0с'] : ['h', 'm', 's'];

document.addEventListener('alpine:init', () => {
  // Live-ticking session row. Updates once per second without any
  // server round-trip; the parent just passes start ISO, accumulated
  // seconds and a paused flag from the server-rendered payload.
  Alpine.data('liveDuration', (startISO, accumulated, paused) => ({
    start: new Date(startISO),
    accumulated: Number(accumulated) || 0,
    paused: paused === true || paused === 'true' || paused === 1,
    now: Date.now(),
    init() {
      setInterval(() => { this.now = Date.now(); }, 1000);
    },
    get seconds() {
      if (this.paused) return this.accumulated;
      return this.accumulated + Math.floor((this.now - this.start.getTime()) / 1000);
    },
    get formatted() {
      // Same ladder as Go fmtDuration: "0m" / "1m" / "Xm" / "Xh" / "Xh Ym".
      const [hu, mu] = U();
      const sec = Math.max(0, this.seconds);
      if (sec <= 0) return '0' + mu;
      if (sec < 60) return '1' + mu;
      const h = Math.floor(sec / 3600);
      const m = Math.floor((sec % 3600) / 60);
      if (h > 0 && m > 0) return h + hu + ' ' + m + mu;
      if (h > 0) return h + hu;
      return m + mu;
    }
  }));

  // Theme toggle state — bound to the data-theme attribute on <html>.
  // `mode` is real Alpine state (reactive); localStorage is only the
  // persistence side-effect. Reading localStorage straight from a
  // getter would never notify x-show after cycle().
  Alpine.data('themeToggle', () => ({
    mode: 'auto',
    init() {
      const stored = localStorage.getItem('paratrack-theme');
      this.mode = (stored === 'light' || stored === 'dark') ? stored : 'auto';
    },
    get current() { return this.mode; },
    cycle() {
      const order = ['auto', 'light', 'dark'];
      const next = order[(order.indexOf(this.mode) + 1) % order.length];
      this.mode = next;
      localStorage.setItem('paratrack-theme', next);
      applyTheme(next);
      const labels = {
        auto: document.querySelector('[data-toast-theme-auto]')?.dataset.toastThemeAuto,
        light: document.querySelector('[data-toast-theme-light]')?.dataset.toastThemeLight,
        dark: document.querySelector('[data-toast-theme-dark]')?.dataset.toastThemeDark,
      };
      window.paratrackToast(labels[next] || ('theme: ' + next), 'success');
    }
  }));

  // Graph tooltip — sits absolutely-positioned inside .card and follows
  // the mouse over graph bars. State is local; positioning reads the
  // bar's data-* attributes.
  Alpine.data('graphTooltip', () => ({
    visible: false,
    x: 0,
    y: 0,
    activity: '',
    time: '',
    duration: '',
    init() {
      // Bind handlers so @mouseenter / @mousemove can find them via $root.
      this._root = this.$root;
      this._root._showBar = (e) => this._show(e);
      this._root._moveBar = (e) => this._move(e);
    },
    _show(e) {
      const t = e.currentTarget;
      this.activity = t.dataset.activity || '';
      this.time = (t.dataset.start || '') + ' → ' + (t.dataset.end || '');
      this.duration = t.dataset.duration || '';
      this.visible = true;
      this._move(e);
    },
    _move(e) {
      const card = this._root.getBoundingClientRect();
      // Position tooltip 16px right of cursor, 8px below.
      const px = e.clientX - card.left + 16;
      const py = e.clientY - card.top + 8;
      // Clamp inside the card horizontally so it doesn't fall off-screen.
      const tt = this.$root.querySelector('.graph-tooltip');
      const tw = tt ? tt.offsetWidth : 180;
      this.x = Math.min(px, card.width - tw - 8);
      this.y = py;
    },
    hide() { this.visible = false; }
  }));

  // ECharts timeline — Alpine.data wrapper would interfere with
  // ECharts's native hover/tooltip event handling (the reactive
  // proxy somehow eats mousemove). Instead, init is done imperatively
  // from a script at the bottom of graph.html.
  Alpine.data('echartsTimeline', () => ({}));

  // Imperative init for the ECharts canvas.
  window.paratrackInitEcharts = function () {
    const wrap = document.getElementById('echart-wrap');
    const canvas = document.getElementById('echart-canvas');
    if (!wrap || !canvas) return;
    const raw = wrap.dataset.chart;
    if (!raw) return;
    let data;
    try { data = JSON.parse(raw); } catch (_) { return; }
    if (!data.hasData || typeof echarts === 'undefined') return;

    const cssVar = (n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim();
    let chart;
    const build = () => {
      const old = echarts.getInstanceByDom(canvas);
      if (old) old.dispose();
      chart = echarts.init(canvas, null, { renderer: 'canvas' });
      const sk = document.getElementById('echart-skeleton');
      if (sk) sk.remove();
      chart.setOption({
        backgroundColor: 'transparent',
        textStyle: { color: cssVar('--color-base-content') || '#1a1d23', fontFamily: 'Inter, sans-serif' },
        grid: { left: 50, right: 20, top: 20, bottom: 40, containLabel: true },
        tooltip: {
          trigger: 'axis',
          backgroundColor: cssVar('--color-base-100') || '#fff',
          borderColor: cssVar('--color-base-300') || '#e5e7eb',
          textStyle: { color: cssVar('--color-base-content') || '#1a1d23', fontSize: 12 },
          formatter: function (ps) {
            if (!ps || !ps.length) return '';
            const rows = ps.filter(p => p.value > 0).map(p =>
              '<span style="display:inline-block;width:8px;height:8px;border-radius:2px;background:' + p.color + ';margin-right:6px"></span>' +
              p.seriesName + ': <b>' + p.value + U()[1] + '</b>');
            const total = ps.reduce((a, p) => a + (p.value || 0), 0);
            return '<div style="font-weight:600;margin-bottom:4px">' + ps[0].axisValue + ':00</div>' +
              (rows.length ? rows.join('<br/>') : '<span style="opacity:.6">0m</span>') +
              '<div style="margin-top:6px;opacity:.65">Σ ' + total + 'm</div>';
          }
        },
        xAxis: {
          type: 'category',
          data: data.hours,
          axisLabel: { color: cssVar('--color-base-content') || '#6b7280', fontSize: 11, opacity: 0.55 },
          axisLine: { lineStyle: { color: cssVar('--color-base-300') || '#e5e7eb' } },
          axisTick: { show: false },
        },
        yAxis: {
          type: 'value',
          name: 'min',
          nameLocation: 'end',
          nameTextStyle: { color: cssVar('--color-base-content') || '#6b7280', fontSize: 10, opacity: 0.55, padding: [0, 0, 4, 0] },
          // Whole-minute ticks. Without minInterval ECharts happily emits
          // 0.2m / 0.4m ticks for tiny data, which clashes with the unified
          // "Xh YYm / Xm" format used everywhere else.
          minInterval: 1,
          axisLabel: {
            color: cssVar('--muted') || '#6b7280',
            fontSize: 11,
            formatter: function (v) {
              if (v >= 60) return Math.floor(v / 60) + U()[0];
              // Sub-minute: data is in minutes, so v*60 = seconds. Show
              // "Xs" so the chart axis matches the column-header units
              // on /stats (Xh YYm / Xm) for any non-zero value.
              if (v < 1 && v > 0) return Math.round(v * 60) + U()[2];
              return Math.round(v) + U()[1];
            },
          },
          splitLine: { lineStyle: { color: cssVar('--color-base-300') || '#e5e7eb', type: 'dashed' } },
        },
        series: data.series.map((s) => ({
          name: s.name,
          type: 'bar',
          stack: 'hour',
          data: s.data,
          itemStyle: { color: s.color, borderRadius: [4, 4, 0, 0], borderColor: cssVar('--color-base-100') || '#fff', borderWidth: 0.5 },
          barMaxWidth: 22,
          barCategoryGap: '28%',
          emphasis: { focus: 'series' },
        })),
        animationDuration: 400,
        animationEasing: 'cubicOut',
      });
    };
    requestAnimationFrame(build);

    // Theme reactivity.
    new MutationObserver(build).observe(document.documentElement, {
      attributes: true, attributeFilter: ['data-theme'],
    });
    // Resize handler.
    new ResizeObserver(() => {
      const i = echarts.getInstanceByDom(canvas);
      if (i) i.resize();
    }).observe(canvas);

    // Wire up legend chips — clicking one toggles the corresponding series
    // visibility in the chart and flips aria-pressed on the chip.
    const chipsHost = document.getElementById('legend-chips');
    if (chipsHost) {
      chipsHost.addEventListener('click', (e) => {
        const chip = e.target.closest('.legend-chip');
        if (!chip) return;
        const idx = parseInt(chip.dataset.seriesIndex, 10);
        const i = echarts.getInstanceByDom(canvas);
        if (!i) return;
        const opt = i.getOption();
        const isHidden = (opt.series[idx] && opt.series[idx].itemStyle && opt.series[idx].itemStyle.opacity === 0);
        i.setOption({
          series: data.series.map((s, j) => j === idx ? {
            ...s,
            itemStyle: { ...(s.itemStyle || {}), color: s.color, opacity: isHidden ? 1 : 0 },
          } : s),
        });
        chip.setAttribute('aria-pressed', isHidden ? 'false' : 'true');
      });
      // Keyboard activation (Enter / Space) for accessibility.
      chipsHost.addEventListener('keydown', (e) => {
        if (e.key !== 'Enter' && e.key !== ' ') return;
        const chip = e.target.closest('.legend-chip');
        if (!chip) return;
        e.preventDefault();
        chip.click();
      });
    }
  };
});

window.applyTheme = function(mode) {
  const html = document.documentElement;
  html.dataset.themeMode = mode;
  document.querySelectorAll('.theme-mode-label').forEach(el => { el.textContent = el.dataset[mode] || mode; });
  if (mode === 'light') html.dataset.theme = 'paratrack-light';
  else if (mode === 'dark') html.dataset.theme = 'paratrack-dark';
  else {
    // auto: resolve now, and keep following the OS
    const dark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    html.dataset.theme = dark ? 'paratrack-dark' : 'paratrack-light';
  }
  // Installed-app status bar follows the app theme, not only the OS.
  const bar = html.dataset.theme === 'paratrack-dark' ? '#1a1d23' : '#ffffff';
  document.querySelectorAll('meta[name="theme-color"]').forEach((m) => m.setAttribute('content', bar));
};

// Resolve theme before paint. base.html ships data-theme="paratrack-light";
// we override when the user has an explicit preference.
(function() {
  const stored = localStorage.getItem('paratrack-theme') || 'auto';
  window.applyTheme(stored);
  // follow the OS while in auto
  if (window.matchMedia) {
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
      if ((localStorage.getItem('paratrack-theme') || 'auto') === 'auto') window.applyTheme('auto');
    });
  }
})();

// Toast — renders a DaisyUI alert in #toast, auto-dismisses after 2.2s.
window.paratrackToast = function(message, kind, ms) {
  const el = document.getElementById('toast');
  if (!el) return;
  const variant = kind || 'success';
  const glyph =
    variant === 'success' ? '✓' :
    variant === 'error'   ? '✕' :
    variant === 'warning' ? '!' :
                            'ⓘ';
  // Build with textContent: messages carry user input (activity names).
  const box = document.createElement('div');
  box.className = `alert alert-${variant} shadow-lg pointer-events-auto opacity-100 transition-opacity duration-200`;
  box.setAttribute('role', 'status');
  const icon = document.createElement('span');
  icon.className = 'text-base font-bold';
  icon.textContent = glyph;
  const text = document.createElement('span');
  text.textContent = message;
  box.append(icon, text);
  el.replaceChildren(box);
  clearTimeout(window._paratrackToastTimer);
  window._paratrackToastTimer = setTimeout(() => {
    const inner = el.firstElementChild;
    if (inner) {
      inner.classList.add('opacity-0');
      setTimeout(() => { el.innerHTML = ''; }, 250);
    }
  }, ms || Math.max(2200, String(message).length * 55));
};

// paratrackResize — parses a human duration ("1h 30m") from the
// inline edit field, recomputes end_at from start_at, and PATCHes
// the same endpoint the end_at input would.
window.paratrackResize = async function(input, sessionID) {
  const row = document.getElementById('row-' + sessionID);
  if (!row) return;
  const startInput = row.querySelector('[name="start_at"]');
  const endInput = row.querySelector('[name="end_at"]');
  const noteInput = row.querySelector('[name="note"]');
  if (!startInput || !endInput || !noteInput) return;

  const startVal = startInput.value;
  const endVal = endInput.value;
  const noteVal = noteInput.value;
  const durVal = input.value.trim();

  if (!startVal || !durVal) {
    window.paratrackToast((window.paratrackT || {}).needStartDuration || 'Start and duration are required', 'error');
    return;
  }

  // Build a form and submit via fetch + htmx-style swap.
  const fd = new FormData();
  fd.set('start_at', startVal);
  fd.set('end_at', endVal); // server overrides when duration is present
  fd.set('duration', durVal);
  fd.set('note', noteVal);
  try {
    const resp = await fetch('/api/sessions/' + sessionID, {
      method: 'PATCH',
      body: new URLSearchParams([...fd.entries()]),
      headers: {
        'Content-Type': 'application/x-www-form-urlencoded',
        'X-CSRF-Token': (document.cookie.match(/(?:^|;\s*)paratrack_csrf=([^;]+)/) || [])[1] || '',
      },
    });
    if (!resp.ok) {
      const t = await resp.text();
      window.paratrackToast(t || 'resize failed', 'error');
      return;
    }
    const html = await resp.text();
    // Toast from response header.
    const toast = window.paratrackDecode(resp.headers.get('X-Toast'));
    if (toast) window.paratrackToast(toast, resp.headers.get('X-Toast-Kind') || 'success');
    // HTMX-style outer swap. htmx.process() is required: content injected
    // outside htmx.load() is inert until processed, which used to kill the
    // row's tag / pause / delete bindings after an inline duration edit.
    const tmp = document.createElement('tbody');
    tmp.innerHTML = html.trim();
    const newRow = tmp.firstElementChild;
    if (newRow) {
      row.outerHTML = newRow.outerHTML;
      const live = document.getElementById('row-' + sessionID);
      if (live && window.htmx) window.htmx.process(live);
    }
  } catch (e) {
    window.paratrackToast((window.paratrackT || {}).sendError || 'Network error', 'error');
  }
};

// CSRF — HTMX must echo the double-submit cookie on every mutating
// request. Plain forms carry a hidden input rendered server-side.
function paratrackCSRF() {
  const m = document.cookie.match(/(?:^|;\s*)paratrack_csrf=([^;]+)/);
  return m ? decodeURIComponent(m[1]) : '';
}
document.addEventListener('htmx:configRequest', (e) => {
  const t = paratrackCSRF();
  if (t) e.detail.headers['X-CSRF-Token'] = t;
});

// Keyboard shortcuts. Available on every page, ignored when typing.
(function() {
  function isTyping(target) {
    if (!target) return false;
    const tag = target.tagName;
    return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target.isContentEditable;
  }
  document.addEventListener('keydown', (e) => {
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    if (isTyping(e.target)) return;
    const key = e.key.toLowerCase();
    const here = window.location.pathname;
    if (key === 'g' && here !== '/graph')  { window.location.href = '/graph'; return; }
    if (key === 's' && here !== '/stats')  { window.location.href = '/stats'; return; }
    if (key === 'd' && here !== '/')       { window.location.href = '/'; return; }
    if (key === 't') { themeBtnClick(); return; }
    if (key === 'n' && here === '/') { document.querySelector('input[name="activity"]')?.focus(); return; }
  });
  function themeBtnClick() {
    const btn = document.querySelector('[data-theme-toggle]');
    if (btn) btn.click();
  }
})();


// ---------------------------------------------------------------------------
// Offline mode (Wave 9): banner + mutation queue flushed on reconnect.
// Queue items carry the request BODY — a replay without it is a 400 that
// silently drops the user's change. Only 2xx dequeues.
// ---------------------------------------------------------------------------
(function () {
  const KEY = "paratrack-offline-queue";

  function queue() {
    try { return JSON.parse(localStorage.getItem(KEY) || "[]"); } catch (_) { return []; }
  }
  function save(q) { localStorage.setItem(KEY, JSON.stringify(q)); }

  function bodyToObject(body) {
    if (!body) return {};
    try {
      if (typeof body === "string") return Object.fromEntries(new URLSearchParams(body));
      if (typeof URLSearchParams !== "undefined" && body instanceof URLSearchParams) {
        return Object.fromEntries(body);
      }
      if (typeof FormData !== "undefined" && body instanceof FormData) {
        const o = {};
        body.forEach((v, k) => { o[k] = v; });
        return o;
      }
    } catch (_) {}
    return {};
  }

  function banner() {
    let el = document.getElementById("offline-banner");
    if (!el) {
      el = document.createElement("div");
      el.id = "offline-banner";
      el.style.cssText =
        "position:fixed;left:50%;transform:translateX(-50%);bottom:1rem;z-index:70;" +
        "padding:.5rem 1rem;border-radius:.75rem;font:600 13px system-ui;" +
        "background:#1a1d23;color:#fff;box-shadow:0 8px 24px rgba(0,0,0,.25);display:none";
      document.body.appendChild(el);
    }
    return el;
  }

  const T = window.paratrackT || {};
  const fill = (tpl, n) => (tpl || "").replace("{n}", n);

  function render() {
    const el = banner();
    const q = queue();
    if (!navigator.onLine) {
      el.style.display = "block";
      el.textContent = q.length ? fill(T.offlinePending, q.length) : T.offline;
    } else if (q.length) {
      el.style.display = "block";
      el.textContent = fill(T.syncing, q.length);
      setTimeout(() => { if (navigator.onLine && queue().length === 0) el.style.display = "none"; }, 1200);
    } else {
      el.style.display = "none";
    }
  }

  function enqueue(item) {
    const q = queue();
    q.push(item);
    save(q);
    render();
    if (window.paratrackToast) window.paratrackToast(T.queued, "warning", 4000);
  }

  // Queue mutating HTMX requests that fire while the network is down.
  document.body.addEventListener("htmx:beforeRequest", (e) => {
    if (navigator.onLine) return;
    const el = e.detail.elt;
    if (!el) return;
    const verb = (el.getAttribute("hx-post") && "POST") ||
                 (el.getAttribute("hx-delete") && "DELETE") ||
                 (el.getAttribute("hx-patch") && "PATCH") ||
                 (el.getAttribute("hx-put") && "PUT") || "";
    if (!verb) return;
    e.preventDefault();
    const cfg = e.detail.requestConfig || {};
    const params = cfg.parameters || cfg.formData || cfg.unfilteredFormData;
    enqueue({
      verb,
      url: el.getAttribute("hx-post") || el.getAttribute("hx-delete") ||
           el.getAttribute("hx-patch") || el.getAttribute("hx-put"),
      body: bodyToObject(params),
      ts: Date.now(),
    });
  });

  // Same for the manual fetch used by paratrackResize / push / etc.
  const origFetch = window.fetch;
  window.fetch = async function (...args) {
    try {
      return await origFetch.apply(this, args);
    } catch (err) {
      const req = args[0];
      const opts = args[1] || {};
      const method = (opts.method || (req && req.method) || "GET").toUpperCase();
      if (method !== "GET" && !navigator.onLine) {
        const url = typeof req === "string" ? req : (req && req.url) || "";
        enqueue({ verb: method, url, body: bodyToObject(opts.body), ts: Date.now() });
      }
      throw err;
    }
  };

  async function flush() {
    if (!navigator.onLine) { render(); return; }
    let q = queue();
    if (!q.length) { render(); return; }
    const csrf = decodeURIComponent((document.cookie.match(/paratrack_csrf=([^;]+)/) || [])[1] || "");
    while (q.length) {
      const item = q[0];
      const body = new URLSearchParams(item.body || {});
      if (!body.has("csrf_token")) body.set("csrf_token", csrf);
      // Timer actions happen at click time, not at reconnect time.
      if (item.ts) body.set("client_ts", String(item.ts));
      let res;
      try {
        res = await origFetch(item.url, {
          method: item.verb === "DELETE" ? "DELETE" : item.verb,
          credentials: "same-origin",
          headers: {
            "X-CSRF-Token": csrf,
            "HX-Request": "true",
            "Content-Type": "application/x-www-form-urlencoded",
          },
          body,
        });
      } catch (_) {
        break; // still offline — keep the rest
      }
      // HTTP 4xx/5xx is NOT a successful replay: drop only on 2xx so the
      // change is not silently lost. Non-2xx stops the drain to avoid
      // hammering the server with a poisoned item.
      if (!res.ok) break;
      q = q.slice(1);
      save(q);
    }
    render();
    // refresh active list / page state after sync
    if (window.htmx) window.htmx.trigger(document.body, "paratrack:synced");
  }

  window.addEventListener("online", () => { render(); flush(); });
  window.addEventListener("offline", render);
  document.addEventListener("DOMContentLoaded", render);
  render();
})();


// ---------------------------------------------------------------------------
// Loading feedback (Wave 10): a thin top progress bar for every HTMX request,
// and skeleton rows in the list regions while their fragment is in flight.
// Server-rendered pages arrive complete — skeletons are only for the async
// regions where there is a real wait.
// ---------------------------------------------------------------------------
(function () {
  const REGION_SKEL = {
    '#active-list': '<div class="sk sk-row"></div><div class="sk sk-row"></div>',
    '#goals-list':  '<div class="sk sk-row"></div><div class="sk sk-row"></div>',
    '#tags-list':   '<div class="sk sk-line w-70"></div><div class="sk sk-line w-50"></div>',
  };

  function bar() {
    let el = document.getElementById('htmx-progress');
    if (!el) {
      el = document.createElement('div');
      el.id = 'htmx-progress';
      document.body.appendChild(el);
    }
    return el;
  }
  // Both the progress bar and the region skeletons are deferred: a fast
  // request (the common case on a local SQLite backend) must NOT flash
  // a skeleton over content the user is already reading.
  const GRACE = 180; // ms before loading feedback appears
  let barTimer = null;

  function showBar() {
    clearTimeout(barTimer);
    barTimer = setTimeout(() => {
      const el = bar();
      el.style.width = '15%';
      void el.offsetWidth;
      el.classList.add('on');
      el.style.width = '70%';
    }, GRACE);
  }
  function hideBar() {
    clearTimeout(barTimer);
    const el = bar();
    if (!el.classList.contains('on')) { el.style.width = '0%'; return; }
    el.style.width = '100%';
    setTimeout(() => {
      el.classList.remove('on');
      el.style.width = '0%';
    }, 160);
  }

  const saved = new WeakMap();
  const skelTimers = new WeakMap();

  document.body.addEventListener('htmx:beforeRequest', (e) => {
    showBar();
    const t = e.detail && e.detail.target;
    if (!t || !t.id) return;
    const sk = REGION_SKEL['#' + t.id];
    if (!sk) return;
    saved.set(t, t.innerHTML);
    skelTimers.set(t, setTimeout(() => {
      // still waiting after GRACE — now the skeleton is honest
      if (saved.has(t)) t.innerHTML = sk;
    }, GRACE));
  });

  document.body.addEventListener('htmx:afterRequest', (e) => {
    hideBar();
    const t = e.detail && e.detail.target;
    if (!t) return;
    const timer = skelTimers.get(t);
    if (timer) { clearTimeout(timer); skelTimers.delete(t); }
    if (saved.has(t)) {
      if (e.detail.xhr && e.detail.xhr.status >= 400) {
        t.innerHTML = saved.get(t);
      }
      saved.delete(t);
    }
  });

  document.body.addEventListener('htmx:sendError', hideBar);
  document.body.addEventListener('htmx:responseError', hideBar);
})();

// ---- PWA: install, offline, running-timer badge ----
(function () {
  const T = window.paratrackT || {};
  const standalone = matchMedia('(display-mode: standalone)').matches || navigator.standalone === true;
  const ios = /iphone|ipad|ipod/i.test(navigator.userAgent);
  let deferred = null;
  const showInstall = (on) => document.querySelectorAll('[data-install]').forEach((el) => { el.hidden = !on; });

  // Chrome / Edge / Android: keep the native prompt for our own button.
  window.addEventListener('beforeinstallprompt', (e) => { e.preventDefault(); deferred = e; showInstall(true); });
  window.addEventListener('appinstalled', () => { deferred = null; showInstall(false); });
  // iOS Safari has no prompt: the button explains Share -> Add to Home Screen.
  if (ios && !standalone) showInstall(true);
  document.addEventListener('click', async (e) => {
    if (!e.target.closest('[data-install]')) return;
    e.preventDefault();
    if (deferred) {
      deferred.prompt();
      await deferred.userChoice;
      deferred = null;
      showInstall(false);
    } else if (ios) {
      window.paratrackToast(T.iosInstall, 'info', 8000);
    }
  });

  // Offline actions are queued by the Wave 9 block above. Online but the
  // server did not answer: say so instead of failing silently.
  document.body.addEventListener('htmx:sendError', () => {
    if (navigator.onLine) window.paratrackToast(T.sendError, 'error', 5000);
  });

  // App shortcut "New timer" lands on /?focus=activity.
  if (new URLSearchParams(location.search).get('focus') === 'activity') {
    document.getElementById('activity')?.focus();
  }

  // Running timers: count on the app icon (installed PWA) and in the tab title.
  const baseTitle = document.title;
  const sync = () => {
    const list = document.getElementById('active-list');
    if (!list) return;
    const n = list.querySelectorAll('.status-pill.is-active').length;
    document.title = n ? `\u25cf ${n} \u00b7 ${baseTitle}` : baseTitle;
    if ('setAppBadge' in navigator) (n ? navigator.setAppBadge(n) : navigator.clearAppBadge()).catch(() => {});
  };
  document.body.addEventListener('htmx:afterSettle', sync);
  sync();
})();

// ---- Phone chrome: bottom sheet + haptics ----
(function () {
  document.addEventListener('click', (e) => {
    const opener = e.target.closest('[data-sheet-open]');
    if (opener) {
      const d = document.getElementById(opener.dataset.sheetOpen);
      if (d && !d.open) d.showModal();
      return;
    }
    // Tap on the dimmed backdrop closes the sheet. Backdrop clicks target
    // the dialog itself, but so do clicks on its padding: check the point.
    const d = e.target.matches && e.target.matches('dialog.sheet[open]') ? e.target : null;
    if (d && e.clientY < d.getBoundingClientRect().top) d.close();
  });

  // Swipe the sheet down to dismiss, like a native sheet.
  let sheet = null, y0 = 0, dy = 0;
  document.addEventListener('touchstart', (e) => {
    const d = e.target.closest && e.target.closest('dialog.sheet[open]');
    if (!d || d.scrollTop > 0) return;
    sheet = d; y0 = e.touches[0].clientY; dy = 0;
    d.style.transition = 'none';
  }, { passive: true });
  document.addEventListener('touchmove', (e) => {
    if (!sheet) return;
    dy = Math.max(0, e.touches[0].clientY - y0);
    sheet.style.transform = dy ? `translateY(${dy}px)` : '';
  }, { passive: true });
  document.addEventListener('touchend', () => {
    if (!sheet) return;
    const d = sheet; sheet = null;
    d.style.transition = 'transform .18s ease-out';
    if (dy > 90) {
      d.style.transform = 'translateY(100%)';
      setTimeout(() => { d.close(); d.style.transform = ''; d.style.transition = ''; }, 180);
    } else {
      d.style.transform = '';
    }
  });

  // A short tick on timer actions (Android; iOS ignores vibrate).
  document.body.addEventListener('htmx:afterRequest', (e) => {
    const path = (e.detail.requestConfig && e.detail.requestConfig.path) || '';
    if (e.detail.successful && /\/api\/(start|focus|sessions\/\d+\/(stop|pause|resume))/.test(path)) {
      if (navigator.vibrate) navigator.vibrate(12);
    }
  });
})();
