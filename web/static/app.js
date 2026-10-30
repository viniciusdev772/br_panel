(function () {
  'use strict';
  document.querySelectorAll('.mdc-text-field').forEach(function (el) { new mdc.textField.MDCTextField(el); });
  document.querySelectorAll('.mdc-button').forEach(function (el) { new mdc.ripple.MDCRipple(el); });
  document.querySelectorAll('.mdc-form-field').forEach(function (el) {
    var box = new mdc.checkbox.MDCCheckbox(el.querySelector('.mdc-checkbox'));
    var ff = new mdc.formField.MDCFormField(el);
    ff.input = box;
  });
  var bar = document.querySelector('.mdc-top-app-bar');
  if (bar) { new mdc.topAppBar.MDCTopAppBar(bar); }

  document.querySelectorAll('form[data-confirm]').forEach(function (f) {
    f.addEventListener('submit', function (e) {
      if (!window.confirm(f.getAttribute('data-confirm'))) { e.preventDefault(); }
    });
  });

  // Live logs on the app page; keep polling the deploy log while a deploy runs.
  function refresh(pre, stickBottom) {
    return fetch(pre.dataset.url, { credentials: 'same-origin' }).then(function (r) {
      var deploying = r.headers.get('X-Deploying') === 'true';
      return r.text().then(function (t) {
        var atBottom = pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 8;
        pre.textContent = t || '(vazio)';
        if (stickBottom || atBottom) { pre.scrollTop = pre.scrollHeight; }
        return deploying;
      });
    });
  }
  var deployLog = document.getElementById('log-deploy');
  var serviceLog = document.getElementById('log-service');
  if (deployLog && serviceLog) {
    var tick = function () {
      refresh(deployLog, false).then(function (deploying) {
        if (deploying) { setTimeout(tick, 2000); } else if (document.getElementById('deploy-status').dataset.deploying === 'true') { location.reload(); }
      }).catch(function () { setTimeout(tick, 5000); });
    };
    refresh(serviceLog, true);
    tick();
  }
})();
