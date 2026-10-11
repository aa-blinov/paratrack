// Graph tooltip and ECharts timeline behavior.
import { durationUnits } from '/static/js/app-time-format.js';

document.addEventListener('alpine:init', () => {
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
  const initEcharts = () => {
    const wrap = document.getElementById('echart-wrap');
    const canvas = document.getElementById('echart-canvas');
    if (!wrap || !canvas) return;
    const raw = wrap.dataset.chart;
    if (!raw) return;
    let data;
    try { data = JSON.parse(raw); } catch (_) { return; }
    if (!data.hasData || typeof echarts === 'undefined') return;
    // Both the script onload and Alpine's cached-script fallback may call us.
    // One chart means one set of listeners and one source of legend state.
    if (canvas.dataset.echartsInitialized === 'true') return;
    canvas.dataset.echartsInitialized = 'true';

    const hiddenSeries = new Set();
    const escapeHTML = value => String(value).replace(/[&<>"']/g, char => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
    })[char]);
    const cssVar = (n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim();

    // A bar is painted on a canvas, where var() resolves to nothing, so a
    // series has to be handed a literal colour. The Go palette still decides
    // *which* colour an activity wears — that mapping is the product's
    // memory — but the paint comes from the interface tokens, so a bar is
    // the same mark as the dot beside the same activity and follows the
    // theme instead of sitting outside it.
    //
    // Measured: before this, the ten --activity-* tokens did not exist, the
    // graph painted raw hex from the server, and the legend dot in React
    // painted the same hex inline — two places deciding colour, one of them
    // unable to see the theme at all.
    const ACTIVITY_TOKENS = [
      '--activity-1', '--activity-2', '--activity-3', '--activity-4', '--activity-5',
      '--activity-6', '--activity-7', '--activity-8', '--activity-9', '--activity-10',
    ];
    const PALETTE_HEX = [
      '#6366f1', '#0ea5e9', '#14b8a6', '#10b981', '#84cc16',
      '#d97706', '#f97316', '#8b5cf6', '#a855f7', '#ec4899',
    ];
    const seriesColor = (hex) => {
      // Only a hex may be painted. This value is interpolated into an inline
      // `background:` in the tooltip, so anything else has to fall back —
      // measured: the escaping test caught 'red;position:absolute' reaching
      // the style attribute when this guard was missing.
      const wanted = typeof hex === 'string' && /^#[0-9a-f]{3,8}$/i.test(hex.trim())
        ? hex.trim().toLowerCase()
        : '';
      if (!wanted) return 'currentColor';
      const index = PALETTE_HEX.findIndex(p => p === wanted);
      if (index < 0) return wanted;
      return cssVar(ACTIVITY_TOKENS[index]) || wanted;
    };
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
            const minuteUnit = durationUnits()[1];
            const rows = ps.filter(p => p.value > 0).map(p => {
              const mark = seriesColor(p.color);
              return '<span style="display:inline-block;width:8px;height:8px;border-radius:2px;background:' + mark + ';margin-right:6px"></span>' +
                escapeHTML(p.seriesName) + ': <b>' + p.value + minuteUnit + '</b>';
            });
            const total = ps.reduce((a, p) => a + (p.value || 0), 0);
            return '<div style="font-weight:600;margin-bottom:4px">' + escapeHTML(ps[0].axisValue) + ':00</div>' +
              (rows.length ? rows.join('<br/>') : '<span style="opacity:.6">0' + minuteUnit + '</span>') +
              '<div style="margin-top:6px;opacity:.65">Σ ' + total + minuteUnit + '</div>';
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
          name: durationUnits()[1].trim(),
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
              if (v >= 60) {
                const hours = Math.floor(v / 60), minutes = Math.round(v % 60);
                const [hourUnit, minuteUnit] = durationUnits();
                return hours + hourUnit + (minutes ? ' ' + minutes + minuteUnit : '');
              }
              // Sub-minute: data is in minutes, so v*60 = seconds. Show
              // "Xs" so the chart axis matches the column-header units
              // on /stats (Xh YYm / Xm) for any non-zero value.
              if (v < 1 && v > 0) return Math.round(v * 60) + durationUnits()[2];
              return Math.round(v) + durationUnits()[1];
            },
          },
          splitLine: { lineStyle: { color: cssVar('--color-base-300') || '#e5e7eb', type: 'dashed' } },
        },
        series: data.series.map((s, index) => ({
          name: s.name,
          type: 'bar',
          stack: 'hour',
          data: hiddenSeries.has(index) ? s.data.map(() => 0) : s.data,
          itemStyle: { color: seriesColor(s.color), borderRadius: [4, 4, 0, 0], borderColor: cssVar('--color-base-100') || '#fff', borderWidth: 0.5 },
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
        if (hiddenSeries.has(idx)) hiddenSeries.delete(idx);
        else hiddenSeries.add(idx);
        // Zero hidden data so the remaining series really restack and the
        // tooltip no longer counts an invisible activity.
        i.setOption({
          series: data.series.map((s, j) => ({
            ...s,
            data: hiddenSeries.has(j) ? s.data.map(() => 0) : s.data,
          })),
        });
        chip.setAttribute('aria-pressed', hiddenSeries.has(idx) ? 'false' : 'true');
      });
    }
  };

  const isReactGraph = () => {
    const payload = document.getElementById('react-page-data');
    if (!payload) return false;
    try { return JSON.parse(payload.textContent || '{}').data?.GraphReact === true; }
    catch (_) { return false; }
  };

  // The graph page can load this module and ECharts in either order.
  document.addEventListener('load', (event) => {
    if (event.target.matches?.('[data-paratrack-echarts]') && !isReactGraph()) initEcharts();
  }, true);
  document.addEventListener('paratrack:graph-ready', () => requestAnimationFrame(initEcharts));
  document.addEventListener('alpine:initialized', () => {
    requestAnimationFrame(() => {
      if (typeof echarts !== 'undefined' && !isReactGraph()) initEcharts();
    });
  });
});
