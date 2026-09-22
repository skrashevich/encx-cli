const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(`${__dirname}/../app.js`, 'utf8');
function harness() {
  const nodes = new Map();
  const calls = [];
  const context = vm.createContext({ URL, Date, setTimeout: () => 1, clearTimeout() {},
    state: { llm: null },
    $: id => { if (!nodes.has(id)) nodes.set(id, { value: '', hidden: true }); return nodes.get(id); },
    escapeHtml: String, setOnboardingResult: (...args) => calls.push(['result', ...args]),
    applyLLMSnapshot() {}, fillOnboardingLLM() {}, loadAgentConfig() {},
    window: { open: () => ({ close() {}, location: {} }) },
    api: async (path, opts) => { calls.push([path, opts]); return {}; },
  });
  vm.runInContext(source.slice(source.indexOf('const POLZA_BASE_URL'), source.indexOf('function isModalOpen()')), context);
  return { context, nodes, calls, run: code => vm.runInContext(code, context) };
}
test('defaults to Polza only for unconfigured user and recognizes saved preset', () => {
  const h = harness();
  assert.equal(h.run('polzaInitialMethod({})'), 'polza');
  assert.equal(h.run('polzaInitialMethod({agent:{auth_method:"gigachat"}})'), 'gigachat');
  assert.equal(h.run('polzaInitialMethod({stored:{auth_method:"codex"}})'), 'codex');
  assert.equal(h.run('polzaInitialMethod({stored:{auth_method:"apikey",base_url:"https://custom.example/v1",has_api_key:true}})'), 'apikey');
  assert.equal(h.run('polzaInitialMethod({stored:{auth_method:"apikey",base_url:"https://polza.ai/api/v1"}})'), 'polza');
});
test('manual connection sends only the dedicated Polza key and selected model', async () => {
  const h = harness();
  h.run('$("llm-polza-model").value = "deepseek/deepseek-v4-flash-0731"; $("llm-api-key").value = "OTHER_SECRET"; $("llm-polza-key").value = "POLZA_SECRET"');
  await h.run('connectPolza("llm")');
  const call = h.calls.find(c => c[0] === '/llm/polza/connect');
  assert.equal(call[1].body.api_key, 'POLZA_SECRET');
  assert.equal(call[1].body.model, 'deepseek/deepseek-v4-flash-0731');
  assert.ok(!JSON.stringify(h.calls).includes('OTHER_SECRET'));
});
test('canceled in-flight OAuth creation cancels server flow and cannot refresh settings', async () => {
  const h = harness();
  let resolve;
  h.context.api = (path, opts) => {
    h.calls.push([path, opts]);
    return path === '/llm/polza/login' ? new Promise(r => { resolve = r; }) : Promise.resolve({});
  };
  h.run('$("llm-polza-model").value = "deepseek/deepseek-v4-flash-0731"');
  const started = h.run('startPolzaLogin("llm")');
  await h.run('cancelPolzaLogin()');
  resolve({ id: 'late', authorize_url: 'https://polza.ai/oauth/authorize' });
  await started;
  assert.ok(h.calls.some(c => c[0] === '/llm/polza/login/late' && c[1].method === 'DELETE'));
  assert.ok(!h.calls.some(c => c[0] === '/llm/settings'));
});
test('available zero explains key limit as well as balance', () => {
  const h = harness();
  h.run('showPolzaBalance("llm", {amount:"100.00",available:"0.00"})');
  assert.match(h.calls[0][2], /100.00 ₽.*0.00 ₽.*лимит ключа/);
});

test('balance includes backend override warning', () => {
  const h = harness();
  h.run('showPolzaBalance("llm", {amount:"10",available:"5",warning:"Ключ задан окружением"})');
  assert.match(h.calls[0][2], /Ключ задан окружением/);
});
test('OAuth success with failed settings refresh reports error', async () => {
  const h = harness();
  h.context.api = async path => {
    if (path === '/llm/settings') throw new Error('refresh failed');
    return {status:'success'};
  };
  await h.run('polzaFlow = {id:"x",pre:"llm",deadline:Date.now()+10000}; pollPolzaLogin(polzaFlow)');
  assert.ok(h.calls.some(c => c[0] === 'result' && c[2] === 'refresh failed'));
});
