// The graph's ECharts timeline: stacked bars per hour, a tooltip in the
// interface's units, theme-aware colours, and legend chips that hide a series.
// Built imperatively because ECharts paints on a canvas outside React's tree;
// React owns the surrounding elements and the chip state it renders.

export type TimelineSeries = { name: string; color: string; data: number[] }
export type TimelineData = { hasData: boolean; hours: string[]; series: TimelineSeries[] }

type ECharts = {
  init: (el: HTMLElement, theme: null, opts: { renderer: "canvas" }) => EChartsInstance
  getInstanceByDom: (el: HTMLElement) => EChartsInstance | undefined
}
type EChartsInstance = { dispose: () => void; resize: () => void; setOption: (option: unknown) => void }

declare global {
  interface Window { echarts?: ECharts }
}

import { PALETTE_HEX } from "@/lib/activity-color"

// Localised short units, as the rest of the interface writes them.
export function durationUnits(lang: string): [string, string, string] {
  return lang === "ru" ? [" ч", " мин", " с"] : ["h", "m", "s"]
}

// The theme tokens live on #paratrack-react-root (the CSS is scoped there),
// not on <html>: read from the document they came back empty, and the chart
// drew light-theme greys at 2:1 on the dark card. A canvas takes a literal, so
// the token is painted once into a pixel and read back as rgba().
const probe = document.createElement("canvas").getContext("2d", { willReadFrequently: true })
function cssVar(name: string, fallback: string): string {
  const root = document.getElementById("paratrack-react-root") ?? document.documentElement
  const raw = getComputedStyle(root).getPropertyValue(name).trim()
  if (!raw || !probe) return fallback
  probe.clearRect(0, 0, 1, 1)
  probe.fillStyle = fallback
  probe.fillStyle = raw
  probe.fillRect(0, 0, 1, 1)
  const [r, g, b, a] = probe.getImageData(0, 0, 1, 1).data
  return `rgba(${r}, ${g}, ${b}, ${(a / 255).toFixed(3)})`
}

// A bar is painted on a canvas, where var() means nothing: a series gets a
// literal colour. The server decides which colour an activity wears; the paint
// comes from the interface tokens so a bar follows the theme.
function seriesColor(hex: string): string {
  const wanted = /^#[0-9a-f]{3,8}$/i.test(hex.trim()) ? hex.trim().toLowerCase() : ""
  if (!wanted) return "currentColor"
  const index = PALETTE_HEX.indexOf(wanted)
  if (index < 0) return wanted
  return cssVar(`--activity-${index + 1}`, wanted)
}

// The tooltip writes a colour into a style attribute: only a plain colour
// literal goes in, never whatever a series name or option carried.
const safeColor = (value: string) => /^(#[0-9a-f]{3,8}|rgba?\([\d.,\s]+\))$/i.test(value.trim()) ? value.trim() : "currentColor"

const escapeHTML = (value: string) => value.replace(/[&<>"']/g, char => ({
  "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
})[char] ?? char)

// Mounts the timeline into `canvas` and wires `legend` chips. Returns a cleanup.
export function mountTimeline(
  canvas: HTMLElement, data: TimelineData, legend: HTMLElement | null, lang: string, onBuilt: () => void,
): () => void {
  const echarts = window.echarts
  if (!echarts || !data.hasData) return () => {}
  const units = durationUnits(lang)
  const hidden = new Set<number>()
  let chart: EChartsInstance | undefined
  const frame = requestAnimationFrame(build)

  function build() {
    const old = echarts!.getInstanceByDom(canvas)
    if (old) old.dispose()
    chart = echarts!.init(canvas, null, { renderer: "canvas" })
    const ink = cssVar("--foreground", "#1a1d23")
    const quiet = cssVar("--muted-foreground", "#6b7280")
    const line = cssVar("--border", "#e5e7eb")
    chart.setOption({
      backgroundColor: "transparent",
      textStyle: { color: ink, fontFamily: "Inter, sans-serif" },
      grid: { left: 50, right: 20, top: 20, bottom: 40, containLabel: true },
      tooltip: {
        trigger: "axis",
        backgroundColor: cssVar("--popover", "#fff"),
        borderColor: line,
        textStyle: { color: cssVar("--popover-foreground", "#1a1d23"), fontSize: 12 },
        formatter: (params: { value: number; color: string; seriesName: string; axisValue: string }[]) => {
          if (!params || !params.length) return ""
          const rows = params.filter(p => p.value > 0).map(p =>
            `<span style="display:inline-block;width:8px;height:8px;border-radius:2px;background:${safeColor(p.color)};margin-right:6px"></span>` +
            `${escapeHTML(p.seriesName)}: <b>${p.value}${units[1]}</b>`)
          const total = params.reduce((sum, p) => sum + (p.value || 0), 0)
          return `<div style="font-weight:600;margin-bottom:4px">${escapeHTML(params[0].axisValue)}:00</div>` +
            (rows.length ? rows.join("<br/>") : `<span style="opacity:.6">0${units[1]}</span>`) +
            `<div style="margin-top:6px;opacity:.65">Σ ${total}${units[1]}</div>`
        },
      },
      xAxis: {
        type: "category",
        data: data.hours,
        axisLabel: { color: quiet, fontSize: 11 },
        axisLine: { lineStyle: { color: line } },
        axisTick: { show: false },
      },
      yAxis: {
        type: "value",
        name: units[1].trim(),
        nameLocation: "end",
        nameTextStyle: { color: quiet, fontSize: 10, padding: [0, 0, 4, 0] },
        // Whole-minute ticks, so the axis uses the same "Xh YYm / Xm" units as the rest.
        minInterval: 1,
        axisLabel: {
          color: quiet,
          fontSize: 11,
          formatter: (value: number) => {
            if (value >= 60) {
              const hours = Math.floor(value / 60)
              const minutes = Math.round(value % 60)
              return `${hours}${units[0]}${minutes ? ` ${minutes}${units[1]}` : ""}`
            }
            // Data is in minutes: a sub-minute tick shows seconds, as /stats does.
            if (value < 1 && value > 0) return `${Math.round(value * 60)}${units[2]}`
            return `${Math.round(value)}${units[1]}`
          },
        },
        splitLine: { lineStyle: { color: line, type: "dashed" } },
      },
      series: data.series.map((series, index) => ({
        name: series.name,
        type: "bar",
        stack: "hour",
        data: hidden.has(index) ? series.data.map(() => 0) : series.data,
        itemStyle: { color: seriesColor(series.color), borderRadius: [4, 4, 0, 0], borderColor: cssVar("--card", "#fff"), borderWidth: 0.5 },
        barMaxWidth: 22,
        barCategoryGap: "28%",
        emphasis: { focus: "series" },
      })),
      animationDuration: 400,
      animationEasing: "cubicOut",
    })
    onBuilt()
  }

  // Theme changes repaint the colours; a resize refits the canvas.
  const theme = new MutationObserver(build)
  theme.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] })
  const resize = new ResizeObserver(() => echarts.getInstanceByDom(canvas)?.resize())
  resize.observe(canvas)

  function toggle(event: Event) {
    const chip = (event.target as Element).closest<HTMLElement>(".legend-chip")
    if (!chip || !chart) return
    const index = Number(chip.dataset.seriesIndex)
    if (hidden.has(index)) hidden.delete(index)
    else hidden.add(index)
    // Zeroed data really restacks the rest and drops the hidden one from the tooltip.
    chart.setOption({
      series: data.series.map((series, j) => ({ ...series, data: hidden.has(j) ? series.data.map(() => 0) : series.data })),
    })
    chip.setAttribute("aria-pressed", hidden.has(index) ? "false" : "true")
  }
  legend?.addEventListener("click", toggle)

  return () => {
    cancelAnimationFrame(frame)
    theme.disconnect()
    resize.disconnect()
    legend?.removeEventListener("click", toggle)
    const current = echarts.getInstanceByDom(canvas)
    current?.dispose()
  }
}
