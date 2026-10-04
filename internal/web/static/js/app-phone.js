// Phone navigation sheet, touch feedback, and small-screen interactions.
// ---- Phone chrome: bottom sheet + haptics ----
(function () {
  document.addEventListener('click', (e) => {
    const opener = e.target.closest('[data-sheet-open]');
    if (opener) {
      const d = document.getElementById(opener.dataset.sheetOpen);
      if (d && !d.open) {
        d.showModal();
        d.querySelector('.sheet-content').scrollTop = 0;
      }
      return;
    }
    const grab = e.target.closest('[data-sheet-grab]');
    if (grab) {
      if (performance.now() >= suppressGrabClickUntil) grab.closest('dialog')?.close();
      return;
    }
    // Tap on the dimmed backdrop closes the sheet. Backdrop clicks target
    // the dialog itself, but so do clicks on its padding: check the point.
    const d = e.target.matches && e.target.matches('dialog.sheet[open]') ? e.target : null;
    if (d && e.clientY < d.getBoundingClientRect().top) d.close();
  });

  // Only the handle drags the sheet. Swiping anywhere in its list scrolls
  // that list, including when it is already at the top.
  let sheet = null, y0 = 0, dy = 0, moved = false, suppressGrabClickUntil = 0;
  document.querySelectorAll('dialog.sheet').forEach((d) => {
    d.addEventListener('close', () => {
      d.querySelector('.sheet-content').scrollTop = 0;
      d.style.transform = '';
      d.style.transition = '';
    });
  });
  document.addEventListener('touchstart', (e) => {
    const grab = e.target.closest && e.target.closest('[data-sheet-grab]');
    const d = grab?.closest('dialog.sheet[open]');
    if (!d || e.touches.length !== 1) return;
    sheet = d; y0 = e.touches[0].clientY; dy = 0; moved = false;
  }, { passive: true });
  document.addEventListener('touchmove', (e) => {
    if (!sheet) return;
    const delta = e.touches[0].clientY - y0;
    if (Math.abs(delta) > 10) moved = true;
    dy = Math.max(0, delta);
    if (dy) sheet.style.transition = 'none';
    sheet.style.transform = dy ? `translateY(${dy}px)` : '';
  }, { passive: true });
  document.addEventListener('touchend', () => {
    if (!sheet) return;
    const d = sheet; sheet = null;
    if (moved) suppressGrabClickUntil = performance.now() + 450;
    if (dy > 90) {
      d.style.transition = 'transform .18s ease-out';
      d.style.transform = 'translateY(100%)';
      setTimeout(() => d.close(), 180);
    } else if (dy) {
      d.style.transition = 'transform .18s ease-out';
      d.style.transform = '';
    }
  });
  document.addEventListener('touchcancel', () => {
    if (!sheet) return;
    sheet.style.transform = '';
    sheet.style.transition = '';
    sheet = null;
  });

  // A short tick on timer actions (Android; iOS ignores vibrate).
  document.body.addEventListener('htmx:afterRequest', (e) => {
    const path = (e.detail.requestConfig && e.detail.requestConfig.path) || '';
    if (e.detail.successful && /\/api\/(start|focus|sessions\/\d+\/(stop|pause|resume))/.test(path)) {
      if (navigator.vibrate) navigator.vibrate(12);
    }
  });
})();
