// Drives the panel through every screen and saves PNGs. Configured by run.sh via environment:
//   PANEL_URL, WORK (temp dir with data/ stubs/ ...), OUT (output dir), SEED, SEED_DATA (python helpers)
const { chromium } = require('playwright');
const fs = require('fs');
const { execFileSync } = require('child_process');

const { PANEL_URL: B, WORK, OUT, SEED, SEED_DATA } = process.env;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const PASSWORD = 'senha-muito-longa-123';
const SUBMIT = 'main form button[type=submit]'; // the top bar also has a (logout) submit button

const T0 = Date.now();
(async () => {
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1360, height: 820 } });
  const page = await ctx.newPage();
  page.setDefaultTimeout(4000); // a missing element means "not here": fail fast, never stall the run
  const shot = async (name, p = page, full = true) => {
    await sleep(250); // let canvas charts and transitions finish drawing
    try {
      await p.screenshot({ path: `${OUT}/${name}.png`, fullPage: full, animations: 'disabled', timeout: 30000 });
      console.log('ok', name);
    } catch (e) { console.log('pulado:', name, '-', e.message.split('\n')[0]); }
  };
  const idle = () => page.waitForLoadState('networkidle');
  // Fields can sit in collapsed sections or other wizard steps: set them straight in the DOM.
  const setv = (n, v) => page.evaluate(([n, v]) => { const e = document.querySelector('[name=' + n + ']'); e.value = v; e.dispatchEvent(new Event('input', { bubbles: true })); }, [n, v]);
  const send = () => page.evaluate(() => document.querySelector('main form').requestSubmit());
  const tab = async (name) => { await page.click(`.tab-btn[data-tab="${name}"]`); await sleep(200); };

  // ---- first run ----
  const token = fs.readFileSync(`${WORK}/data/setup_token`, 'utf8').trim();
  await page.goto(B + '/'); await page.waitForURL('**/setup'); await idle();
  await shot('01-setup');
  await page.fill('[name=token]', token); await page.fill('[name=username]', 'admin');
  await page.fill('[name=password]', PASSWORD); await page.fill('[name=password2]', PASSWORD);
  await page.click(SUBMIT); await idle();
  await shot('02-arquitetura-vazia');

  // ---- settings (point the system dirs at the temp area) ----
  await page.goto(B + '/settings'); await idle();
  await setv('nginx_dir', `${WORK}/nginx`); await setv('acme_root', `${WORK}/acme`);
  await setv('cert_dir', `${WORK}/certs`); await setv('unit_dir', `${WORK}/units`);
  await send(); await idle();
  await shot('03-configuracoes');

  // ---- login ----
  const anon = await browser.newContext({ viewport: { width: 1360, height: 820 } });
  const ap = await anon.newPage();
  await ap.goto(B + '/login'); await ap.waitForLoadState('networkidle');
  await shot('04-login', ap);
  await anon.close();

  // ---- databases: create menu and engine picker ----
  await page.goto(B + '/'); await idle();
  await page.click('#create-btn'); await sleep(200);
  await shot('05-menu-criar');
  await page.goto(B + '/databases/new?kind=mongodb'); await idle();
  await shot('06-novo-banco-mongodb-nao-instalado');

  // Submit every database form first, then poll the API: provisioning runs in parallel instead of one after another.
  const ready = new Set();
  const wanted = [['postgres', 'postgres'], ['redis', 'redis'], ['mysql', 'mysql']];
  const started = [];
  for (const [kind, name] of wanted) {
    await page.goto(B + `/databases/new?kind=${kind}`);
    if (!(await page.locator('[name=name]').count())) { console.log('pulado: banco', name, '(servidor não instalado)'); continue; }
    if (kind === 'postgres') await shot('07-novo-banco-postgres');
    await setv('name', name); await send(); await page.waitForLoadState('domcontentloaded');
    started.push(name);
  }
  const deadline = Date.now() + 75000;
  while (started.length && Date.now() < deadline) {
    const j = await (await ctx.request.get(B + '/api/databases')).json();
    const list = Array.isArray(j) ? j : (j.databases || []);
    for (const d of list) if (d.status === 'ready') ready.add(d.name);
    if (started.every((n) => ready.has(n))) break;
    await sleep(500);
  }
  console.log('bancos prontos:', [...ready].join(', ') || 'nenhum');
  execFileSync('python3', [SEED_DATA, `${WORK}/data/brpanel.db`]);

  // ---- apps that reference the databases ----
  const refs = (...l) => l.filter(([db]) => ready.has(db)).map(([db, v, k = v]) => `${v}=\${{${db}.${k}}}`).join('\n');
  const createApp = async (fields, env) => {
    await page.goto(B + '/apps/new'); await idle();
    for (const [k, v] of Object.entries(fields)) await setv(k, v);
    if (env) await setv('env', env);
    await send(); await idle();
  };
  await createApp({ name: 'loja', repo: 'https://github.com/heroku/node-js-getting-started.git', build_cmd: 'echo instalando && sleep 1', start_cmd: 'sleep 100000', domain: 'loja.exemplo.com' },
    refs(['postgres', 'DATABASE_URL'], ['redis', 'REDIS_URL']));
  await createApp({ name: 'api', repo: 'https://github.com/octocat/Hello-World.git', branch: 'master', build_cmd: 'echo ok', start_cmd: 'sleep 100000', domain: 'api.exemplo.com' },
    refs(['postgres', 'DATABASE_URL'], ['mysql', 'MYSQL_URL', 'DATABASE_URL']));
  await createApp({ name: 'worker', repo: 'https://github.com/octocat/Hello-World.git', branch: 'master', build_cmd: 'echo ok', start_cmd: 'sleep 100000' },
    refs(['redis', 'REDIS_URL']));
  for (let i = 0; i < 60; i++) { // up to ~30 s for the git clones
    const j = await (await ctx.request.get(B + '/api/apps')).json();
    const apps = Array.isArray(j) ? j : (j.apps || []);
    if (apps.length === 3 && apps.every((x) => !x.deploying)) break;
    await sleep(500);
  }
  execFileSync('python3', [SEED, `${WORK}/data/brpanel.db`]);

  // ---- architecture canvas ----
  await page.goto(B + '/'); await idle(); await sleep(600);
  await shot('09-arquitetura');
  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'light')); await sleep(200);
  await shot('10-arquitetura-tema-claro');
  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'dark'));

  // ---- database sheet (needs real database servers) ----
  try {
  if (!ready.has('postgres')) throw new Error('PostgreSQL não ficou pronto neste ambiente');
  await page.goto(B + '/databases/postgres'); await idle();
  await shot('11-banco-conectar');
  await page.click('[data-reveal]'); await sleep(100);
  await shot('12-banco-conectar-senha-visivel');
  await tab('dados');
  await page.click('#dblist button >> nth=0'); await sleep(400);
  await shot('13-banco-dados');
  await tab('backups');
  await page.click('#tab-backups form[action$="/backup"] button'); await idle(); await sleep(1500);
  await page.goto(B + '/databases/postgres#backups'); await idle();
  await shot('14-banco-backups');
  await tab('metricas'); await sleep(400);
  await shot('15-banco-metricas');
  await tab('logs'); await sleep(300);
  await page.click('.sw-tab[data-kind="setup"]'); await sleep(300);
  await shot('16-banco-logs-provisionamento');
  await tab('configuracoes');
  await shot('17-banco-configuracoes');
  if (ready.has('redis')) {
    await page.goto(B + '/databases/redis#dados'); await idle();
    await page.click('#dblist button >> nth=1'); await sleep(400);
    await shot('18-redis-dados');
  }
  if (ready.has('mysql')) {
    await page.goto(B + '/databases/mysql#dados'); await idle();
    await page.click('#dblist button >> nth=0'); await sleep(400);
    await shot('19-mysql-dados');
  }

  } catch (e) { console.log('pulado: telas do banco:', e.message.split('\n')[0]); }

  // ---- app sheet with variables ----
  const soft = async (label, fn) => { try { await fn(); } catch (e) { console.log('pulado:', label, '-', e.message.split('\n')[0]); } };
  await soft('telas do app', async () => {
  await page.goto(B + '/apps/loja'); await idle();
  await shot('20-app-deploy');
  await tab('variaveis');
  await page.click('.ref[data-insert*="mysql.DATABASE_URL"]').catch(() => {});
  await shot('21-app-variaveis');
  await tab('metricas'); await sleep(400);
  await shot('22-app-metricas');
  });
  await soft('nova aplicação', async () => {
  await page.goto(B + '/apps/new'); await idle();
  await page.fill('[name=repo]', 'https://github.com/heroku/node-js-getting-started.git');
  await page.click('#wz-next'); // step 2 holds the detection
  await page.click('#detect-btn');
  await page.waitForFunction(() => !document.querySelector('#detect-btn').disabled && document.querySelector('#detect-result').textContent.includes('Encontrei'), null, { timeout: 90000 });
  await shot('23-nova-app-detectada');
  });
  await page.goto(B + '/github'); await idle();
  await shot('24-github');
  await page.goto(B + '/templates'); await idle();
  await shot('28-templates');
  await page.goto(B + '/cli'); await idle();
  await shot('29-cli', page, false);

  // ---- mobile ----
  const m = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2 });
  await m.addCookies(await ctx.cookies());
  const mp = await m.newPage();
  for (const [name, path] of [['25-mobile-arquitetura', '/'], ['27-mobile-app', '/apps/loja'], ['30-mobile-templates', '/templates']]) {
    await mp.goto(B + path); await mp.waitForLoadState('networkidle'); await sleep(400);
    await shot(name, mp);
  }
  await browser.close();
  console.log('tempo total:', Math.round((Date.now() - T0) / 1000), 's');
})().catch((e) => { console.error(e); process.exit(1); });
