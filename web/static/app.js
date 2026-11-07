(function () {
  'use strict';
  var $ = function (s, r) { return (r || document).querySelector(s); };
  var $$ = function (s, r) { return Array.prototype.slice.call((r || document).querySelectorAll(s)); };
  // The panel polls every few seconds: its timers sleep while the tab is hidden, so a page nobody sees makes no requests.
  var setInterval = function (fn, ms) { return window.setInterval(function () { if (!document.hidden) { fn(); } }, ms); };

  // ---- Material Web components ----
  // Text fields are looked up by their form name (fields.repo, fields.branch…); the elements keep .value/.reportValidity().
  var fields = {};
  $$('md-outlined-text-field[name]').forEach(function (el) { fields[el.getAttribute('name')] = el; });
  var icon = function (name) { return '<md-icon>' + name + '</md-icon>'; };

  // A destructive form asks first, in a dialog instead of window.confirm.
  var dlg = $('#confirm-dialog'), pendingForm = null;
  if (dlg) {
    $$('form[data-confirm]').forEach(function (f) {
      f.addEventListener('submit', function (e) {
        if (f.dataset.confirmed) { return; }
        e.preventDefault();
        pendingForm = f;
        $('#confirm-text').textContent = f.getAttribute('data-confirm');
        dlg.show();
      });
    });
    $('#confirm-cancel').addEventListener('click', function () { pendingForm = null; dlg.close(); });
    $('#confirm-ok').addEventListener('click', function () {
      var f = pendingForm;
      pendingForm = null; dlg.close();
      if (f) { f.dataset.confirmed = '1'; f.requestSubmit(); }
    });
  }

  // ---- theme: follows the system until the user picks one ----
  var themeBtn = $('#theme-toggle');
  if (themeBtn) {
    themeBtn.addEventListener('click', function () {
      var cur = document.documentElement.getAttribute('data-theme') || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
      var next = cur === 'light' ? 'dark' : 'light';
      document.documentElement.setAttribute('data-theme', next);
      try { localStorage.setItem('brpanel-theme', next); } catch (e) { /* storage blocked */ }
      window.dispatchEvent(new Event('resize')); // charts and edges pick up the new colors
    });
  }

  // ---- create menu (md-menu handles focus, arrows and Esc) ----
  var createBtn = $('#create-btn'), createMenu = $('#create-menu');
  if (createBtn && createMenu) {
    createBtn.addEventListener('click', function () { createMenu.open = !createMenu.open; });
  }

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
  function ago(iso) {
    var s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
    if (s < 45) { return 'agora'; }
    if (s < 3600) { return 'há ' + Math.round(s / 60) + ' min'; }
    if (s < 86400) { return 'há ' + Math.round(s / 3600) + ' h'; }
    return 'há ' + Math.round(s / 86400) + ' d';
  }
  function refreshTimes() { $$('time[data-ts]').forEach(function (t) { t.textContent = ago(t.getAttribute('data-ts')); }); }
  refreshTimes();
  setInterval(refreshTimes, 30000);
  function getJSON(url) {
    return fetch(url, { credentials: 'same-origin' }).then(function (r) {
      if (!r.ok) { return r.json().then(function (j) { throw new Error(j.error || r.status); }, function () { throw new Error(r.status); }); }
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
  function esc(s) { var d = document.createElement('div'); d.textContent = s; return d.innerHTML; }

  // ---- tabs (resource sheets) ----
  var tabsEl = $('[data-tabs]');
  var tabNames = tabsEl ? $$('[data-tab]', tabsEl).map(function (t) { return t.dataset.tab; }) : [];
  function showTab(name, updateHash) {
    var idx = tabNames.indexOf(name);
    if (idx < 0) { return; }
    $$('[data-panel]').forEach(function (p) { p.hidden = p.dataset.panel !== name; });
    tabsEl.activeTabIndex = idx;
    if (updateHash) { try { history.replaceState(null, '', '#' + name); } catch (e) { /* ignore */ } }
    window.dispatchEvent(new CustomEvent('tabshown', { detail: name }));
  }
  if (tabsEl) {
    tabsEl.addEventListener('change', function () { showTab(tabNames[tabsEl.activeTabIndex], true); });
    window.addEventListener('hashchange', function () { showTab(location.hash.slice(1), false); });
  }

  // ---- architecture canvas ----
  var canvas = $('#canvas');
  if (canvas) {
    var edgesSvg = $('#edges'), SVGNS = 'http://www.w3.org/2000/svg';
    var edgePairs = (canvas.dataset.edges || '').split(';').filter(Boolean).map(function (p) { return p.split('>'); });
    // "usa: postgres, redis": the dependency list lives on the card too (the lines hide on a phone).
    $$('.rcard').forEach(function (card) {
      var uses = edgePairs.filter(function (p) { return p[0] === card.dataset.res; }).map(function (p) { return p[1]; });
      var el = $('.rcard-uses', card);
      if (!el || !uses.length) { return; }
      el.textContent = 'usa ';
      uses.forEach(function (u, i) {
        if (i) { el.appendChild(document.createTextNode(', ')); }
        var b = document.createElement('b'); b.textContent = u; el.appendChild(b);
      });
    });
    // A route is a list of right-angle segments with rounded corners, like a line on a transit map.
    var routePath = function (pts, r) {
      var d = 'M' + pts[0][0] + ',' + pts[0][1];
      for (var i = 1; i < pts.length - 1; i++) {
        var p0 = pts[i - 1], p1 = pts[i], p2 = pts[i + 1];
        var l1 = Math.hypot(p1[0] - p0[0], p1[1] - p0[1]), l2 = Math.hypot(p2[0] - p1[0], p2[1] - p1[1]);
        if (!l1 || !l2) { d += ' L' + p1[0] + ',' + p1[1]; continue; }
        var rr = Math.min(r, l1 / 2, l2 / 2);
        d += ' L' + (p1[0] + (p0[0] - p1[0]) * rr / l1) + ',' + (p1[1] + (p0[1] - p1[1]) * rr / l1) +
          ' Q' + p1[0] + ',' + p1[1] + ' ' + (p1[0] + (p2[0] - p1[0]) * rr / l2) + ',' + (p1[1] + (p2[1] - p1[1]) * rr / l2);
      }
      return d + ' L' + pts[pts.length - 1][0] + ',' + pts[pts.length - 1][1];
    };
    var drawEdges = function () {
      if (!edgesSvg || !edgesSvg.getClientRects().length) { return; }
      $$('.edge, .station', edgesSvg).forEach(function (p) { p.remove(); });
      var base = canvas.getBoundingClientRect(), ox = base.left, oy = base.top;
      var add = function (tag, attrs) {
        var n = document.createElementNS(SVGNS, tag);
        Object.keys(attrs).forEach(function (k) { n.setAttribute(k, attrs[k]); });
        edgesSvg.appendChild(n);
      };
      edgePairs.forEach(function (ft) {
        var a = $('[data-res="' + ft[0] + '"]'), b = $('[data-res="' + ft[1] + '"]');
        if (!a || !b) { return; }
        var ra = a.getBoundingClientRect(), rb = b.getBoundingClientRect(), pts;
        if (rb.left >= ra.right - 10 || ra.left >= rb.right - 10) { // side by side: right edge -> left edge
          var left = ra.left < rb.left, x1 = (left ? ra.right : ra.left) - ox, y1 = ra.top + ra.height / 2 - oy,
              x2 = (left ? rb.left : rb.right) - ox, y2 = rb.top + rb.height / 2 - oy, mx = (x1 + x2) / 2;
          pts = [[x1, y1], [mx, y1], [mx, y2], [x2, y2]];
        } else { // stacked: bottom -> top
          var down = ra.top < rb.top, sx = ra.left + ra.width / 2 - ox, sy = (down ? ra.bottom : ra.top) - oy,
              ex = rb.left + rb.width / 2 - ox, ey = (down ? rb.top : rb.bottom) - oy, my = (sy + ey) / 2;
          pts = [[sx, sy], [sx, my], [ex, my], [ex, ey]];
        }
        add('path', { 'class': 'edge', d: routePath(pts, 14) });
        add('circle', { 'class': 'station', cx: pts[0][0], cy: pts[0][1], r: 4.5 });
        add('circle', { 'class': 'station', cx: pts[3][0], cy: pts[3][1], r: 4.5 });
      });
    };
    drawEdges();
    window.addEventListener('resize', drawEdges);
    window.addEventListener('load', drawEdges);
    if (document.fonts && document.fonts.ready) { document.fonts.ready.then(drawEdges); }

    var STATUS_ICON = { ok: 'check_circle', bad: 'error', warn: 'autorenew', muted: 'schedule' };
    var describe = function (r) {
      if (r.type === 'db') {
        if (r.status === 'provisioning') { return ['Provisionando…', 'warn']; }
        if (r.status === 'failed') { return ['Falhou ao provisionar', 'bad']; }
        return r.state === 'active' ? ['Online', 'ok'] : ['Serviço ' + r.state, 'warn'];
      }
      if (r.status === 'deploying') { return ['Implantando…', 'warn']; }
      if (r.status === 'failed') { return ['Deploy falhou', 'bad']; }
      if (r.status === 'deployed') { return r.state === 'active' || r.state === 'unknown' ? ['Deploy concluído', 'ok'] : ['Serviço ' + r.state, 'warn']; }
      return ['Aguardando deploy', 'muted'];
    };
    var EV_ICON = { deploy: 'rocket_launch', database: 'storage', backup: 'backup', app: 'add_circle', cache: 'cleaning_services' };
    var setHostBar = function (id, ratio) {
      var el = document.getElementById(id);
      if (!el) { return; }
      el.style.width = Math.max(0, Math.min(1, ratio || 0)) * 100 + '%';
      el.className = ratio > 0.9 ? 'bad' : ratio > 0.75 ? 'warn' : '';
    };
    // The page says when its picture of the server is stale instead of freezing in silence.
    var syncEl = $('#sync'), lastOK = 0;
    var setSync = function (ok) {
      if (!syncEl) { return; }
      if (ok) { lastOK = Date.now(); }
      syncEl.classList.toggle('is-bad', !ok);
      $('.sync-text', syncEl).textContent = ok ? 'Ao vivo' : 'Sem conexão com o painel' + (lastOK ? ' · última leitura ' + ago(new Date(lastOK).toISOString()) : '');
    };
    var known = $$('.rcard').map(function (c) { return c.dataset.res; }).sort().join(','), reloaded = false;
    var overview = function () {
      getJSON('/api/overview').then(function (o) {
        var h = o.host;
        setText('h-mem', fmtBytes(h.mem_used) + ' / ' + fmtBytes(h.mem_total));
        setText('h-load', h.load[0].toFixed(2) + ' (' + h.cpus + ' CPU)');
        setText('h-disk', fmtBytes(h.disk_used) + ' / ' + fmtBytes(h.disk_total));
        setText('h-up', fmtDur(h.uptime));
        setHostBar('h-mem-bar', h.mem_used / h.mem_total);
        setHostBar('h-load-bar', h.load[0] / h.cpus);
        setHostBar('h-disk-bar', h.disk_used / h.disk_total);
        // something was created or removed elsewhere (CLI, another tab): the card list is stale, so load it again
        if (!reloaded && Object.keys(o.res).sort().join(',') !== known) { reloaded = true; location.reload(); return; }
        Object.keys(o.res).forEach(function (name) {
          var r = o.res[name], card = $('[data-res="' + name + '"]');
          if (!card) { return; }
          var d = describe(r), st = $('.rcard-status', card);
          st.className = 'rcard-status ' + d[1];
          $('.st-text', st).textContent = d[0];
          $('md-icon', st).textContent = STATUS_ICON[d[1]];
          $('[data-cpu]', card).textContent = fmtPct(r.cpu);
          $('[data-mem]', card).textContent = fmtBytes(r.mem);
        });
        var box = $('#events');
        if (box && o.events) {
          box.innerHTML = o.events.length ? o.events.map(function (e) {
            return '<div class="event ev-' + esc(e.kind) + '">' + icon(EV_ICON[e.kind] || 'add_circle') + '<div><strong>' + esc(e.resource) +
              '</strong><span>' + esc(e.message) + '</span><time data-ts="' + esc(e.ts) + '"></time></div></div>';
          }).join('') : '<p class="event-empty">Nenhuma atividade ainda.</p>';
          refreshTimes();
        }
        setSync(true);
        drawEdges();
      }).catch(function () { setSync(false); });
    };
    overview();
    setInterval(overview, 5000);
    document.addEventListener('visibilitychange', function () { if (!document.hidden) { overview(); } });
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

    var css = getComputedStyle(document.documentElement);
    var muted = css.getPropertyValue('--md-sys-color-on-surface-variant').trim() || '#888', grid = css.getPropertyValue('--md-sys-color-outline-variant').trim() || '#ddd', ink = css.getPropertyValue('--md-sys-color-on-surface').trim() || '#000';
    c.font = '11px Roboto, sans-serif';
    c.fillStyle = muted; c.strokeStyle = grid; c.lineWidth = 1;
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
      c.strokeStyle = muted; c.beginPath(); c.moveTo(px, T); c.lineTo(px, T + ph); c.stroke();
      c.beginPath(); c.arc(px, y(p[key]), 3.5, 0, 7); c.fillStyle = color; c.fill();
      var label = fmt(p[key]) + '  ' + ('0' + d2.getHours()).slice(-2) + ':' + ('0' + d2.getMinutes()).slice(-2) + ':' + ('0' + d2.getSeconds()).slice(-2);
      var tw = c.measureText(label).width + 12, tx = Math.min(Math.max(px - tw / 2, L), w - R - tw);
      c.fillStyle = ink; c.fillRect(tx, T + 2, tw, 18);
      c.fillStyle = css.getPropertyValue('--md-sys-color-surface').trim() || '#fff'; c.textAlign = 'left'; c.textBaseline = 'middle'; c.fillText(label, tx + 6, T + 11);
    }
  }


  // ---- metrics charts (apps and databases) ----
  var stats = $('#app-stats');
  if (stats) {
    var metricsURL = stats.dataset.metrics, range = '1h', last = null;
    var cpuC = $('#chart-cpu'), memC = $('#chart-mem');
    var render = function (hover) {
      if (!last) { return; }
      var css = getComputedStyle(document.documentElement);
      drawChart(cpuC, last.points, 'cpu', function (v) { return v.toFixed(0) + '%'; }, css.getPropertyValue('--md-sys-color-primary').trim() || '#00687b', last.from, last.to, hover);
      drawChart(memC, last.points, 'mem', fmtBytes, css.getPropertyValue('--md-sys-color-tertiary').trim() || '#575c7e', last.from, last.to, hover);
    };
    var load = function () {
      getJSON(metricsURL + '?range=' + range).then(function (m) {
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
    window.addEventListener('tabshown', function (e) { if (e.detail === 'metricas') { load(); } });
    setInterval(function () { if (!$('#tab-metricas').hidden) { load(); } }, 15000);
  }

  // ---- app page: short deploy/service logs ----
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

  // ---- full logs (tab on apps and databases) ----
  var tools = $('#logtools');
  if (tools) {
    var lbase = tools.dataset.base, kindTabs = $('#log-kinds'), kind = $('md-secondary-tab', tools).dataset.kind, timer = null, raw = '';
    var view = $('#logview'), filter = $('#log-filter'), follow = $('#log-follow'), linesSel = $('#log-lines');
    var errRe = /\b(error|erro|fatal|panic|exception|failed|falhou|traceback)\b/i, warnRe = /\b(warn|warning|aviso)\b/i;
    var url = function (extra) { return lbase + '/log?kind=' + kind + '&lines=' + linesSel.value + (extra || ''); };
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
    var schedule = function () { clearInterval(timer); if (follow.checked && !$('#tab-logs').hidden) { timer = setInterval(fetchLogs, 3000); } };
    kindTabs.addEventListener('change', function () {
      kind = $$('md-secondary-tab', kindTabs)[kindTabs.activeTabIndex].dataset.kind; fetchLogs();
    });
    linesSel.addEventListener('change', fetchLogs);
    filter.addEventListener('input', paint);
    follow.addEventListener('change', function () { schedule(); if (follow.checked) { view.scrollTop = view.scrollHeight; } });
    window.addEventListener('tabshown', function (e) { if (e.detail === 'logs') { fetchLogs(); } schedule(); });
  }

  // ---- copy / reveal / insert helpers ----
  function copy(text, btn) {
    var done = function () { if (btn) { btn.classList.add('copied'); setTimeout(function () { btn.classList.remove('copied'); }, 1200); } };
    if (navigator.clipboard && window.isSecureContext) { navigator.clipboard.writeText(text).then(done, done); return; }
    var ta = document.createElement('textarea'); ta.value = text; document.body.appendChild(ta); ta.select();
    try { document.execCommand('copy'); } catch (e) { /* ignore */ }
    ta.remove(); done();
  }
  $$('[data-copy]').forEach(function (b) {
    b.addEventListener('click', function () { copy(b.closest('.kv-actions').previousElementSibling.dataset.value, b); });
  });
  $$('[data-reveal]').forEach(function (b) {
    b.addEventListener('click', function () {
      var v = b.closest('.kv-actions').previousElementSibling, on = v.dataset.shown === '1';
      v.dataset.shown = on ? '0' : '1';
      v.textContent = on ? '••••••••••••••••' : v.dataset.value;
      $('md-icon', b).textContent = on ? 'visibility' : 'visibility_off';
    });
  });
  $$('[data-copy-text]').forEach(function (b) { b.addEventListener('click', function () { copy(b.dataset.copyText, b); }); });
  var envEditor = $('#env-editor');
  var GENERIC = ['URL', 'HOST', 'PORT', 'USER', 'PASSWORD', 'DATABASE'];
  $$('[data-insert]').forEach(function (b) {
    b.addEventListener('click', function () {
      if (!envEditor) { return; }
      var m = /\$\{\{(.+?)\.(.+?)\}\}/.exec(b.dataset.insert);
      var key = m ? (GENERIC.indexOf(m[2]) >= 0 ? m[1].toUpperCase().replace(/-/g, '_') + '_' + m[2] : m[2]) : 'VARIAVEL';
      var v = envEditor.value;
      envEditor.value = v + (v && !v.endsWith('\n') ? '\n' : '') + key + '=' + b.dataset.insert + '\n';
      envEditor.focus();
    });
  });

  // ---- database: data browser ----
  var browser = $('#databrowser');
  if (browser) {
    var list = $('#dblist'), viewBox = $('#dbview'), active = null;
    var showErr = function (el, msg) { el.innerHTML = '<div class="dbempty">' + esc(msg) + '</div>'; };
    var loadItems = function () {
      getJSON(browser.dataset.url).then(function (d) {
        if (!d.items.length) { showErr(list, 'Nada aqui ainda.'); return; }
        list.innerHTML = '';
        d.items.forEach(function (it) {
          var b = document.createElement('button');
          b.type = 'button';
          b.innerHTML = '<span class="ms">' + (browser.dataset.kind === 'redis' ? 'vpn_key' : browser.dataset.kind === 'mongodb' ? 'folder' : 'table_chart') + '</span>';
          b.appendChild(document.createTextNode(it));
          b.addEventListener('click', function () {
            if (active) { active.classList.remove('is-active'); }
            b.classList.add('is-active'); active = b;
            showErr(viewBox, 'Carregando…');
            getJSON(browser.dataset.url + '?item=' + encodeURIComponent(it)).then(function (p) {
              var html = '<div class="dbmeta">' + esc(it) + (p.total >= 0 ? ' · ' + p.total + ' registro(s)' : '') + ' · mostrando até 50</div><table><thead><tr>' +
                (p.columns || []).map(function (c) { return '<th>' + esc(c) + '</th>'; }).join('') + '</tr></thead><tbody>' +
                (p.rows || []).map(function (r) { return '<tr>' + r.map(function (c) { return c === 'NULL' ? '<td class="null">NULL</td>' : '<td title="' + esc(c) + '">' + esc(c) + '</td>'; }).join('') + '</tr>'; }).join('') + '</tbody></table>';
              viewBox.innerHTML = (p.rows && p.rows.length) ? html : '<div class="dbmeta">' + esc(it) + '</div><div class="dbempty">Sem registros.</div>';
            }).catch(function (e) { showErr(viewBox, e.message); });
          });
          list.appendChild(b);
        });
      }).catch(function (e) { showErr(list, e.message); });
    };
    window.addEventListener('tabshown', function (e) { if (e.detail === 'dados') { loadItems(); } });
  }

  // ---- database: provisioning progress ----
  var sheet = $('[data-sheet="db"]');
  if (sheet && sheet.dataset.status === 'provisioning') {
    var wait = function () {
      fetch('/databases/' + sheet.dataset.name + '/log?kind=setup&lines=50', { credentials: 'same-origin' }).then(function (r) {
        if (r.headers.get('X-Deploying') === 'true') { setTimeout(wait, 2000); } else { location.reload(); }
      }).catch(function () { setTimeout(wait, 4000); });
    };
    setTimeout(wait, 1500);
  }

  // ---- new database: install progress ----
  var inst = $('#install-box');
  if (inst) {
    var ilog = $('#install-log'), poll = function () {
      getJSON('/databases/install/status?kind=' + inst.dataset.kind).then(function (s) {
        if (s.log) { ilog.hidden = false; ilog.textContent = s.log; ilog.scrollTop = ilog.scrollHeight; }
        if (s.running) { setTimeout(poll, 2000); } else if (s.installed || s.error) { location.href = '/databases/new?kind=' + inst.dataset.kind; }
      }).catch(function () { setTimeout(poll, 4000); });
    };
    if (inst.dataset.running === 'true') { poll(); }
  }

  // ---- app form: one step at a time (without JS every step is visible) ----
  var wiz = $('#app-wizard');
  if (wiz) {
    var steps = $$('.step', wiz), stepper = $('#stepper'), backBtn = $('#wz-back'), nextBtn = $('#wz-next'), submitBtn = $('#wz-submit');
    var editing = wiz.dataset.editing === 'true', cur = 0, stepBtns = [], last = steps.length - 1;
    wiz.classList.add('wizard-on');
    [stepper, backBtn, nextBtn].forEach(function (el) { el.hidden = false; });
    var show = function (i) {
      cur = i;
      steps.forEach(function (s, j) { s.classList.toggle('is-active', j === i); });
      stepBtns.forEach(function (b, j) {
        b.classList.toggle('is-active', j === i);
        b.classList.toggle('is-done', j < i);
        if (j === i) { b.setAttribute('aria-current', 'step'); } else { b.removeAttribute('aria-current'); }
      });
      backBtn.hidden = i === 0;
      nextBtn.hidden = i === last;
      submitBtn.hidden = !editing && i !== last; // creating: only the final step can submit
    };
    // Going forward stops at the first step with an empty required field and points at it.
    var go = function (i) {
      for (var j = cur; j < i; j++) {
        var bad = $$('md-outlined-text-field[required]', steps[j]).filter(function (el) { return !el.checkValidity(); })[0];
        if (bad) { show(j); bad.reportValidity(); return; }
      }
      show(i);
    };
    steps.forEach(function (s, i) {
      var li = document.createElement('li'), b = document.createElement('button');
      b.type = 'button';
      b.innerHTML = '<span class="dot">' + (i + 1) + '</span><span class="label"></span>';
      $('.label', b).textContent = s.dataset.title;
      b.addEventListener('click', function () { go(i); });
      li.appendChild(b); stepper.appendChild(li); stepBtns.push(b);
    });
    backBtn.addEventListener('click', function () { go(cur - 1); });
    nextBtn.addEventListener('click', function () { go(cur + 1); });
    wiz.addEventListener('keydown', function (e) { // Enter in a text box moves on instead of submitting half a form
      if (e.key === 'Enter' && !editing && cur !== last && e.target.tagName === 'MD-OUTLINED-TEXT-FIELD' && e.target.type !== 'textarea') { e.preventDefault(); go(cur + 1); }
    });
    show(0);

    // The autoscale fields only matter when it is on.
    var asBox = $('#autoscale'), asFields = $('#as-fields');
    if (asBox && asFields) {
      var syncAs = function () { asFields.hidden = !asBox.checked; };
      asBox.addEventListener('change', syncAs);
      syncAs();
    }
  }

  // ---- storage tab: show only the fields the chosen kind needs ----
  var mountKind = $('#mount-kind');
  if (mountKind) {
    var srcBox = $('#mount-src-box'), contentBox = $('#mount-content-box');
    var syncMount = function () {
      srcBox.hidden = mountKind.value !== 'bind';
      contentBox.hidden = mountKind.value !== 'file';
    };
    mountKind.addEventListener('change', syncMount);
    syncMount();
  }

  // ---- site tab: a redirect needs a destination; a domain that serves the app does not ----
  var domainKind = $('#domain-kind');
  if (domainKind) {
    var toBox = $('#domain-to-box');
    var syncDomain = function () { toBox.hidden = domainKind.value !== 'redirect'; };
    domainKind.addEventListener('change', syncDomain);
    syncDomain();
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
    // "Instalar Node.js com nvm": starts the install, follows its log, then detects again.
    var wireNodeInstall = function () {
      var btn = $('#node-install'), logEl = $('#node-log');
      btn.addEventListener('click', function () {
        btn.disabled = true; logEl.hidden = false; logEl.textContent = 'Iniciando…';
        var poll = function () {
          fetch('/tools/node/status', { credentials: 'same-origin' }).then(function (r) { return r.json(); }).then(function (s) {
            logEl.textContent = s.log || 'Aguardando…';
            logEl.scrollTop = logEl.scrollHeight;
            if (s.running) { setTimeout(poll, 2000); return; }
            if (s.error) { btn.disabled = false; logEl.textContent += '\n\nFalhou: ' + s.error; return; }
            detect(); // node and npm are there now: refresh the result
          }).catch(function () { setTimeout(poll, 3000); });
        };
        fetch('/tools/node/install', { method: 'POST', credentials: 'same-origin', body: new URLSearchParams({ csrf: csrf }) })
          .then(function () { setTimeout(poll, 1000); })
          .catch(function () { btn.disabled = false; logEl.textContent = 'Falha ao iniciar a instalação.'; });
      });
    };
    // Monorepo: the apps found in the repository, to pick the folder of this one. Built with DOM
    // calls on purpose: folder names come from the repository and are not trusted.
    var monoBox = $('#mono-box'), lastName = '';
    var showApps = function (d) {
      if (!monoBox) { return; }
      var apps = d.apps || [];
      monoBox.textContent = '';
      monoBox.hidden = !apps.length;
      if (!apps.length) { return; }
      var cur = fields.root_dir ? fields.root_dir.value.trim() : '';
      var add = function (parent, label, root, current, title) {
        var b = document.createElement(current ? 'md-filled-button' : 'md-outlined-button');
        b.textContent = label;
        if (title) { b.title = title; }
        b.addEventListener('click', function () { fields.root_dir.value = root; detect(); });
        parent.appendChild(b);
      };
      var p = document.createElement('p');
      p.textContent = d.kind && !cur
        ? 'Este repositório é um monorepo: a raiz é um projeto e há outros em pastas. Sem escolher nada, esta aplicação publica a raiz; para publicar uma pasta, escolha-a:'
        : 'Este repositório tem vários projetos. Escolha o que esta aplicação vai publicar:';
      monoBox.appendChild(p);
      var list = document.createElement('div');
      list.className = 'mono-list';
      var libs = [];
      apps.forEach(function (a) {
        if (!a.deployable) { libs.push(a); return; }
        add(list, a.path + (a.kind ? ' (' + a.kind + ')' : ''), a.path, a.path === cur, a.name || '');
      });
      if (cur) { add(list, 'Repositório inteiro', '', false, 'Sem pasta: usar a raiz do repositório'); }
      monoBox.appendChild(list);
      if (libs.length) {
        var q = document.createElement('p');
        q.className = 'muted';
        q.textContent = 'Bibliotecas e pacotes compartilhados (em geral não são publicados sozinhos):';
        monoBox.appendChild(q);
        var l2 = document.createElement('div');
        l2.className = 'mono-list';
        libs.forEach(function (a) { add(l2, a.path, a.path, a.path === cur, a.name || ''); });
        monoBox.appendChild(l2);
      }
    };
    var detect = function () {
      var repo = fields.repo.value.trim();
      if (!repo) { fields.repo.reportValidity(); return; }
      detectBtn.disabled = true;
      box.hidden = false; box.className = 'detect-result'; box.textContent = 'Baixando e analisando o projeto…';
      var body = new URLSearchParams({ csrf: csrf, repo: repo, branch: fields.branch.value.trim(), root_dir: fields.root_dir ? fields.root_dir.value.trim() : '' });
      fetch('/apps/detect', { method: 'POST', credentials: 'same-origin', body: body }).then(function (r) { return r.json(); }).then(function (d) {
        detectBtn.disabled = false;
        if (d.name && (!fields.name.value || fields.name.value === lastName)) { fields.name.value = d.name; }
        lastName = d.name || lastName;
        showApps(d);
        if (d.error && !d.kind) { box.className = 'detect-result ' + (d.apps && d.apps.length ? 'warn' : 'bad'); box.textContent = d.error; return; }
        setField('build_cmd', d.build); setField('start_cmd', d.start);
        var man = $('#manual');
        if (man) { man.open = true; } // show what was filled in
        var html = 'Encontrei um projeto ' + esc(d.kind) + '.';
        if (d.build) { html += '<br>Para preparar: <code>' + esc(d.build) + '</code>'; }
        if (d.start) { html += '<br>Para iniciar: <code>' + esc(d.start) + '</code>'; }
        else { html += '<br>Não achei o comando de início. Preencha em "Ajustar à mão" ou deixe em branco para o painel tentar no primeiro deploy.'; }
        (d.notes || []).forEach(function (n) { html += '<br>' + esc(n); });
        var warn = false;
        var needsNode = false;
        var envReq = (d.env_vars || []).filter(function (v) { return v.required; });
        if (envReq.length) {
          warn = true;
          html += '<br>O projeto usa variáveis de ambiente que você precisa preencher na aba Variáveis depois de criar a aplicação: ' +
            envReq.map(function (v) { return '<code>' + esc(v.name) + '</code>'; }).join(', ') + '.';
        }
        if (d.missing && d.missing.length) {
          warn = true;
          html += '<br>Falta instalar no servidor: ' + d.missing.map(esc).join(', ') + '.';
          needsNode = d.missing.some(function (m) { return /^(node|npm|npx|pnpm|yarn) /.test(m); });
          if (needsNode) {
            html += '<br><md-filled-button id="node-install">Instalar Node.js com nvm</md-filled-button><pre class="detect-log" id="node-log" hidden></pre>';
          }
        }
        box.className = 'detect-result' + (warn || !d.start ? ' warn' : '');
        box.innerHTML = html;
        if (needsNode) { wireNodeInstall(); }
      }).catch(function () { detectBtn.disabled = false; box.className = 'detect-result bad'; box.textContent = 'Não consegui analisar o projeto. Confira o endereço do repositório e tente de novo.'; });
    };
    detectBtn.addEventListener('click', detect);

    // Repositories visible to the GitHub App: pick one to fill repository and branch.
    var picker = $('#repo-search');
    if (picker) {
      var status = $('#repo-status'), byName = {}, picked = '';
      fetch('/api/github/repos', { credentials: 'same-origin' }).then(function (r) {
        return r.json().then(function (d) { return { ok: r.ok, d: d }; });
      }).then(function (x) {
        if (!x.ok) { status.textContent = x.d.error || 'Não foi possível listar os repositórios.'; return; }
        var list = $('#repo-list');
        x.d.repos.forEach(function (r) {
          byName[r.full_name] = r;
          var o = document.createElement('option');
          o.value = r.full_name;
          if (r.private) { o.label = 'privado'; }
          list.appendChild(o);
        });
        picker.disabled = !x.d.repos.length;
        status.textContent = x.d.repos.length
          ? x.d.repos.length + ' repositório(s) disponíveis. Ou digite a URL abaixo.'
          : 'O GitHub App ainda não está instalado em nenhum repositório. Instale-o na página GitHub.';
      }).catch(function () { status.textContent = 'Não foi possível listar os repositórios.'; });
      var pick = function () {
        var r = byName[picker.value.trim()];
        if (!r || picked === r.full_name) { return; }
        picked = r.full_name;
        fields.repo.value = r.clone_url;
        fields.branch.value = r.default_branch || 'main';
        fields.repo.dispatchEvent(new Event('change')); // detects the project when the commands are empty
      };
      picker.addEventListener('input', pick);
      picker.addEventListener('change', pick);
    }
    fields.repo.addEventListener('change', function () {
      if (!fields.start_cmd.value && !fields.build_cmd.value) { detect(); }
    });
    fields.root_dir.addEventListener('change', function () {
      if (!fields.start_cmd.value && !fields.build_cmd.value) { detect(); }
    });
  }

  // ---- assistant (aba IA): the browser keeps the conversation, the server streams JSON lines ----
  var aiBox = $('#ai');
  if (aiBox) {
    var aiLog = $('#ai-log'), aiForm = $('#ai-form'), aiSend = $('#ai-send');
    var aiCsrf = $('input[name=csrf]').value, aiHist = [], aiBusy = false, aiCur = null, aiPend = [], aiOut = {};
    var mk = function (tag, cls, text) {
      var e = document.createElement(tag);
      if (cls) { e.className = cls; }
      if (text) { e.textContent = text; }
      return e;
    };
    var aiBottom = function () { aiLog.scrollTop = aiLog.scrollHeight; };
    var aiAdd = function (cls, text) { var d = mk('div', 'ai-msg ' + cls, text); aiLog.appendChild(d); aiBottom(); return d; };
    // A small, safe markdown: code fences, `code` and **bold**, built as DOM nodes (never innerHTML).
    var aiRender = function (el, text) {
      el.textContent = '';
      var parts = text.split(/```[a-z]*\n?/);
      parts.forEach(function (part, i) {
        if (i % 2) { el.appendChild(mk('pre', 'codeblock', part.replace(/\n$/, ''))); return; }
        // the blank lines around a fence would double the gap the block already has
        if (i > 0) { part = part.replace(/^\n+/, ''); }
        if (i < parts.length - 1) { part = part.replace(/\n+$/, ''); }
        part.split(/(`[^`\n]+`|\*\*[^*\n]+\*\*)/).forEach(function (tok) {
          if (/^`[^`]+`$/.test(tok)) { el.appendChild(mk('code', '', tok.slice(1, -1))); }
          else if (/^\*\*[^*]+\*\*$/.test(tok)) { el.appendChild(mk('strong', '', tok.slice(2, -2))); }
          else if (tok) { el.appendChild(document.createTextNode(tok)); }
        });
      });
    };
    // Streaming repaints at most once per frame.
    var aiPaint = function (el) {
      if (el.painting) { return; }
      el.painting = true;
      requestAnimationFrame(function () { el.painting = false; aiRender(el, el.raw); aiBottom(); });
    };
    var aiHandle = function (line) {
      if (!line.trim()) { return; }
      var e;
      try { e = JSON.parse(line); } catch (x) { return; }
      if (e.t === 'text') {
        if (!aiCur) { aiCur = aiAdd('ai-msg--assistant ai-msg--live', ''); }
        aiCur.raw = (aiCur.raw || '') + e.text;
        aiPaint(aiCur);
      } else if (e.t === 'call') {
        if (aiCur && !aiCur.raw) { aiCur.remove(); }
        aiCur = null;
        var box = mk('details', 'ai-call'), sum = mk('summary', '', e.cmd || e.name), out = mk('pre', 'codeblock', 'executando…');
        box.appendChild(sum); box.appendChild(out); aiLog.appendChild(box);
        aiOut[e.id] = { box: box, out: out, sum: sum };
        aiBottom();
      } else if (e.t === 'result') {
        var o = aiOut[e.id];
        if (o) { o.out.textContent = e.text || '(sem saída)'; o.box.classList.toggle('ai-call--bad', /^(ERRO|RECUSADO)/.test(e.text)); }
      } else if (e.t === 'confirm') {
        aiPend.push(e);
        if (aiOut[e.id]) { aiOut[e.id].out.textContent = 'aguardando a sua aprovação'; }
      } else if (e.t === 'state') {
        aiHist = e.messages || aiHist;
      } else if (e.t === 'error') {
        aiAdd('ai-msg--error', e.text);
      }
    };
    var aiAsk = function () {
      var ask = mk('div', 'ai-msg ai-ask');
      ask.appendChild(mk('p', '', 'O assistente quer executar:'));
      aiPend.forEach(function (p) { ask.appendChild(mk('pre', 'codeblock', p.cmd)); });
      var row = mk('div', 'actions actions--start');
      var answer = function (yes) {
        var map = {};
        aiPend.forEach(function (p) { map[p.id] = yes; });
        ask.remove();
        aiPost(map);
      };
      var no = mk('md-text-button', '', 'Recusar'), yes = mk('md-filled-button', '', 'Aprovar e executar');
      no.addEventListener('click', function () { answer(false); });
      yes.addEventListener('click', function () { answer(true); });
      row.appendChild(no); row.appendChild(yes); ask.appendChild(row);
      aiLog.appendChild(ask); aiBottom();
    };
    var aiPost = function (approve) {
      aiBusy = true; aiSend.disabled = true; aiPend = [];
      aiCur = aiAdd('ai-msg--assistant ai-msg--live', ''); // the blinking cursor shows the model is working
      var body = new URLSearchParams({ csrf: aiCsrf, payload: JSON.stringify({ messages: aiHist, approve: approve || {} }) });
      return fetch('/ai/chat', { method: 'POST', credentials: 'same-origin', body: body }).then(function (resp) {
        var type = resp.headers.get('Content-Type') || '';
        if (type.indexOf('ndjson') < 0) {
          if (type.indexOf('json') < 0) { throw new Error('Sessão expirada: recarregue a página.'); }
          return resp.json().then(function (j) { throw new Error(j.error || ('Erro ' + resp.status)); });
        }
        var reader = resp.body.getReader(), dec = new TextDecoder(), buf = '';
        var pump = function () {
          return reader.read().then(function (r) {
            if (r.done) { aiHandle(buf); return; }
            buf += dec.decode(r.value, { stream: true });
            var lines = buf.split('\n');
            buf = lines.pop();
            lines.forEach(aiHandle);
            return pump();
          });
        };
        return pump();
      }).catch(function (err) {
        aiAdd('ai-msg--error', err.message || 'Falha ao falar com o servidor.');
      }).then(function () {
        aiBusy = false; aiSend.disabled = false;
        if (aiCur && !aiCur.raw) { aiCur.remove(); }
        $$('.ai-msg--live', aiLog).forEach(function (m) { m.classList.remove('ai-msg--live'); });
        if (aiPend.length) { aiAsk(); }
      });
    };
    aiForm.addEventListener('submit', function (ev) {
      ev.preventDefault();
      var text = (fields.ai_prompt.value || '').trim();
      if (!text || aiBusy) { return; }
      fields.ai_prompt.value = '';
      aiHist.push({ role: 'user', text: text });
      aiAdd('ai-msg--user', text);
      aiPost();
    });
    fields.ai_prompt.addEventListener('keydown', function (ev) {
      if (ev.key === 'Enter' && (ev.ctrlKey || ev.metaKey)) { ev.preventDefault(); aiForm.requestSubmit(); }
    });
    $('#ai-clear').addEventListener('click', function () {
      if (aiBusy) { return; }
      aiHist = []; aiPend = []; aiOut = {}; aiLog.textContent = '';
    });
  }

  // ---- a plain confirmation fades away on its own (errors, tokens and anything with a button stay) ----
  var flash = $('.notice[role=status]');
  if (flash && !$('pre, button, a, md-filled-button, md-text-button', flash)) {
    setTimeout(function () {
      flash.classList.add('is-leaving');
      setTimeout(function () { flash.remove(); }, 400);
    }, 7000);
  }

  // ---- pages waiting for a background job reload themselves ----
  var refresher = $('[data-refresh]');
  if (refresher) { setTimeout(function () { location.reload(); }, (+refresher.dataset.refresh || 5) * 1000); }

  // ---- initial tab (after all 'tabshown' listeners exist) ----
  if (tabsEl) {
    if (location.hash.length > 1 && tabNames.indexOf(location.hash.slice(1)) >= 0) { showTab(location.hash.slice(1), false); }
    else { window.dispatchEvent(new CustomEvent('tabshown', { detail: tabNames[0] })); }
  }
})();
