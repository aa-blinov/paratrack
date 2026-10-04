// Ledger form state and first-run guidance.
// ---- Live ledger: optional fields open on wide screens, project named ----

const wide = window.matchMedia('(min-width: 1024px)');
document.querySelectorAll('details[data-open-wide]').forEach((d) => {
  if (wide.matches) d.open = true;
});
const sel = document.getElementById('project_id');
const label = document.querySelector('[data-project-label]');
const activity = document.getElementById('activity');
const rebindHint = document.getElementById('ledger-project-rebind');
// The last chosen project sticks: a freelancer starting one client task
// after another should not fall back to unbilled «Без проекта».
const KEY = 'paratrack-last-project';
const restore = () => {
  // A default project from settings wins over "the last one used".
  let v = sel && sel.dataset.default ? sel.dataset.default : '';
  if (!v) try { v = localStorage.getItem(KEY) || ''; } catch (_) {}
  if (sel && [...sel.options].some((o) => o.value === v)) sel.value = v;
};
const sync = () => {
  if (sel && label) label.textContent = sel.options[sel.selectedIndex].text;
  if (sel && activity && rebindHint) {
    const known = [...document.querySelectorAll('#known-activities option')]
      .find((o) => o.value === activity.value.trim());
    rebindHint.hidden = !known || (known.dataset.project || '0') === (sel.value || '0');
  }
};
activity?.addEventListener('input', sync);
if (sel) {
  sel.addEventListener('change', () => {
    try { localStorage.setItem(KEY, sel.value); } catch (_) {}
    sync();
  });
  restore();
  sync();
}
// The start form resets after each submit; put the project back.
function restoreLedgerProject() {
  setTimeout(() => { restore(); sync(); }, 0);
}

// First-run quick start: an example fills the activity field and starts it.
// After any successful start the "Next" links appear (hidden on a first run).
document.addEventListener('click', (e) => {
  const q = e.target.closest('[data-quick-start]');
  if (q) {
    const input = document.getElementById('activity');
    if (input) { input.value = q.dataset.quickStart; input.form.requestSubmit(); }
    return;
  }
  if (e.target.closest('[data-next-hide]')) {
    try { localStorage.setItem('paratrack-next-hidden', '1'); } catch (_) {}
    document.getElementById('next-steps')?.remove();
  }
});
document.body.addEventListener('htmx:afterRequest', (e) => {
  const form = e.detail.elt?.closest('form');
  if (e.detail.successful && form?.hasAttribute('data-ledger-sync')) restoreLedgerProject();
  if (e.detail.successful && /\/api\/start$/.test(e.detail.requestConfig?.path || '')) {
    document.getElementById('next-steps')?.removeAttribute('hidden');
  }
});

try {
  if (localStorage.getItem('paratrack-next-hidden')) document.getElementById('next-steps')?.remove();
} catch (_) {}
