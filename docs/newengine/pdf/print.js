// Печатает HTML в PDF через Chrome DevTools Protocol.
//
//   node print.js <входной.html> <выходной.pdf>
//
// Отличие от `chrome --headless --print-to-pdf`: тот снимает PDF по событию load,
// когда paged.js ещё раскладывает документ по страницам, и отдаёт огрызок из
// двух-трёх страниц. Здесь печать ждёт колбэк PagedConfig.after (window.__PAGED_DONE__).
//
// Внешних зависимостей нет: WebSocket встроен в node начиная с 22.
const { spawn } = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

const [htmlPath, outPath] = process.argv.slice(2);
if (!htmlPath || !outPath) {
  console.error('использование: node print.js <входной.html> <выходной.pdf>');
  process.exit(2);
}

const CHROME_CANDIDATES = [
  process.env.CHROME_BIN,
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/Applications/Chromium.app/Contents/MacOS/Chromium',
  '/usr/bin/google-chrome',
  '/usr/bin/chromium',
  '/usr/bin/chromium-browser',
].filter(Boolean);

const chromeBin = CHROME_CANDIDATES.find((p) => fs.existsSync(p));
if (!chromeBin) {
  console.error('ошибка: не найден Chrome. Укажите путь через CHROME_BIN=…');
  process.exit(1);
}

const PORT = Number(process.env.CDP_PORT || 0) || 9000 + (process.pid % 900);
const profileDir = fs.mkdtempSync(path.join(os.tmpdir(), 'enpdf-chrome-'));

const chrome = spawn(chromeBin, [
  '--headless',
  '--disable-gpu',
  `--remote-debugging-port=${PORT}`,
  `--user-data-dir=${profileDir}`,
  '--no-first-run',
  '--no-default-browser-check',
  'about:blank',
], { stdio: 'ignore' });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function waitForPort() {
  for (let i = 0; i < 100; i++) {
    try {
      const r = await fetch(`http://127.0.0.1:${PORT}/json/version`);
      if (r.ok) return (await r.json()).webSocketDebuggerUrl;
    } catch {}
    await sleep(100);
  }
  throw new Error(`Chrome не поднял отладочный порт ${PORT}`);
}

class CDP {
  constructor(ws) {
    this.ws = ws;
    this.id = 0;
    this.pending = new Map();
    ws.addEventListener('message', (e) => {
      const msg = JSON.parse(e.data);
      if (msg.id && this.pending.has(msg.id)) {
        const { resolve, reject } = this.pending.get(msg.id);
        this.pending.delete(msg.id);
        msg.error ? reject(new Error(JSON.stringify(msg.error))) : resolve(msg.result);
      }
    });
  }
  send(method, params = {}, sessionId) {
    const id = ++this.id;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.ws.send(JSON.stringify({ id, method, params, sessionId }));
    });
  }
}

async function connect(url) {
  const ws = new WebSocket(url);
  await new Promise((resolve, reject) => {
    ws.addEventListener('open', resolve, { once: true });
    ws.addEventListener('error', reject, { once: true });
  });
  return new CDP(ws);
}

function cleanup() {
  chrome.kill();
  fs.rmSync(profileDir, { recursive: true, force: true });
}

(async () => {
  const browser = await connect(await waitForPort());

  const { targetId } = await browser.send('Target.createTarget', { url: 'about:blank' });
  const { sessionId } = await browser.send('Target.attachToTarget', { targetId, flatten: true });
  const call = (method, params = {}) => browser.send(method, params, sessionId);

  await call('Page.enable');
  await call('Runtime.enable');
  await call('Page.navigate', { url: `file://${path.resolve(htmlPath)}` });

  // ждём, пока paged.js разложит документ по страницам
  const deadline = Date.now() + 180_000;
  let pages = 0;
  let done = false;
  while (Date.now() < deadline && !done) {
    await sleep(400);
    const { result } = await call('Runtime.evaluate', {
      expression:
        'JSON.stringify({done: !!window.__PAGED_DONE__, n: document.querySelectorAll(".pagedjs_page").length})',
      returnByValue: true,
    });
    ({ done, n: pages } = JSON.parse(result.value));
  }
  if (!done) throw new Error(`paged.js не закончил раскладку за 180 с (готово страниц: ${pages})`);
  await sleep(600); // дать дорисоваться шрифтам и колонтитулам

  const { data } = await call('Page.printToPDF', {
    printBackground: true,
    preferCSSPageSize: true,
    displayHeaderFooter: false,
    marginTop: 0, marginBottom: 0, marginLeft: 0, marginRight: 0,
  });

  fs.writeFileSync(outPath, Buffer.from(data, 'base64'));
  console.log(`страниц: ${pages}`);

  await browser.send('Browser.close').catch(() => {});
  cleanup();
  process.exit(0);
})().catch((e) => {
  console.error('ошибка:', e.message);
  cleanup();
  process.exit(1);
});
