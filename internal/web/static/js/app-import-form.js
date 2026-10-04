// Source selection and timezone setup for historical time imports.
document.addEventListener('alpine:init', () => {
  Alpine.data('historyImportForm', () => ({
    p: 'toggl',
    init() {
      const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
      const field = this.$el.querySelector('[name="tz"]');
      if (field && timezone) field.value = timezone;
    },
  }));
});
