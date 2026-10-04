// Prevent inverted report ranges before the browser requests an export.
(function () {
  const form = document.querySelector('[data-export-form]');
  if (!form) return;

  const from = form.elements.namedItem('from');
  const to = form.elements.namedItem('to');
  const validate = () => to.setCustomValidity(
    from.value && to.value && from.value > to.value ? form.dataset.invalidPeriod : '',
  );
  form.addEventListener('input', validate);
  form.addEventListener('change', validate);
  form.addEventListener('submit', validate);
})();
