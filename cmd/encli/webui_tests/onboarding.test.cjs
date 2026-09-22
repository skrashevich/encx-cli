const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function setup() {
  const context = vm.createContext({});
  const source = fs.readFileSync(path.join(__dirname, '../webui/app.js'), 'utf8');
  vm.runInContext(source.replace(/boot\(\);\s*$/, ''), context);
  vm.runInContext(`
    let closed = false, error = '', calls = 0, valid = true;
    const fields = {domain: 'demo.en.cx', login: 'player', password: 'secret'};
    const form = {reportValidity: () => valid, requestSubmit: () => { calls++; }};
    document = {getElementById: id => id === 'onboarding-auth-form' ? form : null};
    FormData = class { get(key) { return fields[key]; } };
    setOnboardingBusy = busy => { state.onboarding.busy = busy; };
    setOnboardingError = message => { error = message; };
    closeOnboarding = () => { closed = true; };
    bootstrapWorkspace = async () => {};
    const requests = [];
    api = async (url, options) => { requests.push({url, options}); return {completed: true}; };
    state.onboarding.step = 'auth';
  `, context);
  return code => vm.runInContext(code, context);
}

test('auth next submits the required form instead of completing directly', async () => {
  const run = setup();
  await run('onboardingNext()');
  assert.equal(run('calls'), 1);
  assert.equal(run('requests.length'), 0);
});

test('invalid form cannot finish onboarding', async () => {
  const run = setup();
  run('valid = false');
  await run('onOnboardingAuthSubmit({preventDefault() {}})');
  assert.equal(run('requests.length'), 0);
  assert.equal(run('closed'), false);
});

test('valid form passes credentials to server before closing', async () => {
  const run = setup();
  await run('onOnboardingAuthSubmit({preventDefault() {}})');
  assert.equal(run('requests[0].url'), '/onboarding/complete');
  assert.equal(run('requests[0].options.body.password'), 'secret');
  assert.equal(run('closed'), true);
});

test('failed validation keeps onboarding open and releases busy state', async () => {
  const run = setup();
  run("api = async () => { throw new Error('Неверный пароль'); }");
  await run('onOnboardingAuthSubmit({preventDefault() {}})');
  assert.equal(run('closed'), false);
  assert.equal(run('error'), 'Неверный пароль');
  assert.equal(run('state.onboarding.busy'), false);
});

test('pending validation prevents duplicate submission', async () => {
  const run = setup();
  run('state.onboarding.busy = true');
  await run('onOnboardingAuthSubmit({preventDefault() {}})');
  assert.equal(run('requests.length'), 0);
});
