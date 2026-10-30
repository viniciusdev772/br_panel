// Drives the panel through every screen and saves PNGs. Configured by run.sh via environment:
//   PANEL_URL, WORK (temp dir with data/ stubs/ ...), OUT (output dir), SEED (path to seed.py)
const { chromium } = require('playwright');
const fs = require('fs');
const { execFileSync } = require('child_process');

const { PANEL_URL: B, WORK, OUT, SEED } = process.env;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const PASSWORD = 'senha-muito-longa-123';
const SUBMIT = 'main form button[type=submit]'; // the top bar also has a (logout) submit button

(async () => {
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await ctx.newPage();
  const shot = async (name, p = page) => {
    await sleep(900);
    await p.screenshot({ path: `${OUT}/${name}.png`, fullPage: true });
    console.log('ok', name);
  };
  const idle = () => page.waitForLoadState('networkidle');

  // ---- first run ----
  const token = fs.readFileSync(`${WORK}/data/setup_token`, 'utf8').trim();
  await page.goto(B + '/'); await page.waitForURL('**/setup'); await idle();
  await shot('01-setup');
  await page.fill('[name=token]', 'token-errado'); await page.fill('[name=username]', 'admin');
  await page.fill('[name=password]', PASSWORD); await page.fill('[name=password2]', PASSWORD);
  await page.click(SUBMIT); await idle();
  await shot('02-setup-erro-token');
  await page.fill('[name=token]', token);
  await page.fill('[name=password]', PASSWORD); await page.fill('[name=password2]', PASSWORD);
  await page.fill('[name=panel_domain]', 'painel.exemplo.com'); await page.fill('[name=acme_email]', 'admin@exemplo.com');
  await shot('03-setup-preenchido');
  await page.fill('[name=panel_domain]', ''); await page.fill('[name=webhook_secret]', '');
  await page.click(SUBMIT); await idle();
  await shot('04-dashboard-vazio');

  // ---- settings (point the system dirs at the temp area) ----
  await page.goto(B + '/settings'); await idle();
  await page.fill('[name=nginx_dir]', `${WORK}/nginx`); await page.fill('[name=acme_root]', `${WORK}/acme`);
  await page.fill('[name=cert_dir]', `${WORK}/certs`); await page.fill('[name=unit_dir]', `${WORK}/units`);
  await page.fill('[name=github_app_id]', '1234567');
  await page.click(SUBMIT); await idle();
  await shot('05-configuracoes-erro-github');
  await page.fill('[name=github_app_id]', '');
  await page.click(SUBMIT); await idle();
  await shot('06-configuracoes');

  // ---- login ----
  const anon = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const ap = await anon.newPage();
  await ap.goto(B + '/login'); await ap.waitForLoadState('networkidle');
  await shot('07-login', ap);
  await ap.fill('[name=username]', 'admin'); await ap.fill('[name=password]', 'errada');
  await ap.click(SUBMIT); await ap.waitForLoadState('networkidle');
  await shot('08-login-erro', ap);
  await anon.close();

  // ---- new app with auto-detection ----
  await page.goto(B + '/apps/new'); await idle();
  await shot('09-nova-app');
  await page.fill('[name=repo]', 'https://github.com/heroku/node-js-getting-started.git');
  await page.click('#detect-btn');
  await page.waitForFunction(() => !document.querySelector('#detect-btn').disabled &&
    document.querySelector('#detect-result').textContent.includes('Detectado'), null, { timeout: 90000 });
  await shot('10-nova-app-detectada');

  const create = async (fields) => {
    await page.goto(B + '/apps/new'); await idle();
    for (const [k, v] of Object.entries(fields)) await page.fill(`[name=${k}]`, v);
    await page.click(SUBMIT); await idle();
  };
  // build commands are harmless stand-ins so the demo deploys finish without installing anything
  await create({ repo: 'https://github.com/heroku/node-js-getting-started.git', name: 'loja', build_cmd: 'echo "instalando dependências…" && sleep 1 && echo ok', domain: 'loja.exemplo.com' });
  await create({ name: 'api', repo: 'https://github.com/octocat/Hello-World.git', branch: 'master', build_cmd: 'echo build ok', start_cmd: 'node server.js', domain: 'api.exemplo.com' });
  await create({ name: 'worker', repo: 'https://github.com/octocat/Hello-World.git', branch: 'master', build_cmd: 'echo baixando…; sleep 60', start_cmd: './worker.sh' });
  await create({ name: 'site', repo: 'https://github.com/octocat/repositorio-que-nao-existe-xyz.git', start_cmd: 'x' });
  await sleep(2500);
  await page.goto(B + '/apps/worker'); await idle();
  await shot('11-app-em-deploy');
  await sleep(8000);
  execFileSync('python3', [SEED, `${WORK}/data/brpanel.db`]);
  await sleep(1500);

  // ---- with data ----
  await page.goto(B + '/'); await idle(); await sleep(1500);
  await shot('12-dashboard');
  await page.goto(B + '/apps/loja'); await idle(); await sleep(1500);
  await shot('13-app-1h');
  await page.click('.range-btn[data-range="24h"]'); await sleep(1500);
  await shot('14-app-24h');
  await page.goto(B + '/apps/site'); await idle();
  await shot('15-app-falhou');
  await page.goto(B + '/apps/loja/edit'); await idle();
  await shot('16-editar-app');
  await page.goto(B + '/apps/loja/logs'); await idle(); await sleep(1500);
  await shot('17-logs-servico');
  await page.click('.tab[data-kind="deploy"]'); await sleep(1500);
  await shot('18-logs-deploy');
  await page.fill('#log-filter', 'detect'); await sleep(600);
  await shot('19-logs-filtro');
  await page.goto(B + '/github'); await idle();
  await shot('20-github');

  // ---- mobile ----
  const m = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2 });
  await m.addCookies(await ctx.cookies());
  const mp = await m.newPage();
  for (const [name, path] of [['21-mobile-dashboard', '/'], ['22-mobile-app', '/apps/loja'], ['23-mobile-logs', '/apps/loja/logs']]) {
    await mp.goto(B + path); await mp.waitForLoadState('networkidle'); await sleep(1500);
    await shot(name, mp);
  }
  await browser.close();
})().catch((e) => { console.error(e); process.exit(1); });
