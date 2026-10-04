// Provider-specific hints for task integration credentials and target IDs.
document.addEventListener('alpine:init', () => {
  Alpine.data('integrationForm', () => ({
    p: 'github',
  }));
});
