// Runs before the page's own script, in the page's test (page_test.go): it
// keeps every error the page throws, and asks for reduced motion, so the
// camera lands at once and the checks need not wait for it.
window.__errors = [];
window.addEventListener('error', e => window.__errors.push(String(e.message || e)));
window.addEventListener('unhandledrejection', e => window.__errors.push('rejection: ' + e.reason));
{
  const real = window.matchMedia.bind(window);
  window.matchMedia = q => (q.includes('prefers-reduced-motion')
    ? { matches: true, media: q, addEventListener() {}, removeEventListener() {} }
    : real(q));
}
