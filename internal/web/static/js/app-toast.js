// Toast creation, actions, focus recovery, and dismissal.
import { isKeyboardInputMode } from '/static/js/app-input-mode.js';

// Toast: a flat note in the #toast live region, auto-dismisses unless held.
// actions: optional [{label, method, url}] run through htmx, so CSRF and
// the X-Toast of their response work as for any other button.
let timer;

export function paratrackToast(message, kind, ms, actions) {
  const el = document.getElementById('toast');
  if (!el) return;
  const variant = kind || 'success';
  // The message already confirms success; errors and notices keep their icon.
  const box = document.createElement('div');
  box.className = `toast-note toast-${variant} opacity-100 transition-opacity duration-200`;
  // #toast is the live region; an error also interrupts.
  if (variant === 'error') box.setAttribute('role', 'alert');
  if (variant !== 'success') {
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('class', 'icon');
    svg.setAttribute('aria-hidden', 'true');
    const use = document.createElementNS('http://www.w3.org/2000/svg', 'use');
    use.setAttribute('href', '/static/icons.svg#i-' + (variant === 'error' ? 'x' : 'info'));
    svg.append(use);
    box.append(svg);
  }
  // textContent: messages carry user input (activity names).
  const text = document.createElement('span');
  text.textContent = message;
  box.append(text);
  for (const a of actions || []) {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'toast-action' + (a.quiet ? ' is-quiet' : '');
    b.textContent = a.label;
    b.addEventListener('click', () => {
      clearTimeout(timer);
      el.replaceChildren();
      // Undo returns the fresh running list; discard returns nothing and
      // lets sessions-changed refresh the rest.
      const list = a.method === 'POST' && document.getElementById('active-list');
      htmx.ajax(a.method, a.url, list ? { target: list, swap: 'innerHTML' } : { swap: 'none' });
    });
    box.append(b);
  }
  el.replaceChildren(box);
  // Hover or focus inside holds the toast (WCAG 2.2.1): the undo must not
  // vanish while someone is reaching for it.
  const wait = ms || (actions && actions.length ? 10000 : Math.max(2200, String(message).length * 55));
  const arm = () => {
    clearTimeout(timer);
    timer = setTimeout(() => {
      if (box.matches(':hover, :focus-within')) return;
      box.classList.add('opacity-0');
      setTimeout(() => { if (box.isConnected) el.replaceChildren(); }, 250);
    }, wait);
  };
  box.addEventListener('mouseleave', arm);
  box.addEventListener('focusout', (e) => { if (!box.contains(e.relatedTarget)) arm(); });
  arm();
  // Keyboard stop: the row is gone, so focus goes to the way back.
  // htmx swaps the list after this runs, so look once the swap has landed.
  if (actions && actions.length && isKeyboardInputMode()) {
    setTimeout(() => {
      const a = document.activeElement;
      if (!a || a === document.body || !a.isConnected) box.querySelector('.toast-action')?.focus();
    }, 60);
  }
}
