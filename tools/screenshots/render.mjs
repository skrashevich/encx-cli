import { chromium } from 'playwright';
import { createServer } from 'node:http';
import { readFile, mkdir, rename, rm } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = fileURLToPath(new URL('../../', import.meta.url));
const output = path.join(root, 'docs/screenshots');
const staging = path.join(output, '.rendering');
const chats = [
  { id: 'scenario', title: 'Проверка сценария', domain: 'tech.en.cx', game_id: 32055 },
  { id: 'route', title: 'Маршрут ночной игры', domain: 'tech.en.cx', game_id: 32055 },
  { id: 'codes', title: 'Коды и подсказки', domain: 'tech.en.cx', game_id: 32055 },
].map(chat => ({ ...chat, security_mode: 'approve', running: false, updated_at: '2026-09-22T09:00:00Z' }));
const messages = [
  { role: 'user', content: 'Проверь сценарий игры: все ли уровни готовы, есть ли коды и подсказки?' },
  { role: 'tool', content: 'admin-game → сценарий получен: 8 уровней\nadmin-level → проверены задания, коды и подсказки' },
  { role: 'assistant', content: '## Сценарий почти готов\nПроверил все **8 уровней**. Задания и основные коды заполнены. Перед стартом стоит исправить два пункта:\n\n| Уровень | Что проверить |\n| --- | --- |\n| 3 · Старое депо | Нет второй подсказки |\n| 6 · На другом берегу | Время первой подсказки — 0 минут |\n\nОстальные уровни готовы. Изменений в игру не вносил.' },
];
const files = { '/': ['index.html', 'text/html'], '/index.html': ['index.html', 'text/html'], '/style.css': ['style.css', 'text/css'], '/app.js': ['app.js', 'text/javascript'] };
const server = createServer(async (req, res) => {
  const pathname = new URL(req.url, 'http://localhost').pathname;
  if (pathname === '/api/v1/chats/scenario/events') {
    res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' });
    res.write(': demo stream stays open until the page closes\n\n');
    return;
  }
  const file = files[pathname];
  if (!file) { res.writeHead(404).end(); return; }
  try {
    const body = await readFile(path.join(root, 'cmd/encli/webui', file[0]));
    res.writeHead(200, { 'Content-Type': file[1] });
    res.end(body);
  } catch { res.writeHead(500).end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
let browser;
try {
  browser = await chromium.launch();
  await mkdir(staging, { recursive: true });
  for (const [name, theme, populated] of [
    ['web-ui-overview', 'light', false],
    ['web-ui-chat', 'light', true],
    ['web-ui-dark', 'dark', true],
  ]) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, deviceScaleFactor: 1, locale: 'ru-RU', timezoneId: 'Europe/Moscow', reducedMotion: 'reduce', colorScheme: theme });
    await context.addInitScript(theme => localStorage.setItem('encli-theme', theme), theme);
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    // Real app and renderers; only the backend is replaced with demo data.
    await page.route('**/api/v1/**', async route => {
      const endpoint = new URL(route.request().url()).pathname.replace('/api/v1', '');
      if (endpoint.endsWith('/events')) {
        await route.continue();
        return;
      }
      if (endpoint.endsWith('/approval')) {
        await route.fulfill({ status: 404, json: { error: 'No pending approval' } });
        return;
      }
      const responses = {
        '/agent/config': { model: 'gpt-5.6-sol' },
        '/auth/status': { domains: [{ domain: 'tech.en.cx', login: 'organizer', logged_in: true }] },
        '/catalog/domains': { domains: [{ domain: 'tech.en.cx' }] },
        '/catalog/games': { games: [{ id: 32055, title: 'Город после полуночи', role: 'admin' }] },
        '/chats': { chats },
        '/chats/scenario': { ...chats[0], messages: populated ? messages : [] },
        '/onboarding': { required: false },
      };
      if (!(endpoint in responses) || route.request().method() !== 'GET') {
        errors.push(`Unexpected API request: ${route.request().method()} ${endpoint}`);
        await route.fulfill({ status: 500, json: { error: 'Unknown screenshot fixture' } });
        return;
      }
      await route.fulfill({ json: responses[endpoint] });
    });
    await page.goto(`http://127.0.0.1:${server.address().port}/`, { waitUntil: 'load' });
    await page.locator('.chat-item.active').waitFor();
    await page.waitForFunction(() => !document.querySelector('#field-security-mode').disabled);
    await page.evaluate(async () => {
      const fonts = await Promise.all([
        document.fonts.load('400 15px Geologica', 'Проверка сценария'),
        document.fonts.load('400 12px \"PT Mono\"', 'Проверка сценария'),
      ]);
      await document.fonts.ready;
      if (fonts.some(loaded => !loaded.length)) throw new Error('UI fonts failed to load');
    });
    if (populated) await page.getByText('Сценарий почти готов', { exact: true }).waitFor();
    if (await page.locator('.toast.visible.err').count()) errors.push(await page.locator('.toast.visible.err').innerText());
    if (errors.length) throw new Error(errors.join('\n'));
    await page.screenshot({ path: path.join(staging, `${name}.png`), fullPage: true, animations: 'disabled' });
    await context.close();
    console.log(`Rendered ${name}.png (${theme})`);
  }
  // Publish only once all three scenes rendered successfully.
  for (const name of ['web-ui-overview', 'web-ui-chat', 'web-ui-dark']) {
    await rename(path.join(staging, `${name}.png`), path.join(output, `${name}.png`));
  }
} finally {
  await browser?.close();
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
  await rm(staging, { recursive: true, force: true });
}
