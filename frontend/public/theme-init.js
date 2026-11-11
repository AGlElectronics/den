// Apply the cached light/dark choice before first paint (see src/lib/theme.ts).
// A file rather than an inline script so the CSP can stay script-src 'self'.
(function () {
  var p = 'system';
  try {
    p = localStorage.getItem('cs-theme') || 'system';
  } catch {
    // storage blocked: follow the system setting
  }
  var dark = p === 'dark' || (p !== 'light' && window.matchMedia('(prefers-color-scheme: dark)').matches);
  document.documentElement.classList.toggle('dark', dark);
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
})();
