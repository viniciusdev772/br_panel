// Drives the panel through every screen and saves PNGs. Configured by run.sh via environment:
//   PANEL_URL, WORK (temp dir with data/ stubs/ ...), OUT (output dir), SEED, SEED_DATA (python helpers)
const { chromium } = require('playwright');
const fs = require('fs');
const { execFileSync } = require('child_process');

const { PANEL_URL: B, WORK, OUT, SEED, SEED_DATA } = process.env;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const PASSWORD = 'senha-muito-longa-123';
const SUBMIT = 'main form button[type=submit]'; // the top bar also has a (logout) submit button

(async () => {
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1360, height: 820 } });
  const page = await ctx.newPage();
  const shot = async (name, p = page) => {
    await sleep(900);
    await p.screenshot({ path: `${OUT}/${name}.png`, fullPage: true });
    console.log('ok', name);
  };
  const idle = () => page.waitForLoadState('networkidle');
  const tab = async (name) => { await page.click(`.tab-btn[data-tab="${name}"]`); await sleep(700); };

  // ---- first run ----
  const token = fs.readFileSync(`${WORK}/data/setup_token`, 'utf8').trim();
  await page.goto(B + '/'); await page.waitForURL('**/setup'); await idle();
  await shot('01-setup');
  await page.fill('[name=token]', token); await page.fill('[name=username]', 'admin');
  await page.fill('[name=password]', PASSWORD); await page.fill('[name=password2]', PASSWORD);
  await page.fill('[name=webhook_secret]', '');
  await page.click(SUBMIT); await idle();
  await shot('02-arquitetura-vazia');

  // ---- settings (point the system dirs at the temp area) ----
  await page.goto(B + '/settings'); await idle();
  await page.fill('[name=nginx_dir]', `${WORK}/nginx`); await page.fill('[name=acme_root]', `${WORK}/acme`);
  await page.fill('[name=cert_dir]', `${WORK}/certs`); await page.fill('[name=unit_dir]', `${WORK}/units`);
  await page.click(SUBMIT); await idle();
  await shot('03-configuracoes');

  // ---- login ----
  const anon = await browser.newContext({ viewport: { width: 1360, height: 820 } });
  const ap = await anon.newPage();
  await ap.goto(B + '/login'); await ap.waitForLoadState('networkidle');
  await shot('04-login', ap);
  await anon.close();

  // ---- databases: create menu and engine picker ----
  await page.goto(B + '/'); await idle();
  await page.click('#create-btn'); await sleep(400);
  await shot('05-menu-criar');
  await page.goto(B + '/databases/new?kind=mongodb'); await idle();
  await shot('06-novo-banco-mongodb-nao-instalado');

  const createDB = async (kind, name) => {
    await page.goto(B + `/databases/new?kind=${kind}`); await idle();
    await page.fill('[name=name]', name);
    await page.click(SUBMIT); await idle();
    await page.waitForFunction(() => document.querySelector('[data-sheet="db"]') && document.querySelector('[data-sheet="db"]').dataset.status === 'ready', null, { timeout: 180000 }).catch(() => {});
    await page.goto(B + `/databases/${name}`); await idle();
  };
  await page.goto(B + '/databases/new?kind=postgres'); await idle();
  await shot('07-novo-banco-postgres');
  await page.fill('[name=name]', 'postgres');
  await page.click(SUBMIT); await idle();
  await shot('08-banco-provisionando');
  for (let i = 0; i < 90; i++) {
    await page.goto(B + '/databases/postgres'); await idle();
    if (await page.locator('[data-sheet="db"][data-status="ready"]').count()) break;
    await sleep(2000);
  }
  await createDB('redis', 'redis');
  await createDB('mysql', 'mysql');
  execFileSync('python3', [SEED_DATA, `${WORK}/data/brpanel.db`]);

  // ---- apps that reference the databases ----
  const createApp = async (fields, env) => {
    await page.goto(B + '/apps/new'); await idle();
    for (const [k, v] of Object.entries(fields)) await page.fill(`[name=${k}]`, v);
    if (env) await page.fill('[name=env]', env);
    await page.click(SUBMIT); await idle();
  };
  await createApp({ name: 'loja', repo: 'https://github.com/heroku/node-js-getting-started.git', build_cmd: 'echo instalando && sleep 1', start_cmd: 'sleep 100000', domain: 'loja.exemplo.com' },
    'DATABASE_URL=${{postgres.DATABASE_URL}}\nREDIS_URL=${{redis.REDIS_URL}}');
  await createApp({ name: 'api', repo: 'https://github.com/octocat/Hello-World.git', branch: 'master', build_cmd: 'echo ok', start_cmd: 'sleep 100000', domain: 'api.exemplo.com' },
    'DATABASE_URL=${{postgres.DATABASE_URL}}\nMYSQL_URL=${{mysql.DATABASE_URL}}');
  await createApp({ name: 'worker', repo: 'https://github.com/octocat/Hello-World.git', branch: 'master', build_cmd: 'echo ok', start_cmd: 'sleep 100000' },
    'REDIS_URL=${{redis.REDIS_URL}}');
  await sleep(9000);
  execFileSync('python3', [SEED, `${WORK}/data/brpanel.db`]);
  await sleep(1500);

  // ---- architecture canvas ----
  await page.goto(B + '/'); await idle(); await sleep(2500);
  await shot('09-arquitetura');
  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'light')); await sleep(600);
  await shot('10-arquitetura-tema-claro');
  await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'dark'));

  // ---- database sheet ----
  await page.goto(B + '/databases/postgres'); await idle();
  await shot('11-banco-conectar');
  await page.click('[data-reveal]'); await sleep(300);
  await shot('12-banco-conectar-senha-visivel');
  await tab('dados');
  await page.click('#dblist button >> nth=0'); await sleep(1200);
  await shot('13-banco-dados');
  await tab('backups');
  await page.click('#tab-backups form[action$="/backup"] button'); await idle(); await sleep(3500);
  await page.goto(B + '/databases/postgres#backups'); await idle();
  await shot('14-banco-backups');
  await tab('metricas'); await sleep(1500);
  await shot('15-banco-metricas');
  await tab('logs'); await sleep(1500);
  await page.click('.sw-tab[data-kind="setup"]'); await sleep(1200);
  await shot('16-banco-logs-provisionamento');
  await tab('configuracoes');
  await shot('17-banco-configuracoes');
  await page.goto(B + '/databases/redis#dados'); await idle(); await sleep(1500);
  await page.click('#dblist button >> nth=1'); await sleep(1200);
  await shot('18-redis-dados');
  await page.goto(B + '/databases/mysql#dados'); await idle(); await sleep(1500);
  await page.click('#dblist button >> nth=0'); await sleep(1200);
  await shot('19-mysql-dados');

  // ---- app sheet with variables ----
  await page.goto(B + '/apps/loja'); await idle();
  await shot('20-app-deploy');
  await tab('variaveis');
  await page.click('.ref[data-insert*="mysql.DATABASE_URL"]').catch(() => {});
  await shot('21-app-variaveis');
  await tab('metricas'); await sleep(1500);
  await shot('22-app-metricas');
  await page.goto(B + '/apps/new'); await idle();
  await page.fill('[name=repo]', 'https://github.com/heroku/node-js-getting-started.git');
  await page.click('#detect-btn');
  await page.waitForFunction(() => !document.querySelector('#detect-btn').disabled && document.querySelector('#detect-result').textContent.includes('Detectado'), null, { timeout: 90000 });
  await shot('23-nova-app-detectada');
  await page.goto(B + '/github'); await idle();
  await shot('24-github');

  // ---- mobile ----
  const m = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2 });
  await m.addCookies(await ctx.cookies());
  const mp = await m.newPage();
  for (const [name, path] of [['25-mobile-arquitetura', '/'], ['26-mobile-banco', '/databases/postgres'], ['27-mobile-app', '/apps/loja']]) {
    await mp.goto(B + path); await mp.waitForLoadState('networkidle'); await sleep(2000);
    await shot(name, mp);
  }
  await browser.close();
})().catch((e) => { console.error(e); process.exit(1); });
