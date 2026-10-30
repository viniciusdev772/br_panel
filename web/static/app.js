(function () {
  'use strict';
  var $ = function (s, r) { return (r || document).querySelector(s); };
  var $$ = function (s, r) { return Array.prototype.slice.call((r || document).querySelectorAll(s)); };

  // ---- Material components ----
  var fields = {};
  $$('.mdc-text-field').forEach(function (el) {
    var tf = new mdc.textField.MDCTextField(el);
    var input = $('input, textarea', el);
    if (input && input.name) { fields[input.name] = tf; }
  });
  $$('.mdc-button').forEach(function (el) { new mdc.ripple.MDCRipple(el); });
  $$('.mdc-form-field').forEach(function (el) {
    var ff = new mdc.formField.MDCFormField(el);
    ff.input = new mdc.checkbox.MDCCheckbox($('.mdc-checkbox', el));
  });
  var bar = $('.mdc-top-app-bar');
  if (bar) { new mdc.topAppBar.MDCTopAppBar(bar); }

  $$('form[data-confirm]').forEach(function (f) {
    f.addEventListener('submit', function (e) {
      if (!window.confirm(f.getAttribute('data-confirm'))) { e.preventDefault(); }
    });
  });

  // ---- formatting ----
  function fmtBytes(n) {
    if (n == null) { return '—'; }
    var u = ['B', 'KB', 'MB', 'GB', 'TB'], i = 0;
    while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
    return (n >= 100 || i === 0 ? n.toFixed(0) : n.toFixed(1)) + ' ' + u[i];
  }
  function fmtPct(n) { return n == null ? '—' : n.toFixed(n >= 10 ? 0 : 1) + '%'; }
  function fmtDur(sec) {
    if (!sec || sec < 0) { return '—'; }
    var d = Math.floor(sec / 86400), h = Math.floor(sec % 86400 / 3600), m = Math.floor(sec % 3600 / 60);
    if (d) { return d + 'd ' + h + 'h'; }
    if (h) { return h + 'h ' + m + 'm'; }
    return m ? m + 'm ' + Math.floor(sec % 60) + 's' : Math.floor(sec) + 's';
  }
  function getJSON(url) {
    return fetch(url, { credentials: 'same-origin' }).then(function (r) {
      if (!r.ok) { throw new Error(r.status); }
      return r.json();
    });
  }
  function setBar(id, ratio) {
    var el = document.getElementById(id);
    if (!el) { return; }
    el.style.width = Math.max(0, Math.min(1, ratio)) * 100 + '%';
    el.className = 'bar-fill' + (ratio > 0.9 ? ' bad' : ratio > 0.75 ? ' warn' : '');
  }
  function setText(id, t) { var el = document.getElementById(id); if (el) { el.textContent = t; } }

  // ---- dashboard: host + per-app live numbers ----
  if ($('#host-stats')) {
    var overview = function () {
      getJSON('/api/overview').then(function (o) {
        var h = o.host;
        setText('h-mem', fmtBytes(h.mem_used) + ' / ' + fmtBytes(h.mem_total));
        setBar('h-mem-bar', h.mem_total ? h.mem_used / h.mem_total : 0);
        setText('h-load', h.load.map(function (x) { return x.toFixed(2); }).join(' / ') + ' (' + h.cpus + ' CPU)');
        setBar('h-load-bar', h.cpus ? h.load[0] / h.cpus : 0);
        setText('h-disk', fmtBytes(h.disk_used) + ' / ' + fmtBytes(h.disk_total));
        setBar('h-disk-bar', h.disk_total ? h.disk_used / h.disk_total : 0);
        setText('h-up', fmtDur(h.uptime));
        Object.keys(o.apps).forEach(function (name) {
          var a = o.apps[name];
          var cpu = $('[data-cpu="' + name + '"]'), mem = $('[data-mem="' + name + '"]'), st = $('[data-state="' + name + '"]');
          if (cpu) { cpu.textContent = fmtPct(a.cpu); }
          if (mem) { mem.textContent = fmtBytes(a.mem); }
          if (st) { st.textContent = a.state; st.className = 'chip chip--' + a.state; }
        });
      }).catch(function () {});
    };
    overview();
    setInterval(overview, 5000);
  }

  // ---- app page: charts ----
  function drawChart(canvas, pts, key, fmt, color, from, to, hoverIdx) {
    var dpr = window.devicePixelRatio || 1, w = canvas.clientWidth, h = canvas.clientHeight;
    if (!w || !h) { return; }
    canvas.width = w * dpr; canvas.height = h * dpr;
    var c = canvas.getContext('2d');
    c.scale(dpr, dpr);
    var L = 56, R = 8, T = 8, B = 26, pw = w - L - R, ph = h - T - B;
    var max = 0;
    pts.forEach(function (p) { if (p[key] > max) { max = p[key]; } });
    var steps = key === 'cpu' ? [10, 25, 50, 100, 200, 400, 800, 1600] : null;
    if (steps) { max = steps.filter(function (s) { return s >= max; })[0] || Math.ceil(max / 100) * 100; }
    else { var mag = Math.pow(2, Math.ceil(Math.log2(Math.max(max, 1 << 20)))); max = mag; }
    var x = function (t) { return L + (t - from) / Math.max(1, to - from) * pw; };
    var y = function (v) { return T + ph - v / max * ph; };

    c.font = '11px Roboto, sans-serif';
    c.fillStyle = 'rgba(0,0,0,.6)'; c.strokeStyle = 'rgba(0,0,0,.08)'; c.lineWidth = 1;
    c.textAlign = 'right'; c.textBaseline = 'middle';
    for (var i = 0; i <= 4; i++) {
      var v = max * i / 4, yy = Math.round(y(v)) + .5;
      c.beginPath(); c.moveTo(L, yy); c.lineTo(w - R, yy); c.stroke();
      c.fillText(fmt(v), L - 6, yy);
    }
    c.textBaseline = 'top';
    for (var k = 0; k <= 4; k++) {
      var t = from + (to - from) * k / 4, d = new Date(t * 1000);
      c.textAlign = k === 0 ? 'left' : k === 4 ? 'right' : 'center';
      c.fillText(('0' + d.getHours()).slice(-2) + ':' + ('0' + d.getMinutes()).slice(-2), L + pw * k / 4, h - B + 8);
    }
    if (!pts.length) { return; }

    // break the line across gaps (service stopped), at 3x the typical step
    var step = pts.length > 1 ? (pts[pts.length - 1].ts - pts[0].ts) / (pts.length - 1) : 15;
    var gap = Math.max(60, step * 3);
    var segs = [], cur = [];
    pts.forEach(function (p, idx) {
      if (cur.length && p.ts - cur[cur.length - 1].ts > gap) { segs.push(cur); cur = []; }
      cur.push(p);
    });
    segs.push(cur);
    c.lineWidth = 1.8; c.strokeStyle = color;
    segs.forEach(function (s) {
      c.beginPath();
      s.forEach(function (p, i) { i ? c.lineTo(x(p.ts), y(p[key])) : c.moveTo(x(p.ts), y(p[key])); });
      c.stroke();
      c.lineTo(x(s[s.length - 1].ts), y(0)); c.lineTo(x(s[0].ts), y(0)); c.closePath();
      c.globalAlpha = .12; c.fillStyle = color; c.fill(); c.globalAlpha = 1;
      if (s.length === 1) { c.beginPath(); c.arc(x(s[0].ts), y(s[0][key]), 2.5, 0, 7); c.fillStyle = color; c.fill(); }
    });
    if (hoverIdx != null && pts[hoverIdx]) {
      var p = pts[hoverIdx], px = x(p.ts), d2 = new Date(p.ts * 1000);
      c.strokeStyle = 'rgba(0,0,0,.35)'; c.beginPath(); c.moveTo(px, T); c.lineTo(px, T + ph); c.stroke();
      c.beginPath(); c.arc(px, y(p[key]), 3.5, 0, 7); c.fillStyle = color; c.fill();
      var label = fmt(p[key]) + '  ' + ('0' + d2.getHours()).slice(-2) + ':' + ('0' + d2.getMinutes()).slice(-2) + ':' + ('0' + d2.getSeconds()).slice(-2);
      var tw = c.measureText(label).width + 12, tx = Math.min(Math.max(px - tw / 2, L), w - R - tw);
      c.fillStyle = 'rgba(33,33,33,.9)'; c.fillRect(tx, T + 2, tw, 18);
      c.fillStyle = '#fff'; c.textAlign = 'left'; c.textBaseline = 'middle'; c.fillText(label, tx + 6, T + 11);
    }
  }

  var stats = $('#app-stats');
  if (stats) {
    var app = stats.dataset.app, range = '1h', last = null;
    var cpuC = $('#chart-cpu'), memC = $('#chart-mem');
    var render = function (hover) {
      if (!last) { return; }
      drawChart(cpuC, last.points, 'cpu', function (v) { return v.toFixed(0) + '%'; }, '#1565c0', last.from, last.to, hover);
      drawChart(memC, last.points, 'mem', fmtBytes, '#00897b', last.from, last.to, hover);
    };
    var load = function () {
      getJSON('/apps/' + app + '/metrics?range=' + range).then(function (m) {
        last = m;
        var p = m.points.length ? m.points[m.points.length - 1] : null;
        var fresh = p && (Date.now() / 1000 - p.ts) < 120 && m.info.state === 'active';
        setText('s-cpu', fresh ? fmtPct(p.cpu) : '—');
        setText('s-mem', fresh ? fmtBytes(p.mem) : '—');
        setText('s-up', fmtDur(m.info.uptime));
        setText('s-restarts', String(m.info.restarts));
        $('#chart-empty').hidden = m.points.length > 0;
        render(null);
      }).catch(function () {});
    };
    $$('.range-btn').forEach(function (b) {
      b.addEventListener('click', function () {
        $$('.range-btn').forEach(function (o) { o.classList.toggle('is-active', o === b); });
        range = b.dataset.range; load();
      });
    });
    [cpuC, memC].forEach(function (cv) {
      cv.addEventListener('mousemove', function (e) {
        if (!last || !last.points.length) { return; }
        var rect = cv.getBoundingClientRect(), t = last.from + (e.clientX - rect.left - 56) / (rect.width - 64) * (last.to - last.from);
        var best = 0;
        last.points.forEach(function (p, i) { if (Math.abs(p.ts - t) < Math.abs(last.points[best].ts - t)) { best = i; } });
        render(best);
      });
      cv.addEventListener('mouseleave', function () { render(null); });
    });
    window.addEventListener('resize', function () { render(null); });
    load();
    setInterval(load, 15000);
  }

  // ---- app page: deploy/service logs (short) ----
  function refresh(pre, stick) {
    return fetch(pre.dataset.url, { credentials: 'same-origin' }).then(function (r) {
      var deploying = r.headers.get('X-Deploying') === 'true';
      return r.text().then(function (t) {
        var atBottom = pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 8;
        pre.textContent = t || '(vazio)';
        if (stick || atBottom) { pre.scrollTop = pre.scrollHeight; }
        return deploying;
      });
    });
  }
  var deployLog = $('#log-deploy'), serviceLog = $('#log-service');
  if (deployLog && serviceLog) {
    var tick = function () {
      refresh(deployLog, false).then(function (deploying) {
        if (deploying) { setTimeout(tick, 2000); }
        else if ($('#deploy-status').dataset.deploying === 'true') { location.reload(); }
      }).catch(function () { setTimeout(tick, 5000); });
    };
    refresh(serviceLog, true);
    tick();
    setInterval(function () { refresh(serviceLog, false).catch(function () {}); }, 10000);
  }

  // ---- full logs page ----
  var tools = $('#logtools');
  if (tools) {
    var lapp = tools.dataset.app, kind = 'service', timer = null, raw = '';
    var view = $('#logview'), filter = $('#log-filter'), follow = $('#log-follow'), linesSel = $('#log-lines');
    var errRe = /\b(error|erro|fatal|panic|exception|failed|falhou|traceback)\b/i, warnRe = /\b(warn|warning|aviso)\b/i;
    var url = function (extra) { return '/apps/' + lapp + '/log?kind=' + kind + '&lines=' + linesSel.value + (extra || ''); };
    var paint = function () {
      var q = filter.value.trim().toLowerCase(), lines = raw.split('\n'), frag = document.createDocumentFragment(), n = 0;
      if (lines[lines.length - 1] === '') { lines.pop(); }
      var wasBottom = view.scrollTop + view.clientHeight >= view.scrollHeight - 24;
      lines.forEach(function (l) {
        if (q && l.toLowerCase().indexOf(q) < 0) { return; }
        var d = document.createElement('div');
        d.className = 'logline' + (errRe.test(l) ? ' err' : warnRe.test(l) ? ' warn' : '');
        d.textContent = l; frag.appendChild(d); n++;
      });
      view.textContent = ''; view.appendChild(frag);
      if (follow.checked || wasBottom) { view.scrollTop = view.scrollHeight; }
      setText('log-status', n + ' de ' + lines.length + ' linhas' + (q ? ' (filtradas)' : ''));
      $('#log-download').href = url('&download=1');
    };
    var fetchLogs = function () {
      fetch(url(), { credentials: 'same-origin' }).then(function (r) { return r.text(); })
        .then(function (t) { raw = t; paint(); }).catch(function () { setText('log-status', 'Falha ao carregar.'); });
    };
    var schedule = function () { clearInterval(timer); if (follow.checked) { timer = setInterval(fetchLogs, 3000); } };
    $$('.tab').forEach(function (b) {
      b.addEventListener('click', function () {
        $$('.tab').forEach(function (o) { o.classList.toggle('is-active', o === b); });
        kind = b.dataset.kind; fetchLogs();
      });
    });
    linesSel.addEventListener('change', fetchLogs);
    filter.addEventListener('input', paint);
    follow.addEventListener('change', function () { schedule(); if (follow.checked) { view.scrollTop = view.scrollHeight; } });
    fetchLogs(); schedule();
  }

  // ---- new app form: auto detection ----
  var detectBtn = $('#detect-btn');
  if (detectBtn) {
    var box = $('#detect-result'), csrf = $('input[name=csrf]').value;
    var esc = function (s) { var d = document.createElement('div'); d.textContent = s; return d.innerHTML; };
    var setField = function (name, v, onlyIfEmpty) {
      var tf = fields[name];
      if (!tf || v == null || v === '') { return; }
      if (onlyIfEmpty && tf.value) { return; }
      tf.value = v;
    };
    var detect = function () {
      var repo = fields.repo.value.trim();
      if (!repo) { fields.repo.valid = false; return; }
      detectBtn.disabled = true;
      box.hidden = false; box.className = 'detect-result'; box.textContent = 'Clonando e analisando o projeto…';
      var body = new URLSearchParams({ csrf: csrf, repo: repo, branch: fields.branch.value.trim() });
      fetch('/apps/detect', { method: 'POST', credentials: 'same-origin', body: body }).then(function (r) { return r.json(); }).then(function (d) {
        detectBtn.disabled = false;
        setField('name', d.name, true);
        if (d.error && !d.kind) { box.className = 'detect-result bad'; box.textContent = d.error; return; }
        setField('build_cmd', d.build); setField('start_cmd', d.start);
        var html = '<b>Detectado: ' + esc(d.kind) + '</b>';
        if (d.start) { html += '<br>Início: <code>' + esc(d.start) + '</code>'; }
        if (d.build) { html += '<br>Build: <code>' + esc(d.build) + '</code>'; }
        (d.notes || []).forEach(function (n) { html += '<br>• ' + esc(n); });
        var warn = false;
        if (d.missing && d.missing.length) { warn = true; html += '<br><b>Faltam no servidor:</b> ' + d.missing.map(esc).join(', '); }
        box.className = 'detect-result' + (warn || !d.start ? ' warn' : '');
        box.innerHTML = html;
      }).catch(function () { detectBtn.disabled = false; box.className = 'detect-result bad'; box.textContent = 'Falha ao detectar.'; });
    };
    detectBtn.addEventListener('click', detect);
    $('input[name=repo]').addEventListener('change', function () {
      if (!fields.start_cmd.value && !fields.build_cmd.value) { detect(); }
    });
  }
})();
