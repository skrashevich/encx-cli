const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function setup() {
  const elements = new Map();
  const context = vm.createContext({
    document: { getElementById(id) {
      if (!elements.has(id)) elements.set(id, { value: '', disabled: false });
      return elements.get(id);
    }},
  });
  const source = fs.readFileSync(path.join(__dirname, '../webui/app.js'), 'utf8');
  vm.runInContext(source.replace(/boot\(\);\s*$/, ''), context);
  vm.runInContext(`
    getSelectedDomain = () => '';
    getSelectedGameId = () => 0;
    syncSecurityModeVisual = clearAgentStatus = clearToolChips =
      renderAttachments = renderMessages = startRunningPoll =
      loadChats = setAgentStatus = () => {};
    selectChat = async id => { state.activeId = id; };
    const requests = [];
    const notices = [];
    toast = message => notices.push(message);
    api = async (url, options) => {
      requests.push({url, options});
      return url === '/chats' ? {id: 'first'} : {};
    };
  `, context);
  return { elements, run: code => vm.runInContext(code, context) };
}

test('composer is available without a chat, domain login or game', () => {
  const { elements, run } = setup();
  run('refreshSendState()');
  assert.equal(elements.get('message-input').disabled, false);
  assert.equal(elements.get('btn-send').disabled, false);
  assert.match(elements.get('message-input').placeholder, /Напишите/);
  run('state.agentRunning = true; refreshSendState()');
  assert.equal(elements.get('message-input').disabled, true);
});

test('first message creates a chat without a game and sends the original text', async () => {
  const { elements, run } = setup();
  run("$('message-input').value = 'Помоги разобраться'");
  await run('sendMessage()');
  const requests = JSON.parse(run('JSON.stringify(requests)'));
  assert.equal(requests[0]?.url, '/chats');
  assert.deepEqual(requests[0].options.body, { domain: '', game_id: 0 });
  assert.equal(requests[1]?.url, '/chats/first/messages');
  assert.equal(requests[1].options.body.content, 'Помоги разобраться');
  assert.equal(elements.get('message-input').value, '');
});

test('failed chat creation preserves the draft and allows retry', async () => {
  const { elements, run } = setup();
  run("api = async () => { throw new Error('offline'); }; $('message-input').value = 'Черновик'");
  await run('sendMessage()');
  run('refreshSendState()');
  assert.equal(elements.get('message-input').value, 'Черновик');
  assert.equal(elements.get('message-input').disabled, false);
  assert.ok(run("notices.includes('offline')"));
});

test('first message keeps a selected domain without requiring a game', async () => {
  const { run } = setup();
  run("getSelectedDomain = () => 'demo.en.cx'; $('message-input').value = 'Привет'");
  await run('sendMessage()');
  assert.deepEqual(JSON.parse(run('JSON.stringify(requests[0].options.body)')), {
    domain: 'demo.en.cx', game_id: 0,
  });
});

test('sending in an existing chat does not create another one', async () => {
  const { run } = setup();
  run("state.activeId = 'existing'; $('message-input').value = 'Привет'");
  await run('sendMessage()');
  assert.equal(run('requests[0].url'), '/chats/existing/messages');
  assert.equal(run("requests.some(r => r.url === '/chats')"), false);
});
