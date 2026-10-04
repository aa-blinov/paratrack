// Preference controls whose state depends on other inputs in the same form.
document.addEventListener('alpine:init', () => {
  Alpine.data('preferenceTabs', () => ({
    n: 0,
    init() {
      const update = () => { this.n = this.$el.querySelectorAll('input:checked').length; };
      update();
      this.$el.addEventListener('change', update);
    },
  }));
});
