document.addEventListener('alpine:init', () => {
  // Invoice form: picking a project fills the client it billed last time,
  // unless the user already typed something else.
  Alpine.data('invoiceForm', () => ({
    init() {
      this.$el.addEventListener('change', (event) => {
        if (event.target.matches('[data-project-fill]')) this.fill(event.target);
      });
    },
    fill(sel) {
      const o = sel.selectedOptions[0];
      if (!o) return;
      const set = (id, v) => {
        const el = document.getElementById(id);
        if (el && (el.value === '' || el.dataset.filled === '1') && v) { el.value = v; el.dataset.filled = '1'; }
      };
      set('inv-client', o.dataset.client);
      set('inv-client-details', o.dataset.details);
      set('inv-email', o.dataset.email);
    },
  }));

});
