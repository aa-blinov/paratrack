// Page navigation.
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
