const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(`${__dirname}/../app.js`, 'utf8');

function harness(api) {
  const select = {
    value: 'full', dataset: {}, options: [{ value: 'full' }, { value: 'approve' }],
    classList: { remove() {}, add() {} },
  };
  const errors = [];
  const state = { activeId: 'chat', detail: { security_mode: 'approve' } };
  const context = vm.createContext({
    state, api, $: () => select, toast: message => errors.push(message),
    window: { setTimeout() {} },
  });
  vm.runInContext(source.slice(source.indexOf('function getSelectedSecurityMode()'),
    source.indexOf('function isLoggedInOnDomain(')), context);
  return { state, select, errors, apply: () => vm.runInContext('applySecurityMode()', context) };
}

test('rejected mode change restores the confirmed mode', async () => {
  const h = harness(async () => { throw new Error('agent run in progress'); });
  await h.apply();
  assert.equal(h.state.detail.security_mode, 'approve');
  assert.equal(h.select.value, 'approve');
  assert.equal(h.select.dataset.mode, 'approve');
  assert.deepEqual(h.errors, ['agent run in progress']);
});

test('mode changes only after server acceptance', async () => {
  let accept;
  const h = harness(() => new Promise(resolve => { accept = resolve; }));
  const pending = h.apply();
  assert.equal(h.state.detail.security_mode, 'approve');
  accept({ security_mode: 'full' });
  await pending;
  assert.equal(h.state.detail.security_mode, 'full');
  assert.equal(h.select.value, 'full');
});
