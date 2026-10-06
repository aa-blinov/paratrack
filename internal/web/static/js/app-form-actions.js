// Small delegated behaviors for server-rendered forms and controls.
document.body.addEventListener('htmx:afterRequest', (event) => {
  const source = event.detail.elt;
  const form = source?.closest('form');
  if (!event.detail.successful) return;
  if (form?.hasAttribute('data-reset-after-request')) form.reset();
  if (source?.hasAttribute('data-refresh-active-after-request')) {
    window.htmx?.ajax('GET', '/api/active', { target: '#active-list', swap: 'innerHTML' });
  }
});

document.addEventListener('change', (event) => {
  const control = event.target.closest('[data-submit-on-change]');
  if (control?.form) control.form.requestSubmit();
});

document.addEventListener('click', (event) => {
  if (event.target.closest('[data-print]')) window.print();
});

const confirmedForms = new WeakSet();
document.addEventListener('submit', (event) => {
  const form = event.target.closest('form[data-confirm]');
  if (!form) return;
  if (confirmedForms.has(form)) { confirmedForms.delete(form); return; }
  if (!form.closest('#paratrack-react-root')) {
    if (!window.confirm(form.dataset.confirm)) event.preventDefault();
    return;
  }
  event.preventDefault();
  const submitter = event.submitter;
  document.dispatchEvent(new CustomEvent('paratrack:confirm', { detail: {
    message: form.dataset.confirm,
    trigger: submitter || document.activeElement,
    resolve: accepted => {
      if (!accepted || !form.isConnected) return;
      confirmedForms.add(form);
      form.requestSubmit(submitter);
    },
  }}));
}, true);
