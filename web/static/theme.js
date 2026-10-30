// Applies the saved theme before first paint (loaded in <head>; no inline scripts under the CSP).
(function () {
  try {
    var t = localStorage.getItem('brpanel-theme');
    if (t === 'light' || t === 'dark') { document.documentElement.setAttribute('data-theme', t); }
  } catch (e) { /* storage blocked: keep the default (dark) */ }
})();
