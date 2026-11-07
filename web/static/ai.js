// The assistant: the full page (/ai) and the floating help sheet share this code. The browser keeps the
// conversation and sends it back each time; the server streams JSON lines (text, call, result, confirm, state, error).
(function () {
  'use strict';
  var $ = function (s, r) { return (r || document).querySelector(s); };
  var $$ = function (s, r) { return Array.prototype.slice.call((r || document).querySelectorAll(s)); };
  var csrfEl = $('input[name=csrf]');
  var csrf = csrfEl ? csrfEl.value : '';

  var mk = function (tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) { e.className = cls; }
    if (text) { e.textContent = text; }
    return e;
  };
  var icon = function (name, slot) {
    var i = mk('md-icon', '', name);
    if (slot) { i.slot = slot; }
    i.setAttribute('aria-hidden', 'true');
    return i;
  };

  // A small, safe markdown: code fences, `code` and **bold**, built as DOM nodes (never innerHTML).
  function render(el, text) {
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
  }

  // Which screen the user is on, in words, and what is worth asking there.
  function pageInfo() {
    var p = location.pathname.replace(/\/+$/, '') || '/', m;
    if ((m = /^\/apps\/([^/]+)\/edit$/.exec(p))) { return { label: 'Editando ' + m[1], ask: formHelp() }; }
    if (p === '/apps/new') { return { label: 'Nova aplicação', ask: formHelp() }; }
    if ((m = /^\/apps\/([^/]+)$/.exec(p))) {
      var a = m[1];
      return { label: 'Aplicação ' + a, ask: [
        { i: 'bug_report', t: 'Por que o deploy falhou?', q: 'Analise o último deploy da aplicação "' + a + '": leia o log do deploy, ache a causa e diga como corrigir.' },
        { i: 'speed', t: 'O site está no ar?', q: 'Teste se a aplicação "' + a + '" responde e me diga o estado dela (serviço, porta, logs recentes).' },
        { i: 'vpn_key', t: 'Revisar variáveis', q: 'Veja se a aplicação "' + a + '" tem as variáveis de ambiente que o código precisa e se alguma está faltando.' },
        { i: 'language', t: 'Domínio e SSL', q: 'Me ajude a configurar o domínio e o SSL da aplicação "' + a + '": confira o DNS e diga o que falta.' }
      ] };
    }
    if ((m = /^\/databases\/([^/]+)$/.exec(p)) && m[1] !== 'new') {
      var d = m[1];
      return { label: 'Banco ' + d, ask: [
        { i: 'link', t: 'Como conecto um app?', q: 'Como conecto uma aplicação ao banco "' + d + '"? Mostre a variável de ambiente certa para colar.' },
        { i: 'check_circle', t: 'O banco está saudável?', q: 'Veja o estado e os logs do banco "' + d + '" e me diga se há algo errado.' }
      ] };
    }
    if (p === '/settings') { return { label: 'Configurações', ask: [{ i: 'language', t: 'Domínio e SSL do painel', q: 'Me explique como configurar o domínio e o SSL do painel, passo a passo.' }] }; }
    return { label: 'Servidor', ask: [
      { i: 'monitoring', t: 'Resumo do servidor', q: 'Dê um resumo do servidor: aplicações, bancos, o que está com problema e o que eu devo olhar primeiro.' },
      { i: 'rocket_launch', t: 'Publicar um projeto', q: 'Quero publicar um projeto novo a partir de um repositório Git. Me guie e pergunte só o que for preciso.' },
      { i: 'bug_report', t: 'Algo está com problema?', q: 'Veja todas as aplicações e bancos e me diga se algum está com problema, com a causa provável.' }
    ] };
  }
  function formHelp() {
    return [
      { i: 'smart_toy', t: 'Como preencho os comandos?', q: 'Estou criando uma aplicação. Explique o que vai em "comando de preparação" e "comando para iniciar" e como usar $PORT, com exemplos do meu tipo de projeto.' },
      { i: 'dns', t: 'Qual porta uso?', q: 'Como o painel escolhe a porta e como meu código precisa lê-la (variável PORT)?' },
      { i: 'vpn_key', t: 'Variáveis e banco', q: 'Como ligo um banco do painel a esta aplicação usando variáveis de ambiente?' }
    ];
  }

  // mount wires one chat: cfg = {log, form, input, send, clear, busy, key, suggest, ctx}.
  function mount(cfg) {
    var log = $(cfg.log), form = $(cfg.form), input = $(cfg.input), send = $(cfg.send);
    if (!log || !form || !input) { return null; }
    var hist = [], busy = false, cur = null, pend = [], outs = {};
    var busyBar = cfg.busy ? $(cfg.busy) : null;
    var intro = cfg.intro ? $(cfg.intro) : null;
    var bottom = function () { log.scrollTop = log.scrollHeight; };
    var add = function (cls, text) { var d = mk('div', 'ai-msg ' + cls, text); log.appendChild(d); bottom(); return d; };
    var setBusy = function (on) {
      busy = on;
      if (send) { send.disabled = on; }
      if (busyBar) { busyBar.hidden = !on; }
    };
    var save = function () {
      if (!cfg.key) { return; }
      try { sessionStorage.setItem(cfg.key, JSON.stringify(hist)); } catch (e) { /* storage blocked or full */ }
    };
    var hideIntro = function () { if (intro) { intro.hidden = true; } };

    // Streaming repaints at most once per frame.
    var paint = function (el) {
      if (el.painting) { return; }
      el.painting = true;
      requestAnimationFrame(function () { el.painting = false; render(el, el.raw); bottom(); });
    };
    var callBox = function (id, label) {
      var box = mk('details', 'ai-call'), sum = mk('summary', ''), out = mk('pre', 'codeblock', 'executando…');
      sum.appendChild(icon('terminal'));
      sum.appendChild(mk('span', '', label));
      box.appendChild(sum); box.appendChild(out); log.appendChild(box);
      outs[id] = { box: box, out: out };
      return outs[id];
    };
    var handle = function (line) {
      if (!line.trim()) { return; }
      var e;
      try { e = JSON.parse(line); } catch (x) { return; }
      if (e.t === 'text') {
        if (!cur) { cur = add('ai-msg--assistant ai-msg--live', ''); }
        cur.raw = (cur.raw || '') + e.text;
        paint(cur);
      } else if (e.t === 'call') {
        if (cur && !cur.raw) { cur.remove(); }
        cur = null;
        callBox(e.id, e.cmd || e.name);
        bottom();
      } else if (e.t === 'result') {
        var o = outs[e.id];
        if (o) { o.out.textContent = e.text || '(sem saída)'; o.box.classList.toggle('ai-call--bad', /^(ERRO|RECUSADO)/.test(e.text)); }
      } else if (e.t === 'confirm') {
        pend.push(e);
        if (outs[e.id]) { outs[e.id].out.textContent = 'aguardando a sua aprovação'; }
      } else if (e.t === 'state') {
        hist = e.messages || hist;
        save();
      } else if (e.t === 'error') {
        add('ai-msg--error', e.text);
      }
    };
    // Changes to the server wait for a yes, in a card with the exact commands.
    var ask = function () {
      var card = mk('div', 'ai-ask');
      var head = mk('div', 'ai-ask-head');
      head.appendChild(icon('security'));
      head.appendChild(mk('span', '', pend.length > 1 ? 'O assistente quer executar ' + pend.length + ' ações' : 'O assistente quer executar uma ação'));
      card.appendChild(head);
      pend.forEach(function (p) { card.appendChild(mk('pre', 'codeblock', p.cmd)); });
      var row = mk('div', 'actions');
      var answer = function (yes) {
        var map = {};
        pend.forEach(function (p) { map[p.id] = yes; });
        card.remove();
        post(map);
      };
      var no = mk('md-text-button', '', 'Recusar'), yes = mk('md-filled-button', '', 'Aprovar e executar');
      no.setAttribute('type', 'button'); yes.setAttribute('type', 'button');
      yes.appendChild(icon('check', 'icon'));
      no.addEventListener('click', function () { answer(false); });
      yes.addEventListener('click', function () { answer(true); });
      row.appendChild(no); row.appendChild(yes); card.appendChild(row);
      log.appendChild(card); bottom();
    };
    var post = function (approve) {
      setBusy(true); pend = [];
      cur = add('ai-msg--assistant ai-msg--live', ''); // the blinking cursor shows the model is working
      var payload = { messages: hist, approve: approve || {}, page: location.pathname };
      var body = new URLSearchParams({ csrf: csrf, payload: JSON.stringify(payload) });
      return fetch('/ai/chat', { method: 'POST', credentials: 'same-origin', body: body }).then(function (resp) {
        var type = resp.headers.get('Content-Type') || '';
        if (type.indexOf('ndjson') < 0) {
          if (type.indexOf('json') < 0) { throw new Error('Sessão expirada: recarregue a página.'); }
          return resp.json().then(function (j) { throw new Error(j.error || ('Erro ' + resp.status)); });
        }
        var reader = resp.body.getReader(), dec = new TextDecoder(), buf = '';
        var pump = function () {
          return reader.read().then(function (r) {
            if (r.done) { handle(buf); return; }
            buf += dec.decode(r.value, { stream: true });
            var lines = buf.split('\n');
            buf = lines.pop();
            lines.forEach(handle);
            return pump();
          });
        };
        return pump();
      }).catch(function (err) {
        add('ai-msg--error', err.message || 'Falha ao falar com o servidor.');
      }).then(function () {
        setBusy(false);
        if (cur && !cur.raw) { cur.remove(); }
        $$('.ai-msg--live', log).forEach(function (m) { m.classList.remove('ai-msg--live'); });
        if (pend.length) { ask(); }
      });
    };
    var say = function (text) {
      text = (text || '').trim();
      if (!text || busy) { return; }
      hideIntro();
      hist.push({ role: 'user', text: text });
      add('ai-msg--user', text);
      return post();
    };
    var clear = function () {
      if (busy) { return; }
      hist = []; pend = []; outs = {}; log.textContent = '';
      if (intro) { intro.hidden = false; }
      save();
    };
    // Rebuilds the screen from a saved conversation (a calls still waiting for approval are dropped).
    var restore = function () {
      if (!cfg.key) { return; }
      var saved;
      try { saved = JSON.parse(sessionStorage.getItem(cfg.key) || '[]'); } catch (e) { saved = []; }
      if (!Array.isArray(saved) || !saved.length) { return; }
      var last = saved[saved.length - 1];
      if (last && last.role === 'assistant' && (last.calls || []).some(function (c) { return !c.done; })) { saved.pop(); }
      hist = saved;
      hideIntro();
      hist.forEach(function (m) {
        if (m.role === 'user') { add('ai-msg--user', m.text); return; }
        if (m.text) { var d = add('ai-msg--assistant', ''); render(d, m.text); }
        (m.calls || []).forEach(function (c) {
          var args = c.args && typeof c.args === 'object' ? JSON.stringify(c.args) : '';
          var o = callBox(c.id, c.name + (args && args !== '{}' ? ' ' + args : ''));
          o.out.textContent = c.out || '(sem saída)';
          o.box.classList.toggle('ai-call--bad', /^(ERRO|RECUSADO)/.test(c.out || ''));
        });
      });
      bottom();
    };

    form.addEventListener('submit', function (ev) {
      ev.preventDefault();
      var text = input.value;
      input.value = '';
      say(text);
    });
    input.addEventListener('keydown', function (ev) {
      if (ev.key === 'Enter' && (ev.ctrlKey || ev.metaKey || (cfg.enterSends && !ev.shiftKey))) { ev.preventDefault(); form.requestSubmit(); }
    });
    var clearBtn = cfg.clear ? $(cfg.clear) : null;
    if (clearBtn) { clearBtn.addEventListener('click', clear); }
    restore();
    return { say: say, clear: clear, focus: function () { input.focus(); }, busy: function () { return busy; } };
  }

  // ---- the assistant page ----
  if ($('#ai')) { mount({ log: '#ai-log', form: '#ai-form', input: '#f-ai_prompt', send: '#ai-send', clear: '#ai-clear' }); }

  // ---- the floating help sheet ----
  var fab = $('#ai-fab'), sheet = $('#ai-sheet');
  if (!fab || !sheet) { return; }
  var scrim = $('#ai-scrim'), chat = null, wired = false;
  var info = pageInfo();
  var ctxEl = $('#ai-sheet-ctx');
  if (ctxEl) { ctxEl.textContent = info.label; }
  var setOpen = function (open) {
    sheet.hidden = !open;
    if (scrim) { scrim.hidden = !open; }
    fab.hidden = open && window.matchMedia('(max-width: 599.98px)').matches;
    fab.setAttribute('aria-expanded', String(open));
    try { sessionStorage.setItem('brpanel-ai-open', open ? '1' : '0'); } catch (e) { /* storage blocked */ }
    if (!open) { fab.focus(); return; }
    if (!wired) {
      wired = true;
      chat = mount({ log: '#ai-sheet-log', form: '#ai-sheet-form', input: '#ai-sheet-input', send: '#ai-sheet-send', clear: '#ai-sheet-clear', busy: '#ai-sheet-busy', intro: '#ai-intro', key: 'brpanel-ai-sheet', enterSends: true });
      var box = $('#ai-suggest');
      if (box && chat) {
        info.ask.forEach(function (s) {
          var chip = mk('md-assist-chip', '');
          chip.setAttribute('label', s.t);
          chip.appendChild(icon(s.i, 'icon'));
          chip.addEventListener('click', function () { chat.say(s.q); });
          box.appendChild(chip);
        });
      }
    }
    if (chat) { chat.focus(); }
  };
  fab.addEventListener('click', function () { setOpen(sheet.hidden); });
  var closeBtn = $('#ai-sheet-close');
  if (closeBtn) { closeBtn.addEventListener('click', function () { setOpen(false); }); }
  if (scrim) { scrim.addEventListener('click', function () { setOpen(false); }); }
  document.addEventListener('keydown', function (e) { if (e.key === 'Escape' && !sheet.hidden) { setOpen(false); } });
  var wasOpen = false;
  try { wasOpen = sessionStorage.getItem('brpanel-ai-open') === '1'; } catch (e) { /* storage blocked */ }
  if (wasOpen && !window.matchMedia('(max-width: 599.98px)').matches) { setOpen(true); }
})();
