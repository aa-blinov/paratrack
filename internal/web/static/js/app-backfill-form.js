// Preserve field feedback for the dashboard's historical-session form.
document.querySelector('#backfill form')?.addEventListener('input', (event) => {
  const field = event.target;
  if (!['b-activity', 'b-start', 'b-end'].includes(field.id)) return;
  field.removeAttribute('aria-invalid');
  const message = document.getElementById(field.id + '-error');
  if (message) message.textContent = '';
});

function beforeBackfillRequest(form) {
  form.parentElement.querySelectorAll('[id^="b-"][id$="-error"]').forEach((element) => {
    element.textContent = '';
  });
  form.querySelectorAll('[aria-invalid]').forEach((element) => {
    element.removeAttribute('aria-invalid');
  });
}

function afterBackfillRequest(form, event) {
  const xhr = event.detail.xhr;
  if (xhr.getResponseHeader('X-Backfill-Saved') === 'true') {
    form.reset();
    return;
  }
  const field = xhr.getResponseHeader('X-Backfill-Field');
  const input = field && ['activity', 'start', 'end'].includes(field)
    ? form.querySelector('#b-' + field)
    : null;
  if (input) {
    input.setAttribute('aria-invalid', 'true');
    input.focus();
  } else if (!field) {
    form.parentElement.querySelector('#b-form-error').textContent = form.dataset.networkError;
  }
}

document.body.addEventListener('htmx:beforeRequest', (event) => {
  const form = event.detail.elt;
  if (form?.matches('[data-backfill-form]')) beforeBackfillRequest(form);
});

document.body.addEventListener('htmx:afterRequest', (event) => {
  const form = event.detail.elt?.closest('form');
  if (form?.matches('[data-backfill-form]')) afterBackfillRequest(form, event);
});
