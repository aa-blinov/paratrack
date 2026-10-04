// Theme selection, persistence, and system color-scheme synchronization.
function storedTheme() {
  try { return localStorage.getItem('paratrack-theme'); } catch (_) { return null; }
}

function persistTheme(mode) {
  try { localStorage.setItem('paratrack-theme', mode); } catch (_) {}
}

document.addEventListener('alpine:init', () => {
  // Theme toggle state — bound to the data-theme attribute on <html>.
  // `mode` is real Alpine state (reactive); localStorage is only the
  // persistence side-effect. Reading localStorage straight from a
  // getter would never notify x-show after cycle().
  Alpine.data('themeToggle', () => ({
    mode: 'auto',
    init() {
      const stored = storedTheme();
      this.mode = (stored === 'light' || stored === 'dark') ? stored : 'auto';
      this.$el.addEventListener('click', () => this.cycle());
    },
    get current() { return this.mode; },
    cycle() {
      const order = ['auto', 'light', 'dark'];
      const next = order[(order.indexOf(this.mode) + 1) % order.length];
      this.mode = next;
      persistTheme(next);
      applyTheme(next);
    }
  }));
});

function applyTheme(mode) {
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

// app-bootstrap.js applies the initial theme before paint; this module
// fills template labels and follows later system color-scheme changes.
applyTheme(document.documentElement.dataset.themeMode || 'auto');
if (window.matchMedia) {
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    if (document.documentElement.dataset.themeMode === 'auto') applyTheme('auto');
  });
}
