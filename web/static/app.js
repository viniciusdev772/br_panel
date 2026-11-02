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
    var cb = $('.mdc-checkbox', el);
    if (!cb) { return; }
    var ff = new mdc.formField.MDCFormField(el);
    ff.input = new mdc.checkbox.MDCCheckbox(cb);
  });
  $$('form[data-confirm]').forEach(function (f) {
    f.addEventListener('submit', function (e) {
      if (!window.confirm(f.getAttribute('data-confirm'))) { e.preventDefault(); }
    });
  });

  // ---- theme ----
  var themeBtn = $('#theme-toggle');
  if (themeBtn) {
    themeBtn.addEventListener('click', function () {
      var next = document.documentElement.getAttribute('data-theme') === 'light' ? 'dark' : 'light';
      document.documentElement.setAttribute('data-theme', next);
      try { localStorage.setItem('brpanel-theme', next); } catch (e) { /* storage blocked */ }
      window.dispatchEvent(new Event('resize')); // charts and edges pick up the new colors
    });
  }

  // ---- create menu ----
  var createBtn = $('#create-btn'), createMenu = $('#create-menu');
  if (createBtn && createMenu) {
    var closeMenu = function () { createMenu.classList.remove('open'); createBtn.setAttribute('aria-expanded', 'false'); };
    createBtn.addEventListener('click', function (e) {
      e.stopPropagation();
      var open = createMenu.classList.toggle('open');
      createBtn.setAttribute('aria-expanded', String(open));
    });
    document.addEventListener('click', function (e) { if (!createMenu.contains(e.target)) { closeMenu(); } });
    document.addEventListener('keydown', function (e) { if (e.key === 'Escape') { closeMenu(); } });
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
  function showTab(name, updateHash) {
    var found = false;
    $$('[data-panel]').forEach(function (p) { var on = p.dataset.panel === name; p.hidden = !on; found = found || on; });
    if (!found) { return; }
    $$('.tab-btn', tabsEl).forEach(function (b) { b.classList.toggle('is-active', b.dataset.tab === name); });
    if (updateHash) { try { history.replaceState(null, '', '#' + name); } catch (e) { /* ignore */ } }
    window.dispatchEvent(new CustomEvent('tabshown', { detail: name }));
  }
  if (tabsEl) {
    $$('.tab-btn', tabsEl).forEach(function (b) { b.addEventListener('click', function () { showTab(b.dataset.tab, true); }); });
    window.addEventListener('hashchange', function () { showTab(location.hash.slice(1), false); });
  }

  // ---- architecture canvas ----
  var canvas = $('#canvas');
  if (canvas) {
    var edgesSvg = $('#edges');
    var drawEdges = function () {
      if (!edgesSvg) { return; }
      $$('path.edge', edgesSvg).forEach(function (p) { p.remove(); });
      var base = canvas.getBoundingClientRect();
      (canvas.dataset.edges || '').split(';').filter(Boolean).forEach(function (pair) {
        var ft = pair.split('>'), a = $('[data-res="' + ft[0] + '"]'), b = $('[data-res="' + ft[1] + '"]');
        if (!a || !b) { return; }
        var ra = a.getBoundingClientRect(), rb = b.getBoundingClientRect(), d;
        var ox = base.left, oy = base.top;
        if (rb.left >= ra.right - 10 || ra.left >= rb.right - 10) { // side by side: right edge -> left edge
          var left = ra.left < rb.left, x1 = (left ? ra.right : ra.left) - ox, y1 = ra.top + ra.height / 2 - oy,
              x2 = (left ? rb.left : rb.right) - ox, y2 = rb.top + rb.height / 2 - oy, dx = (x2 - x1) / 2;
          d = 'M' + x1 + ',' + y1 + ' C' + (x1 + dx) + ',' + y1 + ' ' + (x2 - dx) + ',' + y2 + ' ' + x2 + ',' + y2;
        } else { // stacked: bottom -> top
          var down = ra.top < rb.top, sx = ra.left + ra.width / 2 - ox, sy = (down ? ra.bottom : ra.top) - oy,
              ex = rb.left + rb.width / 2 - ox, ey = (down ? rb.top : rb.bottom) - oy, dy = (ey - sy) / 2;
          d = 'M' + sx + ',' + sy + ' C' + sx + ',' + (sy + dy) + ' ' + ex + ',' + (ey - dy) + ' ' + ex + ',' + ey;
        }
        var path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
        path.setAttribute('class', 'edge'); path.setAttribute('d', d); path.setAttribute('marker-end', 'url(#arrow)');
        edgesSvg.appendChild(path);
      });
    };
    drawEdges();
    window.addEventListener('resize', drawEdges);
    window.addEventListener('load', drawEdges);

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
    var EV_ICON = { deploy: 'rocket_launch', database: 'storage', backup: 'backup', app: 'add_circle' };
    var overview = function () {
      getJSON('/api/overview').then(function (o) {
        var h = o.host;
        setText('h-mem', fmtBytes(h.mem_used) + ' / ' + fmtBytes(h.mem_total));
        setText('h-load', h.load[0].toFixed(2) + ' (' + h.cpus + ' CPU)');
        setText('h-disk', fmtBytes(h.disk_used) + ' / ' + fmtBytes(h.disk_total));
        setText('h-up', fmtDur(h.uptime));
        Object.keys(o.res).forEach(function (name) {
          var r = o.res[name], card = $('[data-res="' + name + '"]');
          if (!card) { return; }
          var d = describe(r), st = $('.rcard-status', card);
          st.className = 'rcard-status ' + d[1];
          $('.st-text', st).textContent = d[0];
          $('.material-icons', st).textContent = STATUS_ICON[d[1]];
          $('[data-cpu]', card).textContent = fmtPct(r.cpu);
          $('[data-mem]', card).textContent = fmtBytes(r.mem);
        });
        var box = $('#events');
        if (box && o.events) {
          box.innerHTML = o.events.length ? o.events.map(function (e) {
            return '<div class="event ev-' + esc(e.kind) + '"><i class="material-icons">' + (EV_ICON[e.kind] || 'add_circle') + '</i><div><strong>' + esc(e.resource) +
              '</strong><span>' + esc(e.message) + '</span><time data-ts="' + esc(e.ts) + '"></time></div></div>';
          }).join('') : '<p class="event-empty">Nenhuma atividade ainda.</p>';
          refreshTimes();
        }
        drawEdges();
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

    var css = getComputedStyle(document.documentElement);
    var muted = css.getPropertyValue('--muted').trim() || '#888', grid = css.getPropertyValue('--border').trim() || '#ddd', ink = css.getPropertyValue('--text').trim() || '#000';
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
      c.fillStyle = css.getPropertyValue('--surface').trim() || '#fff'; c.textAlign = 'left'; c.textBaseline = 'middle'; c.fillText(label, tx + 6, T + 11);
    }
  }


  // ---- metrics charts (apps and databases) ----
  var stats = $('#app-stats');
  if (stats) {
    var metricsURL = stats.dataset.metrics, range = '1h', last = null;
    var cpuC = $('#chart-cpu'), memC = $('#chart-mem');
    var render = function (hover) {
      if (!last) { return; }
      drawChart(cpuC, last.points, 'cpu', function (v) { return v.toFixed(0) + '%'; }, '#8b5cf6', last.from, last.to, hover);
      drawChart(memC, last.points, 'mem', fmtBytes, '#34d399', last.from, last.to, hover);
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
    var lbase = tools.dataset.base, kind = $('.sw-tab', tools).dataset.kind, timer = null, raw = '';
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
    $$('.sw-tab', tools).forEach(function (b) {
      b.addEventListener('click', function () {
        $$('.sw-tab', tools).forEach(function (o) { o.classList.toggle('is-active', o === b); });
        kind = b.dataset.kind; fetchLogs();
      });
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
      $('.material-icons', b).textContent = on ? 'visibility' : 'visibility_off';
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
          b.innerHTML = '<i class="material-icons">' + (browser.dataset.kind === 'redis' ? 'vpn_key' : browser.dataset.kind === 'mongodb' ? 'folder' : 'table_chart') + '</i>';
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
        var needsNode = false;
        if (d.missing && d.missing.length) {
          warn = true;
          html += '<br><b>Faltam no servidor:</b> ' + d.missing.map(esc).join(', ');
          needsNode = d.missing.some(function (m) { return /^(node|npm|npx|pnpm|yarn) /.test(m); });
          if (needsNode) {
            html += '<br><button type="button" class="mdc-button mdc-button--raised" id="node-install">Instalar Node.js com nvm</button><pre class="detect-log" id="node-log" hidden></pre>';
          }
        }
        box.className = 'detect-result' + (warn || !d.start ? ' warn' : '');
        box.innerHTML = html;
        if (needsNode) { wireNodeInstall(); }
      }).catch(function () { detectBtn.disabled = false; box.className = 'detect-result bad'; box.textContent = 'Falha ao detectar.'; });
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
        $('input[name=repo]').dispatchEvent(new Event('change')); // detects the project when the commands are empty
      };
      picker.addEventListener('input', pick);
      picker.addEventListener('change', pick);
    }
    $('input[name=repo]').addEventListener('change', function () {
      if (!fields.start_cmd.value && !fields.build_cmd.value) { detect(); }
    });
  }

  // ---- pages waiting for a background job reload themselves ----
  var refresher = $('[data-refresh]');
  if (refresher) { setTimeout(function () { location.reload(); }, (+refresher.dataset.refresh || 5) * 1000); }

  // ---- initial tab (after all 'tabshown' listeners exist) ----
  if (tabsEl) {
    if (location.hash.length > 1) { showTab(location.hash.slice(1), false); }
    else { window.dispatchEvent(new CustomEvent('tabshown', { detail: $('.tab-btn.is-active', tabsEl).dataset.tab })); }
  }
})();
