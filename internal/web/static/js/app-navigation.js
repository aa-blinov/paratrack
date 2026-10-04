// Page-wide navigation, keyboard shortcuts, and focus placement.
function sessionValue(key) {
  try { return sessionStorage.getItem(key); } catch (_) { return null; }
}

function setSessionValue(key, value) {
  try { sessionStorage.setItem(key, value); } catch (_) {}
}

function removeSessionValue(key) {
  try { sessionStorage.removeItem(key); } catch (_) {}
}

// Clicking the already active navigation item should not tear down the page.
// Keep real navigation for links with a query/hash, modifiers, downloads, or
// another origin; only the exact current document is a no-op.
document.addEventListener('click', (event) => {
  if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
  const link = event.target.closest('a[href]');
  if (!link || link.hasAttribute('download') || (link.target && link.target !== '_self')) return;
  let url;
  try { url = new URL(link.href, location.href); } catch (_) { return; }
  if (url.origin !== location.origin || url.pathname !== location.pathname || url.search !== location.search || url.hash) return;
  event.preventDefault();
  link.blur();
  link.closest('dialog[open]')?.close();
}, true);

// A tab strip that scrolls sideways on a phone opens with the current tab in view.
(function () {
  function centre() {
    document.querySelectorAll('[role="tablist"]').forEach((list) => {
      const current = list.querySelector('[aria-current="page"]');
      if (current && list.scrollWidth > list.clientWidth) {
        list.scrollLeft = current.offsetLeft - (list.clientWidth - current.offsetWidth) / 2;
      }
    });
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', centre);
  else centre();
})();

// Keyboard shortcuts are available on every page and ignored while typing.
(function () {
  function isTyping(target) {
    if (!target) return false;
    const tag = target.tagName;
    return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target.isContentEditable;
  }

  document.addEventListener('keydown', (event) => {
    if (event.metaKey || event.ctrlKey || event.altKey || isTyping(event.target)) return;
    const key = event.key.toLowerCase();
    const here = window.location.pathname;
    if (key === 'g' && here !== '/graph') { window.location.href = '/graph'; return; }
    if (key === 's' && here !== '/stats') { window.location.href = '/stats'; return; }
    if (key === 'd' && here !== '/') { window.location.href = '/'; return; }
    if (key === 't') {
      document.querySelector('[data-theme-toggle]')?.click();
      return;
    }
    if (key === 'n') {
      if (here === '/') document.querySelector('input[name="activity"]')?.focus();
      else {
        setSessionValue('paratrack-focus-new', '1');
        window.location.href = '/';
      }
      return;
    }
    // On other pages the response refreshes only the minibar, never the dashboard list.
    if (key === 'p' && window.htmx) {
      window.htmx.ajax('POST', '/api/active/pause-all', {
        target: here === '/' ? '#active-list' : '#minibar',
        swap: here === '/' ? 'innerHTML' : 'none',
      });
    }
  });

  if (window.location.pathname === '/' && sessionValue('paratrack-focus-new') === '1') {
    removeSessionValue('paratrack-focus-new');
    document.querySelector('input[name="activity"]')?.focus();
  }
})();
