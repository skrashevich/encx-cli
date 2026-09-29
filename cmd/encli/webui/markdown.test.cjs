const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const { test } = require('node:test');

const source = fs.readFileSync(`${__dirname}/app.js`, 'utf8').replace(/\nboot\(\);\s*$/, '');
const context = vm.createContext({
  URL,
  URLSearchParams,
  document: {
    createElement() {
      let value = '';
      return {
        set textContent(text) { value = String(text); },
        get innerHTML() {
          return value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');
        },
      };
    },
  },
});
vm.runInContext(source, context);
vm.runInContext("state.detail = {domain: 'svk.en.cx'}", context);

test('renders an Encounter image inline through the authenticated media route', () => {
  const html = vm.runInContext("renderMarkdown('![Артефакт](https://d1.endata.cx/data/games/81369/artefact.jpg)')", context);
  assert.match(html, /<img class="md-image"/);
  assert.match(html, /alt="Артефакт"/);
  assert.match(html, /\/api\/v1\/media\?domain=svk\.en\.cx&amp;url=https%3A%2F%2Fd1\.endata\.cx/);
});

test('rejects active content in Markdown image URLs', () => {
  const html = vm.runInContext("renderMarkdown('![bad](javascript:alert(1)) ![bad](https://a.example/x\\\"onerror=\\\"alert(1))')", context);
  assert.doesNotMatch(html, /onerror="alert/);
  assert.doesNotMatch(html, /src="javascript:/);
});

test('links to a locally generated PDF artifact', () => {
  const html = vm.runInContext("renderMarkdown('[Сценарий PDF](/api/v1/chats/1840cf2226446dc0/artifacts/0123456789abcdef0123456789abcdef.pdf)')", context);
  assert.match(html, /href="\/api\/v1\/chats\/1840cf2226446dc0\/artifacts\/0123456789abcdef0123456789abcdef.pdf"/);
});
