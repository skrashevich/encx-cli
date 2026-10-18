const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(`${__dirname}/../app.js`, 'utf8');
function harness() {
  const nodes = new Map();
  const calls = [];
  let configured = false;
  const context = vm.createContext({
    state: { llm: null }, escapeHtml: String,
    $: id => { if (!nodes.has(id)) nodes.set(id, { value: '', disabled: false }); return nodes.get(id); },
    applyLLMSnapshot() {}, fillOnboardingLLM() {}, loadAgentConfig() {}, toast: (...a) => calls.push(['toast', ...a]),
    api: async (path, opts) => {
      calls.push([path, opts]);
      if (path === '/llm/claude/login') configured = true;
      if (path === '/llm/claude/models') return { models: [{id:'claude-opus-4-6',name:'Claude Opus 4.6'},{id:'claude-sonnet-5-5',name:'Claude Sonnet 5.5'}], default_model:'claude-sonnet-5-5' };
      return { claude: { configured }, stored: { base_url: 'https://existing.example/v1' }, agent: { auth_method: 'claude' } };
    },
  });
  vm.runInContext(source.slice(source.indexOf('let claudeCatalog ='), source.indexOf('function bindPolza(')), context);
  return { context, nodes, calls, run: code => vm.runInContext(code, context) };
}
for (const pre of ['llm', 'onboarding-llm']) {
  test(`${pre}: token is saved separately; key and endpoint survive selecting Claude`, async () => {
    const h = harness();
    h.run(`$('${pre}-claude-token').value = 'SUBSCRIPTION_SECRET'; $('${pre}-claude-model').value = 'claude-opus-4-6'`);
    assert.equal(await h.run(`connectClaude('${pre}')`), true);
    assert.equal(h.nodes.get(`${pre}-claude-token`).value, '');
    assert.equal(h.nodes.get(`${pre}-claude-connect`).disabled, false);
    assert.equal(h.calls.find(c => c[0] === '/llm/claude/login')[1].body.token, 'SUBSCRIPTION_SECRET');
    const settings = h.calls.find(c => c[0] === '/llm/settings' && c[1]);
    assert.equal(settings[1].body.auth_method, 'claude');
    assert.equal(settings[1].body.model, 'claude-opus-4-6');
    assert.equal(settings[1].body.base_url, 'https://existing.example/v1');
    assert.equal(settings[1].body.api_key, undefined);
    assert.ok(!JSON.stringify(settings).includes('SUBSCRIPTION_SECRET'));
  });
}
test('invalid token does not select Claude or erase typed input', async () => {
  const h = harness();
  h.run(`$('llm-claude-token').value = 'bad'`);
  h.context.api = async () => { throw new Error('invalid token'); };
  assert.equal(await h.run(`connectClaude('llm')`), false);
  assert.equal(h.nodes.get('llm-claude-token').value, 'bad');
  assert.equal(h.nodes.get('llm-claude-connect').disabled, false);
  assert.ok(!h.calls.some(c => c[0] === '/llm/settings'));
});
test('missing credentials cannot silently replace the active transport', async () => {
  const h = harness();
  assert.equal(await h.run(`connectClaude('llm')`), false);
  assert.ok(!h.calls.some(c => c[0] === '/llm/settings' && c[1]));
});

test('catalog loads for both dropdowns and preserves a chosen model on refresh', async () => {
  const h = harness();
  h.run(`state.llm={claude:{configured:true}}; $('llm-claude-model').value='claude-opus-4-6'`);
  await h.run('loadClaudeModels()');
  assert.equal(h.nodes.get('llm-claude-model').value, 'claude-opus-4-6');
  assert.equal(h.nodes.get('onboarding-llm-claude-model').value, 'claude-sonnet-5-5');
  assert.match(h.nodes.get('llm-claude-model').innerHTML, /Claude Sonnet 5.5/);
  assert.equal(h.nodes.get('llm-claude-model').disabled, false);
  await h.run('loadClaudeModels()');
  assert.equal(h.calls.filter(c => c[0] === '/llm/claude/models').length, 1);
  await h.run('loadClaudeModels(true)');
  assert.equal(h.nodes.get('llm-claude-model').value, 'claude-opus-4-6');
});
test('catalog failure keeps the saved model, reports the error, and can be retried', async () => {
  const h = harness();
  h.run(`state.llm={claude:{configured:true}}; $('llm-claude-model').value='claude-old'`);
  h.context.api = async () => { throw new Error('HTTP 429'); };
  await h.run('loadClaudeModels()');
  assert.equal(h.nodes.get('llm-claude-model').value, 'claude-old');
  assert.match(h.nodes.get('llm-claude-models-status').textContent, /HTTP 429/);
  assert.equal(h.nodes.get('llm-claude-models-retry').disabled, false);
  h.context.api = async () => ({models:[{id:'claude-new',name:'New'}],default_model:'claude-new'});
  await h.run('loadClaudeModels(true)');
  assert.equal(h.nodes.get('llm-claude-model').value, 'claude-old');
  assert.match(h.nodes.get('llm-claude-model').innerHTML, /отсутствует в каталоге/);
});
test('a catalog response from before logout cannot repopulate the dropdown', async () => {
  const h = harness();
  h.run('state.llm={claude:{configured:true}}');
  let resolve;
  h.context.api = () => new Promise(r => { resolve=r; });
  const pending = h.run('loadClaudeModels()');
  h.run('resetClaudeModels(); state.llm={claude:{configured:false}}');
  resolve({models:[{id:'claude-stale',name:'Stale'}],default_model:'claude-stale'});
  await pending;
  assert.equal(h.run('claudeCatalog'), null);
  assert.equal(h.nodes.get('llm-claude-model').disabled, true);
});
