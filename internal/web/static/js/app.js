// paratrack frontend helpers — registered as Alpine.data components
// and exposed as globals for templates that want lightweight inline JS.

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
      const sec = Math.max(0, this.seconds);
      const h = Math.floor(sec / 3600);
      const m = Math.floor((sec % 3600) / 60);
      const s = sec % 60;
      return String(h).padStart(2,'0') + ':' + String(m).padStart(2,'0') + ':' + String(s).padStart(2,'0');
    }
  }));

  // Theme toggle state — bound to the data-theme attribute on <html>.
  Alpine.data('themeToggle', () => ({
    get current() {
      const stored = localStorage.getItem('paratrack-theme');
      if (stored === 'light' || stored === 'dark') return stored;
      return 'auto';
    },
    cycle() {
      const order = ['auto', 'light', 'dark'];
      const next = order[(order.indexOf(this.current) + 1) % order.length];
      localStorage.setItem('paratrack-theme', next);
      applyTheme(next);
      window.paratrackToast('theme: ' + next, 'success');
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
      chart.setOption({
        backgroundColor: 'transparent',
        textStyle: { color: cssVar('--text') || '#1a1d23', fontFamily: 'inherit' },
        grid: { left: 50, right: 20, top: 20, bottom: 40, containLabel: true },
        tooltip: { trigger: 'axis' },
        xAxis: {
          type: 'category',
          data: data.hours,
          axisLabel: { color: cssVar('--muted') || '#6b7280', fontSize: 11 },
          axisLine: { lineStyle: { color: cssVar('--border') || '#e5e7eb' } },
          axisTick: { show: false },
        },
        yAxis: {
          type: 'value',
          name: 'min',
          nameLocation: 'end',
          nameTextStyle: { color: cssVar('--muted') || '#6b7280', fontSize: 10, padding: [0, 0, 4, 0] },
          axisLabel: {
            color: cssVar('--muted') || '#6b7280',
            fontSize: 11,
            formatter: function (v) { return v >= 60 ? Math.floor(v / 60) + 'h' : v + 'm'; },
          },
          splitLine: { lineStyle: { color: cssVar('--border') || '#e5e7eb', type: 'dashed' } },
        },
        series: data.series.map((s) => ({
          name: s.name,
          type: 'bar',
          stack: 'hour',
          data: s.data,
          itemStyle: { color: s.color, borderRadius: [3, 3, 0, 0] },
          barMaxWidth: 28,
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
  };
});

window.applyTheme = function(mode) {
  const html = document.documentElement;
  if (mode === 'light') {
    html.dataset.theme = 'light';
  } else if (mode === 'dark') {
    html.dataset.theme = 'dark';
  } else {
    delete html.dataset.theme; // fall back to prefers-color-scheme
  }
};

// Resolve the effective theme on every page load before paint.
(function() {
  const stored = localStorage.getItem('paratrack-theme');
  if (stored === 'light' || stored === 'dark') {
    document.documentElement.dataset.theme = stored;
  }
})();

// Toast helper used by HTMX handlers in base.html.
window.paratrackToast = function(message, kind) {
  const el = document.getElementById('toast');
  if (!el) return;
  el.className = 'toast show toast-' + (kind || 'success');
  el.textContent = message;
  clearTimeout(window._paratrackToastTimer);
  window._paratrackToastTimer = setTimeout(() => {
    el.className = 'toast toast-' + (kind || 'success');
  }, 2200);
};

// paratrackResize is called from the inline onchange handler on the
// duration input. It parses the human value (e.g. "1h 30m"), computes
// a new end_at from the row's start_at, and fires the same PATCH that
// the end_at input would. The server handles the update and HTMX swaps
// the whole row back in.
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
    window.paratrackToast('need start and duration', 'error');
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
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    });
    if (!resp.ok) {
      const t = await resp.text();
      window.paratrackToast(t || 'resize failed', 'error');
      return;
    }
    const html = await resp.text();
    // Toast from response header.
    const toast = resp.headers.get('X-Toast');
    if (toast) window.paratrackToast(toast, resp.headers.get('X-Toast-Kind') || 'success');
    // HTMX-style outer swap.
    const tmp = document.createElement('tbody');
    tmp.innerHTML = html.trim();
    const newRow = tmp.firstElementChild;
    if (newRow) row.outerHTML = newRow.outerHTML;
  } catch (e) {
    window.paratrackToast('network error', 'error');
  }
};

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
