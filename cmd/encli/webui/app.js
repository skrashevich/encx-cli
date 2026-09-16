'use strict';

const API = '/api/v1';

const state = {
  chats: [],
  activeId: null,
  detail: null,
  authDomains: [],
  es: null,
  streamBuf: '',
  agentRunning: false,
  agentStatus: { phase: '', message: '' },
  lastActivityAt: 0,
  activityTimer: null,
  runningPoll: null,
  pendingTools: [],
  approvalPrompt: null,
  searchQuery: '',
  catalogGames: [],
  attachments: [],
  llm: null,
  llmAuth: '',
  llmEdited: { base_url: false, model: false },
  codexFlow: null,
  codexPoll: null,
  onboarding: { step: 'welcome', status: null, llmAuth: 'codex', engine: null, busy: false },
};

const ROLE_RU = {
  user: 'вы',
  assistant: 'ассистент',
  tool: 'инструмент',
  system: 'система',
};

const GAME_ROLE_RU = {
  player: 'участие',
  admin: 'админ',
  both: 'участие+админ',
};

const $ = (id) => document.getElementById(id);

function escapeHtml(s) {
  const d = document.createElement('div');
  d.textContent = s == null ? '' : String(s);
  return d.innerHTML;
}

function renderInlineMarkdown(s) {
  let x = escapeHtml(String(s ?? ''));
  x = x.replace(/`([^`]+)`/g, '<code class="md-code">$1</code>');
  x = x.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  x = x.replace(/\*([^*]+)\*/g, '<em>$1</em>');
  x = x.replace(/\[([^\]]+)\]\((https?:\/\/[^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>');
  return x;
}

function isTableRow(line) {
  return /^\s*\|.+\|\s*$/.test(line);
}

function isTableSeparator(line) {
  const t = line.trim();
  if (!t.includes('-')) return false;
  const inner = t.replace(/^\|/, '').replace(/\|$/, '');
  return inner.split('|').every((part) => /^[\s\-:]+$/.test(part.trim()));
}

function parseTableRow(line) {
  const t = line.trim().replace(/^\|/, '').replace(/\|$/, '');
  return t.split('|').map((c) => c.trim());
}

function renderMarkdownTable(lines) {
  if (!lines.length) return '';
  const header = parseTableRow(lines[0]);
  let bodyStart = 1;
  if (lines.length > 1 && isTableSeparator(lines[1])) bodyStart = 2;
  const body = [];
  for (let i = bodyStart; i < lines.length; i++) {
    if (!isTableRow(lines[i])) break;
    body.push(parseTableRow(lines[i]));
  }
  const cols = header.length;
  let html = '<div class="md-table-wrap"><table class="md-table"><thead><tr>';
  for (const c of header) html += `<th>${renderInlineMarkdown(c)}</th>`;
  html += '</tr></thead><tbody>';
  for (const row of body) {
    html += '<tr>';
    for (let c = 0; c < cols; c++) {
      html += `<td>${renderInlineMarkdown(row[c] ?? '')}</td>`;
    }
    html += '</tr>';
  }
  html += '</tbody></table></div>';
  return html;
}

function renderMarkdownList(lines, ordered) {
  const tag = ordered ? 'ol' : 'ul';
  let html = `<${tag} class="md-list">`;
  for (const line of lines) {
    const m = ordered ? line.match(/^\s*\d+\.\s+(.*)$/) : line.match(/^\s*[-*+]\s+(.*)$/);
    if (m) html += `<li>${renderInlineMarkdown(m[1])}</li>`;
  }
  html += `</${tag}>`;
  return html;
}

function renderMarkdown(text) {
  const raw = String(text ?? '').replace(/\r\n/g, '\n');
  const lines = raw.split('\n');
  const blocks = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];

    if (line.trim().startsWith('```')) {
      const fence = line.trim();
      const lang = fence.slice(3).trim();
      i++;
      const codeLines = [];
      while (i < lines.length && !lines[i].trim().startsWith('```')) {
        codeLines.push(lines[i]);
        i++;
      }
      if (i < lines.length) i++;
      const code = escapeHtml(codeLines.join('\n'));
      const langAttr = lang ? ` data-lang="${escapeHtml(lang)}"` : '';
      blocks.push(`<pre class="md-pre"${langAttr}><code>${code}</code></pre>`);
      continue;
    }

    if (isTableRow(line)) {
      const tableLines = [];
      while (i < lines.length && (isTableRow(lines[i]) || isTableSeparator(lines[i]))) {
        tableLines.push(lines[i]);
        i++;
      }
      blocks.push(renderMarkdownTable(tableLines));
      continue;
    }

    const heading = line.match(/^(#{1,6})\s+(.+)$/);
    if (heading) {
      const level = heading[1].length;
      blocks.push(`<h${level} class="md-h${level}">${renderInlineMarkdown(heading[2])}</h${level}>`);
      i++;
      continue;
    }

    if (/^\s*[-*+]\s+/.test(line)) {
      const listLines = [];
      while (i < lines.length && /^\s*[-*+]\s+/.test(lines[i])) {
        listLines.push(lines[i]);
        i++;
      }
      blocks.push(renderMarkdownList(listLines, false));
      continue;
    }

    if (/^\s*\d+\.\s+/.test(line)) {
      const listLines = [];
      while (i < lines.length && /^\s*\d+\.\s+/.test(lines[i])) {
        listLines.push(lines[i]);
        i++;
      }
      blocks.push(renderMarkdownList(listLines, true));
      continue;
    }

    if (line.trim() === '') {
      i++;
      continue;
    }

    const para = [];
    while (i < lines.length && lines[i].trim() !== '' && !lines[i].trim().startsWith('```') && !isTableRow(lines[i]) && !/^(#{1,6})\s+/.test(lines[i]) && !/^\s*[-*+]\s+/.test(lines[i]) && !/^\s*\d+\.\s+/.test(lines[i])) {
      para.push(lines[i]);
      i++;
    }
    blocks.push(`<p class="md-p">${renderInlineMarkdown(para.join('\n')).replace(/\n/g, '<br>')}</p>`);
  }

  return blocks.join('\n');
}

function toast(msg, err) {
  const el = $('toast');
  el.textContent = msg;
  el.classList.toggle('err', !!err);
  el.classList.add('visible');
  clearTimeout(toast._t);
  toast._t = setTimeout(() => el.classList.remove('visible'), 4200);
}

function normGameId(raw) {
  const t = String(raw ?? '').trim();
  if (!t) return 0;
  const n = Number(t);
  return Number.isFinite(n) ? n : 0;
}

async function api(path, opts = {}) {
  const init = {
    credentials: 'same-origin',
    headers: { ...(opts.headers || {}), Accept: 'application/json' },
    ...opts,
  };
  if (opts.body != null && typeof opts.body === 'object' && !(opts.body instanceof FormData)) {
    init.body = JSON.stringify(opts.body);
    init.headers['Content-Type'] = 'application/json';
  }
  const res = await fetch(`${API}${path}`, init);
  const text = await res.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = text;
    }
  }
  if (!res.ok) {
    const msg =
      typeof data === 'object' && data && (data.message || data.error || data.detail)
        ? String(data.message || data.error || data.detail)
        : `HTTP ${res.status}`;
    const err = new Error(msg);
    err.status = res.status;
    err.data = data;
    throw err;
  }
  return data;
}

function parseSSEPayload(ev) {
  if (ev.data == null || ev.data === '') return {};
  try {
    return JSON.parse(ev.data);
  } catch {
    return { _raw: ev.data };
  }
}

async function loadAuthStatus() {
  const data = await api('/auth/status');
  state.authDomains = Array.isArray(data?.domains) ? data.domains : [];
  renderAuth();
  await fillDomainSelect();
}

async function loadAgentConfig() {
  const el = $('brand-model');
  if (!el) return;
  try {
    const data = await api('/agent/config');
    const model = String(data?.model || '').trim();
    if (model) {
      el.textContent = model;
      const base = String(data?.base_url || '').trim();
      el.title = base ? `Модель: ${model}\nAPI: ${base}` : `Модель: ${model}`;
      el.classList.remove('is-missing');
    } else {
      const err = String(data?.error || '').trim();
      el.textContent = err ? 'модель не настроена' : '—';
      el.title = err || 'Откройте «Настройки LLM» или задайте LLM_MODEL';
      el.classList.add('is-missing');
    }
  } catch (e) {
    el.textContent = 'модель ?';
    el.title = e.message || String(e);
    el.classList.add('is-missing');
  }
}

async function fillDomainSelect() {
  const sel = $('field-domain');
  if (!sel) return;
  const prev = sel.value;
  let domains = [];
  try {
    const data = await api('/catalog/domains');
    domains = Array.isArray(data?.domains) ? data.domains.map((d) => d.domain).filter(Boolean) : [];
  } catch {
    domains = state.authDomains.filter((d) => d.logged_in).map((d) => d.domain);
  }
  sel.innerHTML = '';
  if (!domains.length) {
    const o = document.createElement('option');
    o.value = '';
    o.textContent = '— войдите на домен —';
    sel.appendChild(o);
    sel.disabled = true;
    await fillGameSelect([]);
    return;
  }
  sel.disabled = false;
  for (const domain of domains) {
    const o = document.createElement('option');
    o.value = domain;
    o.textContent = domain;
    sel.appendChild(o);
  }
  if (prev && domains.includes(prev)) {
    sel.value = prev;
  }
  await loadGamesForDomain(sel.value);
}

async function loadGamesForDomain(domain) {
  const sel = $('field-game-id');
  if (!sel) return;
  sel.innerHTML = '';
  if (!domain) {
    const o = document.createElement('option');
    o.value = '0';
    o.textContent = '— выберите домен —';
    sel.appendChild(o);
    sel.disabled = true;
    return;
  }
  sel.disabled = false;
  const loading = document.createElement('option');
  loading.value = '';
  loading.textContent = 'Загрузка игр…';
  sel.appendChild(loading);
  sel.disabled = true;
  try {
    const data = await api(`/catalog/games?domain=${encodeURIComponent(domain)}`);
    const games = Array.isArray(data?.games) ? data.games : [];
    state.catalogGames = games;
    await fillGameSelect(games);
  } catch (e) {
    state.catalogGames = [];
    sel.innerHTML = '';
    const o = document.createElement('option');
    o.value = '0';
    o.textContent = `Ошибка: ${e.message || 'нет игр'}`;
    sel.appendChild(o);
    sel.disabled = true;
  }
}

function fillGameSelect(games, selectedId) {
  const sel = $('field-game-id');
  if (!sel) return Promise.resolve();
  const want = selectedId != null ? Number(selectedId) : Number(sel.value);
  sel.innerHTML = '';
  if (!games.length) {
    const o = document.createElement('option');
    o.value = '0';
    o.textContent = '— нет незавершённых игр —';
    sel.appendChild(o);
    sel.disabled = true;
    return Promise.resolve();
  }
  sel.disabled = false;
  const empty = document.createElement('option');
  empty.value = '0';
  empty.textContent = '— не выбрана —';
  sel.appendChild(empty);
  for (const g of games) {
    const o = document.createElement('option');
    o.value = String(g.id);
    const role = GAME_ROLE_RU[g.role] || g.role || '';
    o.textContent = `#${g.id} · ${g.title}${role ? ` (${role})` : ''}`;
    sel.appendChild(o);
  }
  if (want && games.some((g) => g.id === want)) {
    sel.value = String(want);
  }
  return Promise.resolve();
}

function getSelectedDomain() {
  return ($('field-domain')?.value || '').trim();
}

function getSelectedGameId() {
  return normGameId($('field-game-id')?.value);
}

function getSelectedSecurityMode() {
  return ($('field-security-mode')?.value || 'approve').trim();
}

function syncSecurityModeFromDetail(detail) {
  const sel = $('field-security-mode');
  if (!sel) return;
  const mode = detail?.security_mode || 'approve';
  if ([...sel.options].some((o) => o.value === mode)) {
    sel.value = mode;
  }
  syncSecurityModeVisual();
}

function syncSecurityModeVisual() {
  const sel = $('field-security-mode');
  if (!sel) return;
  sel.dataset.mode = sel.value || 'approve';
}

function flashSecurityModeApplied() {
  const sel = $('field-security-mode');
  if (!sel) return;
  sel.classList.remove('is-applied');
  void sel.offsetWidth;
  sel.classList.add('is-applied');
  window.setTimeout(() => sel.classList.remove('is-applied'), 700);
}

async function applySecurityMode() {
  const mode = getSelectedSecurityMode();
  syncSecurityModeVisual();
  if (state.detail) {
    state.detail.security_mode = mode;
  }
  if (!state.activeId) return;
  try {
    const updated = await api(`/chats/${encodeURIComponent(state.activeId)}`, {
      method: 'PATCH',
      body: { security_mode: mode },
    });
    if (updated && typeof updated === 'object') {
      state.detail = { ...state.detail, ...updated };
      syncSecurityModeFromDetail(state.detail);
    }
    flashSecurityModeApplied();
  } catch (e) {
    toast(e.message || String(e), true);
    if (state.detail?.security_mode) {
      syncSecurityModeFromDetail(state.detail);
    }
  }
}

function isLoggedInOnDomain(domain) {
  if (!domain) return false;
  return state.authDomains.some((d) => d.domain === domain && d.logged_in);
}

/** Создаёт чат для текущего домена/игры, если в сайдбаре ничего не выбрано. */
async function ensureActiveChat() {
  if (state.activeId) return state.activeId;
  const domain = getSelectedDomain();
  if (!domain || !isLoggedInOnDomain(domain)) return null;
  const gameId = getSelectedGameId();
  if (!gameId) return null;
  try {
    const created = await api('/chats', {
      method: 'POST',
      body: { domain, game_id: gameId },
    });
    const id =
      created?.id != null
        ? String(created.id)
        : created?.chat?.id != null
          ? String(created.chat.id)
          : null;
    if (!id) return null;
    await loadChats();
    await selectChat(id);
    return id;
  } catch (e) {
    toast(e.message || String(e), true);
    return null;
  }
}

async function onChatContextChanged() {
  renderAuth();
  renderMessages();
  if (getSelectedDomain() && getSelectedGameId() && !state.activeId) {
    await ensureActiveChat();
  }
  refreshSendState();
}

function renderComposerPlaceholder() {
  const input = $('message-input');
  if (!input) return;
  const domain = getSelectedDomain();
  if (!isLoggedInOnDomain(domain)) {
    input.placeholder = 'Войдите на выбранный домен…';
    return;
  }
  if (!state.activeId) {
    input.placeholder = getSelectedGameId()
      ? 'Создаём чат… или нажмите «Новый чат»'
      : 'Выберите игру в списке выше…';
    return;
  }
  if (state.agentRunning) {
    input.placeholder = state.agentStatus.message || 'Агент отвечает…';
    return;
  }
  input.placeholder = 'Напишите задачу агенту… (Enter — отправить)';
}

async function loadChats() {
  const q = state.searchQuery.trim();
  const path = q ? `/chats?q=${encodeURIComponent(q)}` : '/chats';
  const data = await api(path);
  state.chats = Array.isArray(data?.chats) ? data.chats : [];
  state.chats.sort((a, b) => {
    const ta = Date.parse(a.updated_at || 0) || 0;
    const tb = Date.parse(b.updated_at || 0) || 0;
    return tb - ta;
  });
  renderChatList();
}

function findAuthDomain(domain) {
  return state.authDomains.find((d) => d.domain === domain);
}

function renderAuth() {
  const box = $('auth-status');
  const form = $('login-form');
  const logoutBtn = $('btn-logout');
  const domain = getSelectedDomain();
  const loggedInHere = isLoggedInOnDomain(domain);

  if (form) {
    form.classList.toggle('is-collapsed', loggedInHere);
    const domainInput = form.querySelector('input[name="domain"]');
    if (domainInput && domain && !loggedInHere && !domainInput.value.trim()) {
      domainInput.value = domain;
    }
  }
  if (logoutBtn) logoutBtn.hidden = !loggedInHere;

  if (!box) return;

  if (!state.authDomains.length) {
    box.innerHTML = '';
    return;
  }

  const sorted = [...state.authDomains].sort((a, b) => {
    const aSel = a.domain === domain ? 0 : 1;
    const bSel = b.domain === domain ? 0 : 1;
    if (aSel !== bSel) return aSel - bSel;
    return String(a.domain || '').localeCompare(String(b.domain || ''), 'ru');
  });

  box.innerHTML = sorted
    .map((d) => {
      const dom = escapeHtml(d.domain || '');
      const login = String(d.login || '').trim();
      const active = d.domain === domain ? ' is-active' : '';
      const offline = d.logged_in ? '' : ' is-offline';
      const userClass = login ? 'auth-session-user' : 'auth-session-user is-missing';
      const userText = login || (d.logged_in ? '…' : '—');
      const title = login
        ? `${d.domain} — ${login}`
        : d.logged_in
          ? `${d.domain} — вход без имени`
          : `${d.domain} — не авторизован`;
      return `<li class="auth-session-chip${active}${offline}" title="${escapeHtml(title)}">
        <span class="auth-session-dot" aria-hidden="true"></span>
        <span class="auth-session-domain">${dom}</span>
        <span class="${userClass}">${escapeHtml(userText)}</span>
      </li>`;
    })
    .join('');
}

function renderChatList() {
  const ul = $('chat-list');
  ul.innerHTML = '';
  for (const c of state.chats) {
    const id = String(c.id);
    const li = document.createElement('li');
    li.className = 'chat-list-item';

    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'chat-item' + (id === state.activeId ? ' active' : '');
    const title = c.title || `Чат ${id}`;
    const running = !!c.running;
    btn.innerHTML = `<span class="chat-item-title">${escapeHtml(title)}</span>
      <span class="chat-item-meta">
        ${running ? '<span class="dot-running" aria-hidden="true"></span>' : ''}
        <span>${escapeHtml(c.domain || '')}</span>
      </span>`;
    btn.addEventListener('click', () => switchChat(id));

    const del = document.createElement('button');
    del.type = 'button';
    del.className = 'chat-item-delete btn btn-ghost';
    del.title = 'Удалить чат';
    del.setAttribute('aria-label', `Удалить чат: ${title}`);
    del.innerHTML = '<span aria-hidden="true">✕</span>';
    del.addEventListener('click', (e) => {
      e.preventDefault();
      e.stopPropagation();
      void deleteChat(id);
    });

    li.appendChild(btn);
    li.appendChild(del);
    ul.appendChild(li);
  }
}

async function deleteChat(chatId) {
  const chat = state.chats.find((c) => String(c.id) === chatId);
  const title = chat?.title || `Чат ${chatId}`;
  const running = !!chat?.running;
  let msg = `Удалить «${title}»?`;
  if (running) {
    msg += '\nАгент сейчас работает — выполнение будет остановлено.';
  }
  if (!window.confirm(msg)) return;

  try {
    await api(`/chats/${encodeURIComponent(chatId)}`, { method: 'DELETE' });
    if (state.activeId === chatId) {
      hideApprovalBar();
      clearAgentStatus();
      state.agentRunning = false;
      await switchChat(null);
    }
    await loadChats();
    toast('Чат удалён.');
  } catch (e) {
    toast(e.message || String(e), true);
  }
}

function getMessagesFromDetail() {
  const d = state.detail;
  if (!d) return [];
  const arr = d.messages;
  return Array.isArray(arr) ? arr : [];
}

function renderMessages() {
  const wrap = $('messages');
  wrap.innerHTML = '';

  if (!state.activeId) {
    const domain = getSelectedDomain();
    const gameId = getSelectedGameId();
    let hint = '<p class="empty-hint-title">Чат не выбран</p>';
    if (!isLoggedInOnDomain(domain)) {
      hint +=
        '<p class="empty-hint">Войдите на домен в панели справа, затем выберите игру — чат создастся автоматически.</p>';
    } else if (!gameId) {
      hint +=
        '<p class="empty-hint">Выберите <strong>игру</strong> в списке над перепиской или нажмите <strong>Новый чат</strong> слева.</p>';
    } else {
      hint +=
        '<p class="empty-hint">Игра выбрана — чат появится через мгновение или нажмите <strong>Новый чат</strong>.</p>';
    }
    const empty = document.createElement('div');
    empty.className = 'empty-state';
    empty.innerHTML = hint;
    wrap.appendChild(empty);
    return;
  }

  const msgs = getMessagesFromDetail();
  for (const m of msgs) {
    const role = (m.role || 'assistant').toLowerCase();
    const roleLabel = ROLE_RU[role] || role;
    const content = m.content ?? m.text ?? '';
    const div = document.createElement('div');
    div.className = `msg ${role}`;
    const body = role === 'assistant' || role === 'user' ? renderMarkdown(content) : escapeHtml(content);
    div.innerHTML = `<span class="msg-role">${escapeHtml(roleLabel)}</span><span class="msg-body">${body}</span>`;
    wrap.appendChild(div);
  }
  if (state.agentRunning || state.streamBuf) {
    const div = document.createElement('div');
    div.className = 'msg assistant streaming';
    div.id = 'msg-streaming';
    div.innerHTML = `<span class="msg-role">${escapeHtml(ROLE_RU.assistant)}</span><span class="msg-body">${renderMarkdown(state.streamBuf)}</span>`;
    wrap.appendChild(div);
  }
  wrap.scrollTop = wrap.scrollHeight;
}

const PILL_LABEL_RU = {
  start: 'Запуск',
  llm: 'Модель',
  llm_wait: 'Ожидание',
  tool: 'Инструмент',
  stream: 'Ответ',
  retry: 'Повтор',
  log: 'Агент',
};

function shortPillLabel(phase) {
  return PILL_LABEL_RU[phase] || 'Агент';
}

function markAgentActivity() {
  state.lastActivityAt = Date.now();
  const pill = $('running-pill');
  if (pill && !pill.hidden) {
    pill.classList.add('is-active');
  }
  const bar = $('agent-status-bar');
  if (bar && !bar.hidden) {
    bar.classList.add('is-active');
  }
}

function scheduleActivityDecay() {
  clearTimeout(state.activityTimer);
  state.activityTimer = setTimeout(() => {
    if (!state.agentRunning) return;
    const idle = Date.now() - state.lastActivityAt > 3500;
    if (idle) {
      $('running-pill')?.classList.remove('is-active');
      $('agent-status-bar')?.classList.remove('is-active');
    } else {
      scheduleActivityDecay();
    }
  }, 3500);
}

function setAgentStatus(phase, message) {
  const text = String(message || '').trim();
  if (!text) return;
  state.agentStatus = { phase: phase || 'log', message: text };
  markAgentActivity();
  scheduleActivityDecay();

  const bar = $('agent-status-bar');
  const textEl = $('agent-status-text');
  const phaseEl = $('agent-status-phase');
  const pillLabel = $('running-pill-label');
  if (bar) {
    bar.hidden = false;
    bar.dataset.phase = phase || 'log';
  }
  if (textEl) textEl.textContent = text;
  if (phaseEl) phaseEl.textContent = shortPillLabel(phase);
  if (pillLabel) pillLabel.textContent = shortPillLabel(phase);

  refreshSendState();
}

function clearAgentStatus() {
  state.agentStatus = { phase: '', message: '' };
  clearTimeout(state.activityTimer);
  state.activityTimer = null;
  $('agent-status-bar')?.classList.remove('is-active');
  const bar = $('agent-status-bar');
  if (bar) bar.hidden = true;
  $('running-pill')?.classList.remove('is-active');
  const pillLabel = $('running-pill-label');
  if (pillLabel) pillLabel.textContent = 'Агент';
}

function clearToolChips() {
  hideToolChipTooltip();
  $('tool-chips').innerHTML = '';
  state.pendingTools = [];
}

function formatFileSize(bytes) {
  const n = Number(bytes) || 0;
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

function renderAttachments() {
  const wrap = $('composer-attachments');
  if (!wrap) return;
  wrap.innerHTML = '';
  wrap.hidden = state.attachments.length === 0;
  state.attachments.forEach((att, idx) => {
    const chip = document.createElement('span');
    chip.className = `attachment-chip ${att.status}`;
    const name = document.createElement('span');
    name.className = 'attachment-chip-name';
    name.textContent = att.status === 'error' ? `${att.file.name} — ошибка` : att.file.name;
    name.title = att.status === 'error' ? att.error || '' : `${att.file.name} (${formatFileSize(att.file.size)})`;
    chip.appendChild(name);
    if (att.status === 'uploading') {
      const spinner = document.createElement('span');
      spinner.textContent = '…';
      chip.appendChild(spinner);
    }
    const remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'attachment-chip-remove';
    remove.setAttribute('aria-label', `Убрать файл ${att.file.name}`);
    remove.textContent = '×';
    remove.addEventListener('click', () => removeAttachment(idx));
    chip.appendChild(remove);
    wrap.appendChild(chip);
  });
}

function removeAttachment(idx) {
  state.attachments.splice(idx, 1);
  renderAttachments();
}

async function uploadAttachment(att) {
  att.status = 'uploading';
  renderAttachments();
  try {
    const chatId = state.activeId || (await ensureActiveChat());
    if (!chatId) throw new Error('Нет активного чата');
    const fd = new FormData();
    fd.append('file', att.file, att.file.name);
    const res = await api(`/chats/${encodeURIComponent(chatId)}/files`, { method: 'POST', body: fd });
    att.path = res.path;
    att.name = res.name || att.file.name;
    att.size = res.size;
    att.status = 'done';
  } catch (e) {
    att.status = 'error';
    att.error = e.message || String(e);
    toast(`Не удалось загрузить файл «${att.file.name}»: ${att.error}`, true);
  }
  renderAttachments();
}

function onFilesSelected(ev) {
  const files = Array.from(ev.target.files || []);
  ev.target.value = '';
  for (const file of files) {
    const att = { file, status: 'pending', path: '', name: file.name, size: file.size, error: '' };
    state.attachments.push(att);
    void uploadAttachment(att);
  }
  renderAttachments();
}

function toolNameFromPayload(p) {
  return String(p.name ?? p.tool ?? p.tool_name ?? 'tool');
}

function buildToolChipMeta(p) {
  const name = toolNameFromPayload(p);
  const action = String(p.action || name).trim();
  const details = Array.isArray(p.details) ? p.details.map((d) => String(d).trim()).filter(Boolean) : [];
  return { name, action, details };
}

let toolTipAnchor = null;

function hideToolChipTooltip() {
  toolTipAnchor = null;
  const tip = $('tool-chip-tooltip');
  if (tip) tip.hidden = true;
}

function showToolChipTooltip(el, meta) {
  const tip = $('tool-chip-tooltip');
  if (!tip || !el) return;
  toolTipAnchor = el;
  const detailItems = meta.details.length
    ? meta.details.map((line) => `<li>${escapeHtml(line)}</li>`).join('')
    : '';
  tip.innerHTML = `<p class="tool-chip-tooltip-action">${escapeHtml(meta.action)}</p>${
    detailItems ? `<ul class="tool-chip-tooltip-details">${detailItems}</ul>` : ''
  }`;
  tip.hidden = false;
  positionToolChipTooltip(el);
}

function positionToolChipTooltip(el) {
  const tip = $('tool-chip-tooltip');
  if (!tip || tip.hidden || !el) return;
  const rect = el.getBoundingClientRect();
  const margin = 8;
  tip.style.left = '0';
  tip.style.top = '0';
  tip.hidden = false;
  const tipRect = tip.getBoundingClientRect();
  let left = rect.left + rect.width / 2 - tipRect.width / 2;
  let top = rect.top - tipRect.height - margin;
  if (top < margin) {
    top = rect.bottom + margin;
  }
  left = Math.max(margin, Math.min(left, window.innerWidth - tipRect.width - margin));
  top = Math.max(margin, Math.min(top, window.innerHeight - tipRect.height - margin));
  tip.style.left = `${Math.round(left)}px`;
  tip.style.top = `${Math.round(top)}px`;
}

function bindToolChipTooltip(el, meta) {
  const show = () => showToolChipTooltip(el, meta);
  const hide = () => hideToolChipTooltip();
  el.addEventListener('mouseenter', show);
  el.addEventListener('mouseleave', hide);
  el.addEventListener('focus', show);
  el.addEventListener('blur', hide);
  el.tabIndex = 0;
  el.setAttribute('role', 'button');
  const label = [meta.action, ...meta.details].filter(Boolean).join('. ');
  el.setAttribute('aria-label', `${meta.name}: ${label}`);
}

function onToolStart(p) {
  const meta = buildToolChipMeta(p);
  setAgentStatus('tool', `Вызов инструмента: ${meta.name}`);
  const el = document.createElement('span');
  el.className = 'tool-chip pending';
  el.innerHTML = `<span class="tool-chip-label">инстр.</span> <strong class="tool-chip-name">${escapeHtml(meta.name)}</strong>`;
  bindToolChipTooltip(el, meta);
  $('tool-chips').appendChild(el);
  state.pendingTools.push({ name: meta.name, el, meta });
}

function onToolDone(p) {
  const name = toolNameFromPayload(p);
  const idx = state.pendingTools.findIndex((t) => t.name === name && !t.el.classList.contains('done'));
  if (idx >= 0) {
    state.pendingTools[idx].el.classList.remove('pending');
    state.pendingTools[idx].el.classList.add('done');
  }
  setAgentStatus('tool', `Готово: ${name}`);
}

function disconnectES() {
  if (state.es) {
    state.es.close();
    state.es = null;
  }
}

function handleStreamEvent(kind, payload) {
  switch (kind) {
    case 'status':
      setAgentStatus(payload.phase, payload.message);
      break;
    case 'stderr':
      setAgentStatus('log', payload.line ?? payload.message);
      break;
    case 'assistant_text': {
      const piece = payload.text ?? payload.content ?? payload.delta ?? payload.chunk ?? payload._raw ?? '';
      if (!state.streamBuf) {
        setAgentStatus('stream', 'Модель формирует ответ…');
      }
      state.streamBuf += String(piece);
      const el = $('msg-streaming');
      if (el) {
        el.innerHTML = `<span class="msg-role">${escapeHtml(ROLE_RU.assistant)}</span><span class="msg-body">${renderMarkdown(state.streamBuf)}</span>`;
        el.parentElement.scrollTop = el.parentElement.scrollHeight;
      } else {
        renderMessages();
      }
      break;
    }
    case 'tool_start':
      onToolStart(payload);
      break;
    case 'tool_done':
      onToolDone(payload);
      break;
    case 'done':
      void finishAgentTurn();
      break;
    case 'error':
      toast(String(payload.message || payload.error || 'Ошибка агента'), true);
      void finishAgentTurn();
      break;
    case 'approval_prompt':
      showApprovalPrompt(payload);
      break;
    case 'approval_resolved':
    case 'approval_result':
      hideApprovalBar();
      break;
    case 'approval_summary':
      hideApprovalBar();
      toast(`Согласования: применено ${payload.applied ?? 0}, пропущено ${payload.skipped ?? 0}`);
      break;
    default:
      break;
  }
}

function wireSSE(es) {
  const kinds = [
    'assistant_text',
    'tool_start',
    'tool_done',
    'status',
    'stderr',
    'done',
    'error',
    'approval_prompt',
    'approval_resolved',
    'approval_result',
    'approval_summary',
  ];
  for (const k of kinds) {
    es.addEventListener(k, (e) => handleStreamEvent(k, parseSSEPayload(e)));
  }
  es.onmessage = (e) => {
    let o = parseSSEPayload(e);
    const t = o.type || o.event || o.kind;
    if (t && kinds.includes(String(t))) {
      handleStreamEvent(String(t), o);
      return;
    }
    if (o.assistant_text != null) {
      handleStreamEvent('assistant_text', { text: o.assistant_text });
    }
  };
  es.onerror = () => {
    /* browser will retry; avoid spam */
  };
}

function connectES(chatId) {
  disconnectES();
  const url = `${API}/chats/${encodeURIComponent(chatId)}/events`;
  const es = new EventSource(url);
  state.es = es;
  wireSSE(es);
  es.addEventListener('open', () => {
    void syncApprovalPrompt(chatId);
  });
}

async function syncApprovalPrompt(chatId) {
  try {
    const prompt = await api(`/chats/${encodeURIComponent(chatId)}/approval`);
    if (state.activeId === chatId) showApprovalPrompt(prompt);
  } catch (e) {
    if (e?.status !== 404) {
      toast(e.message || String(e), true);
    }
  }
}

function syncRunningFromDetail() {
  state.agentRunning = !!state.detail?.running;
}

function refreshSendState() {
  const hasChat = !!state.activeId;
  const busy = state.agentRunning;
  const domain = getSelectedDomain();
  const gameId = getSelectedGameId();
  const canCompose =
    !busy && (hasChat || (isLoggedInOnDomain(domain) && gameId > 0));
  $('message-input').disabled = !canCompose;
  $('btn-send').disabled = !canCompose;
  const attachBtn = $('btn-attach');
  if (attachBtn) attachBtn.disabled = !canCompose;
  $('btn-export').disabled = !hasChat;
  $('btn-cancel').disabled = !hasChat || !busy;
  const modeSel = $('field-security-mode');
  if (modeSel) {
    modeSel.disabled = !hasChat;
    syncSecurityModeVisual();
  }
  const pill = $('running-pill');
  if (pill) pill.hidden = !busy;
  if (!busy) {
    clearAgentStatus();
  } else if (busy && !state.agentStatus.message) {
    setAgentStatus('start', 'Агент работает…');
  }
  renderComposerPlaceholder();
}

function renderApprovalDetails(p) {
  const items = [];
  if (Array.isArray(p.details) && p.details.length) {
    for (const line of p.details) {
      if (line) items.push(`<li>${escapeHtml(line)}</li>`);
    }
  } else if (p.summary) {
    items.push(`<li>${escapeHtml(p.summary)}</li>`);
  }
  if (!items.length) return '';
  return `<ul class="approval-details">${items.join('')}</ul>`;
}

function showApprovalPrompt(p) {
  state.approvalPrompt = p;
  const bar = $('approval-bar');
  const body = $('approval-body');
  if (!bar || !body) return;
  if (p.kind === 'tool') {
    const action = p.action || p.summary || p.tool || '';
    body.innerHTML = `<div class="approval-head">
        <span class="approval-kicker">Согласование</span>
        <span class="approval-tool">${escapeHtml(p.tool || '')}</span>
      </div>
      <p class="approval-action">${escapeHtml(action)}</p>
      ${renderApprovalDetails(p)}`;
    bar.hidden = false;
    setApprovalButtonsDisabled(false);
    bar.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    return;
  }
  const steps = Array.isArray(p.steps) ? p.steps.map((s) => `<li>${escapeHtml(s)}</li>`).join('') : '';
  body.innerHTML = `<div class="approval-head">
      <span class="approval-kicker">Правка</span>
      <strong>${escapeHtml(p.title || 'Предложение')}</strong>
    </div>
    <p class="approval-action">${escapeHtml(p.summary || '')}</p>
    ${steps ? `<ul class="approval-details">${steps}</ul>` : ''}`;
  bar.hidden = false;
  setApprovalButtonsDisabled(false);
  bar.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
}

function setApprovalButtonsDisabled(disabled) {
  const bar = $('approval-bar');
  if (!bar) return;
  bar.querySelectorAll('.approval-actions button').forEach((btn) => {
    btn.disabled = disabled;
  });
}

function hideApprovalBar() {
  state.approvalPrompt = null;
  const bar = $('approval-bar');
  if (bar) {
    bar.hidden = true;
    setApprovalButtonsDisabled(false);
  }
}

async function postApproval(action) {
  if (!state.activeId) return;
  setApprovalButtonsDisabled(true);
  try {
    await api(`/chats/${encodeURIComponent(state.activeId)}/approval`, {
      method: 'POST',
      body: { action },
    });
    hideApprovalBar();
  } catch (e) {
    setApprovalButtonsDisabled(false);
    toast(e.message || String(e), true);
  }
}

async function cancelAgent() {
  if (!state.activeId) return;
  try {
    await api(`/chats/${encodeURIComponent(state.activeId)}/cancel`, { method: 'POST' });
    toast('Отменено.');
    state.agentRunning = false;
    refreshSendState();
  } catch (e) {
    toast(e.message || String(e), true);
  }
}

function exportChat(format) {
  if (!state.activeId) return;
  const url = `${API}/chats/${encodeURIComponent(state.activeId)}/export?format=${encodeURIComponent(format)}`;
  window.open(url, '_blank');
}

function toggleTheme() {
  const root = document.documentElement;
  const next = root.dataset.theme === 'light' ? 'dark' : 'light';
  root.dataset.theme = next;
  localStorage.setItem('encli-theme', next);
}

/** Below this width the sidebar is an off-canvas drawer, not a grid column. */
function isMobileViewport() {
  return window.matchMedia('(max-width: 760px)').matches;
}

function setSidebarOpen(open, persist = true) {
  const root = document.documentElement;
  root.dataset.sidebar = open ? 'open' : 'closed';
  const btn = $('btn-sidebar-toggle');
  if (btn) btn.setAttribute('aria-expanded', String(open));
  const backdrop = $('sidebar-backdrop');
  if (backdrop) backdrop.hidden = !(open && isMobileViewport());
  if (persist) localStorage.setItem('encli-sidebar', open ? '1' : '0');
}

function toggleSidebar() {
  const open = document.documentElement.dataset.sidebar === 'open';
  setSidebarOpen(!open);
}

function initSidebarState() {
  const saved = localStorage.getItem('encli-sidebar');
  const open = saved != null ? saved === '1' : !isMobileViewport();
  setSidebarOpen(open, false);
}

function toggleAuthPanel() {
  const panel = $('auth-panel');
  const btn = $('btn-auth-toggle');
  if (!panel || !btn) return;
  const open = !panel.classList.contains('is-open');
  panel.classList.toggle('is-open', open);
  btn.setAttribute('aria-expanded', String(open));
}

function stopRunningPoll() {
  if (state.runningPoll) {
    clearInterval(state.runningPoll);
    state.runningPoll = null;
  }
}

function startRunningPoll(chatId) {
  stopRunningPoll();
  state.runningPoll = setInterval(() => {
    void pollAgentRunning(chatId);
  }, 2000);
}

async function pollAgentRunning(chatId) {
  if (!state.agentRunning || state.activeId !== chatId) {
    stopRunningPoll();
    return;
  }
  try {
    const detail = await api(`/chats/${encodeURIComponent(chatId)}`);
    if (!detail.running) {
      state.detail = detail;
      await finishAgentTurn();
    }
  } catch {
    /* ignore transient errors */
  }
}

async function finishAgentTurn() {
  stopRunningPoll();
  state.streamBuf = '';
  clearToolChips();
  state.agentRunning = false;
  try {
    await loadChats();
    if (state.activeId) {
      const detail = await api(`/chats/${encodeURIComponent(state.activeId)}`);
      state.detail = detail;
      renderMessages();
      state.agentRunning = !!state.detail?.running;
    }
  } catch (e) {
    toast(e.message || String(e), true);
    state.agentRunning = false;
  }
  refreshSendState();
}

/** Switches to a different chat than the one currently active, discarding any
 * staged attachments — unlike selectChat, which ensureActiveChat also calls
 * mid-upload to create a chat on demand, when attachments must survive. */
async function switchChat(chatId) {
  state.attachments = [];
  renderAttachments();
  if (isMobileViewport()) setSidebarOpen(false);
  await selectChat(chatId);
}

async function selectChat(chatId) {
  state.activeId = chatId;
  renderChatList();
  disconnectES();
  clearToolChips();
  hideApprovalBar();
  state.streamBuf = '';
  if (!chatId) {
    state.detail = null;
    $('btn-patch-chat').disabled = true;
    renderMessages();
    renderAuth();
    refreshSendState();
    return;
  }
  $('btn-patch-chat').disabled = false;
  try {
    const detail = await api(`/chats/${encodeURIComponent(chatId)}`);
    state.detail = detail;
    const domain = detail.domain ?? '';
    if (domain && $('field-domain')) {
      const domSel = $('field-domain');
      if (![...domSel.options].some((o) => o.value === domain)) {
        const o = document.createElement('option');
        o.value = domain;
        o.textContent = domain;
        domSel.appendChild(o);
      }
      domSel.value = domain;
    }
    await loadGamesForDomain(domain);
    await fillGameSelect(state.catalogGames, detail.game_id);
    syncSecurityModeFromDetail(detail);
    syncRunningFromDetail();
    if (state.agentRunning) {
      setAgentStatus('llm_wait', 'Агент выполняет задачу…');
      startRunningPoll(chatId);
    }
    renderMessages();
    renderAuth();
    connectES(chatId);
    await syncApprovalPrompt(chatId);
    refreshSendState();
  } catch (e) {
    toast(e.message || String(e), true);
    state.detail = null;
    renderMessages();
    refreshSendState();
  }
}

async function patchActiveChat() {
  if (!state.activeId) return;
  try {
    await api(`/chats/${encodeURIComponent(state.activeId)}`, {
      method: 'PATCH',
      body: {
        domain: getSelectedDomain(),
        game_id: getSelectedGameId(),
      },
    });
    toast('Чат сохранён.');
    await loadChats();
    await selectChat(state.activeId);
  } catch (e) {
    toast(e.message || String(e), true);
  }
}

async function createChat() {
  try {
    const body = {
      domain: getSelectedDomain(),
      game_id: getSelectedGameId(),
    };
    const created = await api('/chats', { method: 'POST', body });
    const id = created?.id != null ? String(created.id) : created?.chat?.id != null ? String(created.chat.id) : null;
    if (!id) {
      toast('Не удалось создать чат: нет id в ответе', true);
      await loadChats();
      return;
    }
    await loadChats();
    await switchChat(id);
    toast('Новый чат создан.');
  } catch (e) {
    toast(e.message || String(e), true);
  }
}

async function sendMessage() {
  const input = $('message-input');
  const text = input.value.trim();
  if (state.agentRunning) return;
  if (state.attachments.some((a) => a.status === 'uploading')) {
    toast('Файлы ещё загружаются…', true);
    return;
  }
  const failed = state.attachments.filter((a) => a.status === 'error');
  const ready = state.attachments.filter((a) => a.status === 'done');
  if (!text && ready.length === 0) return;
  if (!state.activeId) {
    const id = await ensureActiveChat();
    if (!id) {
      toast('Выберите игру и войдите на домен.', true);
      return;
    }
  }
  clearToolChips();
  try {
    const files = ready.map((a) => ({ path: a.path, name: a.name }));
    await api(`/chats/${encodeURIComponent(state.activeId)}/messages`, {
      method: 'POST',
      body: { content: text, files },
    });
    input.value = '';
    state.attachments = failed;
    renderAttachments();
    state.agentRunning = true;
    clearToolChips();
    setAgentStatus('start', 'Запуск агента…');
    startRunningPoll(state.activeId);
    state.streamBuf = '';
    const detail = await api(`/chats/${encodeURIComponent(state.activeId)}`);
    state.detail = detail;
    renderMessages();
    refreshSendState();
    await loadChats();
  } catch (e) {
    toast(e.message || String(e), true);
    state.agentRunning = false;
    refreshSendState();
  }
}

async function onLoginSubmit(ev) {
  ev.preventDefault();
  const fd = new FormData(ev.target);
  const domain = String(fd.get('domain') || '').trim();
  const login = String(fd.get('login') || '').trim();
  const password = String(fd.get('password') || '');
  if (!domain || !login || !password) return;
  try {
    await api('/auth/login', { method: 'POST', body: { domain, login, password } });
    toast(`Вход выполнен (${domain}).`);
    await loadAuthStatus();
    const domSel = $('field-domain');
    if (domSel) domSel.value = domain;
    await loadGamesForDomain(domain);
    await onChatContextChanged();
  } catch (e) {
    toast(e.message || String(e), true);
  }
}

async function logout() {
  const domain =
    getSelectedDomain() ||
    $('login-form')?.querySelector('input[name="domain"]')?.value?.trim() ||
    '';
  try {
    await api('/auth/logout', {
      method: 'POST',
      body: domain ? { domain } : {},
    });
    toast('Выход выполнен.');
  } catch (e) {
    if (e.status === 404) {
      toast('Выход не реализован (404).', true);
    } else {
      toast(e.message || String(e), true);
    }
  }
  await loadAuthStatus();
  await fillDomainSelect();
}

/* —— Настройки LLM —— */

const LLM_OVERRIDE_FLAGS = {
  auth_method: '--llm-auth',
};

const LLM_OVERRIDE_FIELDS = ['auth_method', 'base_url', 'model', 'api_key'];

const LLM_TRANSPORT_RU = {
  codex: 'подписка ChatGPT',
  gigachat: 'GigaChat',
  apikey: 'OpenAI-совместимый провайдер',
};

/** Опрос статуса входа через ChatGPT: раз в 2 с, не дольше пяти минут — столько
 * же живёт поток на стороне сервера (codexLoginTTL). */
const CODEX_POLL_MS = 2000;
const CODEX_LOGIN_TTL_MS = 5 * 60 * 1000;

/** Вход через ChatGPT нарисован дважды: в модалке настроек и в мастере первого
 * запуска. Оба набора элементов всегда лежат в DOM, виден только один, а поток
 * входа на всё приложение один — поэтому каждая отрисовка обновляет обе
 * площадки, вместо второго поллера и второго state.codexFlow. */
const CODEX_TARGETS = [
  { status: 'llm-codex-status', progress: 'llm-codex-progress', manual: 'llm-codex-manual', code: 'llm-codex-code', login: 'btn-codex-login' },
  {
    status: 'onboarding-codex-status',
    progress: 'onboarding-codex-progress',
    manual: 'onboarding-codex-manual',
    code: 'onboarding-codex-code',
    login: 'btn-onboarding-codex-login',
  },
];

let llmLastFocus = null;

function transportLabel(method) {
  return LLM_TRANSPORT_RU[method] || LLM_TRANSPORT_RU.apikey;
}

/** Вкладка, открытая при загрузке: сохранённое значение важнее действующего,
 * чтобы форма показывала то, что она же и перезапишет. Пустое сохранённое
 * значение остаётся пустым — при сохранении оно не превратится в «apikey». */
function initialAuthMethod(data) {
  const stored = String(data?.stored?.auth_method || '').trim();
  if (stored) return stored;
  const effective = String(data?.effective?.auth_method?.value || '').trim();
  return effective === 'codex' || effective === 'apikey' ? effective : '';
}

function isModalOpen() {
  const modal = $('llm-modal');
  return !!modal && !modal.hidden;
}

/** Виден ли элемент оператору. У скрытого поддерева нет прямоугольников — чем
 * бы его ни прятали: атрибутом hidden, display:none или закрытым <details>. */
function isElementVisible(el) {
  return !!el && el.getClientRects().length > 0;
}

/** Видимые фокусируемые элементы контейнера. Ловушка фокуса нужна и модалке
 * настроек LLM, и мастеру первого запуска, поэтому контейнер приходит
 * аргументом, а не прибит к одному диалогу. */
function modalFocusable(container) {
  if (!container) return [];
  const sel =
    'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex]:not([tabindex="-1"])';
  return [...container.querySelectorAll(sel)].filter(isElementVisible);
}

function trapModalFocus(e, container) {
  const items = modalFocusable(container);
  if (!items.length) return;
  const first = items[0];
  const last = items[items.length - 1];
  const active = document.activeElement;
  if (e.shiftKey && (active === first || !items.includes(active))) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && active === last) {
    e.preventDefault();
    first.focus();
  }
}

function onLLMModalKeydown(e) {
  if (!isModalOpen()) return;
  if (e.key === 'Escape') {
    e.preventDefault();
    e.stopPropagation();
    closeLLMModal();
    return;
  }
  if (e.key === 'Tab') {
    e.stopPropagation();
    trapModalFocus(e, $('llm-modal-dialog'));
  }
}

async function openLLMModal() {
  const modal = $('llm-modal');
  if (!modal || !modal.hidden) return;
  llmLastFocus = document.activeElement;
  modal.hidden = false;
  document.body.classList.add('is-modal-open');
  document.addEventListener('keydown', onLLMModalKeydown, true);
  renderLLMSettings();
  try {
    const data = await api('/llm/settings');
    applyLLMSnapshot(data);
  } catch (e) {
    toast(`Настройки LLM: ${e.message || String(e)}`, true);
  }
  const items = modalFocusable($('llm-modal-dialog'));
  (items[0] || modal).focus?.();
}

function closeLLMModal() {
  const modal = $('llm-modal');
  if (!modal || modal.hidden) return;
  modal.hidden = true;
  // Мастер первого запуска мог остаться открытым под модалкой: прокрутку
  // возвращает тот, кто закрывается последним.
  if (!isOnboardingOpen()) document.body.classList.remove('is-modal-open');
  document.removeEventListener('keydown', onLLMModalKeydown, true);
  llmLastFocus?.focus?.();
  llmLastFocus = null;
}

/** Принимает свежий снимок настроек. keepEdits оставляет незасохранённый ввод —
 * его используют обновления, вызванные входом/выходом ChatGPT, а не формой. */
function applyLLMSnapshot(data, opts = {}) {
  state.llm = data || null;
  if (!opts.keepEdits) {
    state.llmEdited = { base_url: false, model: false };
    state.llmAuth = initialAuthMethod(data);
    const key = $('llm-api-key');
    if (key) key.value = '';
  }
  renderLLMSettings();
}

function setTabState(btn, active, reachable) {
  if (!btn) return;
  btn.classList.toggle('is-active', active);
  btn.setAttribute('aria-selected', active ? 'true' : 'false');
  btn.tabIndex = active || reachable ? 0 : -1;
}

function selectLLMTab(method) {
  state.llmAuth = method;
  renderLLMSettings();
  const pane = $(method === 'codex' ? 'llm-pane-codex' : 'llm-pane-apikey');
  pane?.focus?.();
}

/** Узлы плашки «значение задано снаружи». Одна и та же лексика нужна и модалке
 * настроек (по слоту на поле, без подписи), и мастеру (общий слот, поэтому
 * каждая плашка называет своё поле). */
function overrideBadgeNodes(o, flags, fieldLabel) {
  const flag = flags[o.field];
  const source =
    o.source === 'flag'
      ? flag
        ? `флагом ${flag}`
        : 'флагом командной строки'
      : `переменной ${o.env_var || 'окружения'}`;
  const badge = document.createElement('span');
  badge.className = 'llm-override' + (o.shadows_stored ? ' is-shadowing' : '');
  badge.textContent = fieldLabel ? `${fieldLabel}: переопределено ${source}` : `переопределено ${source}`;
  const nodes = [badge];
  if (o.shadows_stored) {
    const note = document.createElement('span');
    note.className = 'llm-override-note';
    note.textContent = 'сохранённое здесь значение не применяется';
    nodes.push(note);
  }
  return nodes;
}

function renderLLMOverrides(list) {
  for (const field of LLM_OVERRIDE_FIELDS) {
    const slot = $(`llm-override-${field}`);
    if (slot) slot.innerHTML = '';
  }
  for (const o of Array.isArray(list) ? list : []) {
    const slot = $(`llm-override-${o.field}`);
    if (!slot) continue;
    slot.append(...overrideBadgeNodes(o, LLM_OVERRIDE_FLAGS, ''));
  }
}

function formatCodexExpiry(raw) {
  const ts = Date.parse(String(raw || ''));
  if (!Number.isFinite(ts)) return String(raw || '');
  return new Date(ts).toLocaleString('ru-RU');
}

function renderCodexStatus(codex) {
  const c = codex || {};
  const rows = [];
  let cls = 'llm-codex-status';
  let title = 'Не выполнен вход';
  if (c.signed_in) {
    cls += c.expired ? ' is-warn' : ' is-ok';
    title = c.expired ? 'Вход выполнен, срок действия истёк' : 'Вход выполнен';
    if (c.account_id) rows.push(`Аккаунт: ${c.account_id}`);
    if (c.expires_at) rows.push(`Действует до: ${formatCodexExpiry(c.expires_at)}`);
  } else {
    cls += ' is-off';
  }
  if (c.error) {
    cls += ' is-warn';
    rows.push(`Ошибка: ${c.error}`);
  }
  if (c.path) rows.push(`Файл: ${c.path}`);
  const html = `<p class="llm-codex-title">${escapeHtml(title)}</p>${
    rows.length ? `<ul class="llm-codex-rows">${rows.map((r) => `<li>${escapeHtml(r)}</li>`).join('')}</ul>` : ''
  }`;
  for (const target of CODEX_TARGETS) {
    const box = $(target.status);
    if (!box) continue;
    box.className = cls;
    box.innerHTML = html;
  }
}

function renderLLMSettings() {
  const data = state.llm;
  const defaults = data?.defaults || {};
  const stored = data?.stored || {};
  const method = state.llmAuth;
  const isCodex = method === 'codex';
  const isOther = method !== '' && method !== 'codex' && method !== 'apikey';

  setTabState($('llm-tab-codex'), isCodex, isOther);
  setTabState($('llm-tab-apikey'), !isCodex && !isOther, isOther);
  const codexPane = $('llm-pane-codex');
  const apikeyPane = $('llm-pane-apikey');
  if (codexPane) codexPane.hidden = !isCodex;
  if (apikeyPane) apikeyPane.hidden = isCodex;

  const note = $('llm-transport-note');
  if (note) {
    note.hidden = !isOther;
    note.textContent = isOther
      ? `Сохранён транспорт «${method}» — он настраивается через переменные окружения. Выбор вкладки заменит его.`
      : '';
  }

  const baseInput = $('llm-base-url');
  if (baseInput) {
    baseInput.placeholder = defaults.base_url || '';
    if (!state.llmEdited.base_url) baseInput.value = stored.base_url || '';
  }
  const modelInput = $('llm-model');
  if (modelInput) {
    modelInput.placeholder = defaults.model || '';
    if (!state.llmEdited.model) modelInput.value = stored.model || '';
  }
  const keyInput = $('llm-api-key');
  const keyHint = $('llm-api-key-hint');
  if (keyInput) {
    keyInput.placeholder = stored.has_api_key ? stored.api_key_masked || '••••' : 'sk-…';
  }
  if (keyHint) {
    keyHint.textContent = stored.has_api_key
      ? 'Ключ сохранён. Пустое поле при сохранении его не затирает.'
      : 'Ключ не сохранён.';
  }
  const clearBtn = $('btn-llm-clear-key');
  if (clearBtn) clearBtn.disabled = !stored.has_api_key;

  renderLLMOverrides(data?.env_overrides);
  renderCodexStatus(data?.codex);

  const pending = !!state.codexFlow;
  for (const target of CODEX_TARGETS) {
    const loginBtn = $(target.login);
    if (loginBtn) loginBtn.disabled = pending;
  }
  const logoutBtn = $('btn-codex-logout');
  if (logoutBtn) logoutBtn.disabled = !data?.codex?.signed_in;
  const cancelBtn = $('btn-codex-cancel');
  if (cancelBtn) cancelBtn.hidden = !pending;

  const agent = data?.agent || {};
  const summary = $('llm-summary');
  if (summary) {
    const model = String(agent.model || '').trim() || '—';
    const where = String(agent.base_url || '').trim() || transportLabel(agent.auth_method || method);
    summary.textContent = `Сейчас используется: ${model} · ${where}`;
  }
  const pathEl = $('llm-settings-path');
  if (pathEl) {
    pathEl.textContent = data?.settings_path ? `Файл настроек: ${data.settings_path}` : '';
  }

  const alert = $('llm-alert');
  if (alert) {
    // Нечитаемый файл настроек ломает и чтение, и резолв конфига агента — один
    // и тот же текст приходит дважды, показывать его дважды незачем.
    const problems = [...new Set([data?.error, agent.error].map((x) => String(x || '').trim()).filter(Boolean))];
    alert.hidden = problems.length === 0;
    alert.innerHTML = problems.map((p) => `<p>${escapeHtml(p)}</p>`).join('');
  }
}

function setLLMBusy(busy) {
  for (const id of ['btn-llm-save', 'btn-llm-reset', 'btn-llm-clear-key']) {
    const el = $(id);
    if (el) el.disabled = busy;
  }
  if (!busy) renderLLMSettings();
}

function llmFormBody(extra) {
  return {
    auth_method: state.llmAuth,
    base_url: ($('llm-base-url')?.value || '').trim(),
    model: ($('llm-model')?.value || '').trim(),
    api_key: ($('llm-api-key')?.value || '').trim(),
    clear_api_key: false,
    ...extra,
  };
}

async function putLLMSettings(extra, successMsg) {
  setLLMBusy(true);
  try {
    const data = await api('/llm/settings', { method: 'PUT', body: llmFormBody(extra) });
    applyLLMSnapshot(data);
    toast(successMsg);
    void loadAgentConfig();
  } catch (e) {
    toast(e.message || String(e), true);
  } finally {
    setLLMBusy(false);
  }
}

async function saveLLMSettings() {
  await putLLMSettings(undefined, 'Настройки LLM сохранены.');
}

async function clearLLMAPIKey() {
  if (!window.confirm('Удалить сохранённый API-ключ?')) return;
  await putLLMSettings({ api_key: '', clear_api_key: true }, 'API-ключ удалён.');
}

async function resetLLMSettings() {
  if (!window.confirm('Сбросить все сохранённые настройки LLM?')) return;
  setLLMBusy(true);
  try {
    const data = await api('/llm/settings', { method: 'DELETE' });
    applyLLMSnapshot(data);
    toast('Настройки LLM сброшены.');
    void loadAgentConfig();
  } catch (e) {
    toast(e.message || String(e), true);
  } finally {
    setLLMBusy(false);
  }
}

/** Ссылка собирается через DOM, а не через innerHTML: escapeHtml не экранирует
 * кавычки, а здесь значение попало бы в атрибут href. */
function setCodexProgress(text, linkURL) {
  const withLink = /^https?:\/\//i.test(String(linkURL || ''));
  for (const target of CODEX_TARGETS) {
    const el = $(target.progress);
    if (!el) continue;
    el.textContent = '';
    el.hidden = !text;
    if (!text) continue;
    el.append(text);
    if (!withLink) continue;
    el.append(' Если вкладка не открылась — ');
    const a = document.createElement('a');
    a.href = linkURL;
    a.target = '_blank';
    a.rel = 'noopener';
    a.textContent = 'откройте ссылку вручную';
    el.append(a, '.');
  }
}

function stopCodexPoll() {
  if (state.codexPoll) {
    clearInterval(state.codexPoll);
    state.codexPoll = null;
  }
}

async function refreshLLMAfterAuth() {
  try {
    const data = await api('/llm/settings');
    applyLLMSnapshot(data, { keepEdits: true });
    // Вход мог быть начат изнутри мастера: тогда его плашки и подсказки описывают
    // состояние до входа. Снимок уже здесь — второй запрос за тем же не нужен.
    if (isOnboardingOpen()) fillOnboardingLLM(data, { keepEdits: true });
  } catch (e) {
    toast(`Настройки LLM: ${e.message || String(e)}`, true);
  }
  void loadAgentConfig();
}

/** Успешный вход обязан ещё и записать транспорт: автовыбор подписки на стороне
 * сервера срабатывает, только когда не задано вообще ничего (web_agent.go), и у
 * пользователя с сохранённым api_key вход без записи оставил бы агента на старом
 * провайдере, пока панель показывает «Вход выполнен».
 *
 * Пишутся сохранённые base_url/model, а не содержимое формы: поток мог
 * завершиться, пока оператор правил поля, и сохранять его черновик молча — нет. */
async function persistCodexTransport() {
  // Без снимка писать нельзя: PUT перезаписывает base_url и model как есть, так
  // что пустые поля затёрли бы сохранённое. Начальный GET мог упасть — модалка
  // при этом открыта и вход доступен, — поэтому снимок сначала добирается.
  if (!state.llm) {
    try {
      applyLLMSnapshot(await api('/llm/settings'), { keepEdits: true });
    } catch (e) {
      return e.message || String(e);
    }
    if (!state.llm) return 'не удалось прочитать текущие настройки';
  }
  const stored = state.llm.stored || {};
  if (String(stored.auth_method || '') === 'codex') return '';
  try {
    await api('/llm/settings', {
      method: 'PUT',
      body: {
        auth_method: 'codex',
        // base_url подписка не использует, и он переживёт обратное переключение
        // на провайдера — а вот model очищается намеренно: codexModel() пропускает
        // без единого слова любое gpt-/o3-/o4-имя, поэтому сохранённое
        // `openai/gpt-4o-mini` ушло бы в бэкенд подписки, тот молча подменил бы
        // его своим дефолтом, а в отчёте стояло бы запрошенное имя.
        base_url: stored.base_url || '',
        model: '',
        api_key: '',
        clear_api_key: false,
      },
    });
    return '';
  } catch (e) {
    return e.message || String(e);
  }
}

async function finishCodexFlow(ok, message) {
  stopCodexPoll();
  state.codexFlow = null;
  setCodexProgress('');
  // Тост один на всё завершение: #toast — единственный элемент с перезаписью
  // текста, поэтому предупреждение, показанное перед сообщением об успехе,
  // прожило бы нулевое время и оператор увидел бы только «Вход выполнен».
  const failedToSave = ok ? await persistCodexTransport() : '';
  if (!ok) {
    toast(message || 'Вход через ChatGPT не удался.', true);
  } else if (failedToSave) {
    toast(`Вход выполнен, но транспорт не сохранён: ${failedToSave}`, true);
  } else {
    toast('Вход через ChatGPT выполнен.');
  }
  await refreshLLMAfterAuth();
}

function startCodexPoll() {
  stopCodexPoll();
  state.codexPoll = setInterval(() => void pollCodexFlow(), CODEX_POLL_MS);
}

async function pollCodexFlow() {
  const flow = state.codexFlow;
  if (!flow) {
    stopCodexPoll();
    return;
  }
  if (Date.now() > flow.deadline) {
    await finishCodexFlow(false, 'Вход через ChatGPT не завершён за 5 минут.');
    return;
  }
  try {
    const snap = await api(`/llm/codex/login/${encodeURIComponent(flow.id)}`);
    if (snap?.status === 'success') {
      await finishCodexFlow(true);
    } else if (snap?.status === 'error') {
      await finishCodexFlow(false, snap.error);
    }
  } catch (e) {
    if (e.status === 404) {
      await finishCodexFlow(false, 'Поток входа больше не существует.');
    }
    /* остальные ошибки считаем временными и продолжаем опрос */
  }
}

async function startCodexLogin(noBrowser) {
  if (state.codexFlow) return;
  // Вкладка открывается синхронно, до запроса: window.open после await уже вне
  // пользовательского жеста, и блокировщик всплывающих окон его отклонит.
  // Адрес известен только после ответа сервера, поэтому окно открывается пустым
  // и переадресуется ниже — а значит нужен handle, и флаг 'noopener' здесь не
  // годится: с ним window.open по спецификации возвращает null. Связь рвётся
  // вручную (popup.opener = null), пока вкладка ещё about:blank.
  const popup = noBrowser ? null : window.open('', '_blank');
  try {
    const flow = await api('/llm/codex/login', { method: 'POST', body: { no_browser: !!noBrowser } });
    if (!flow?.id) throw new Error('Сервер не вернул идентификатор входа');
    state.codexFlow = { id: flow.id, deadline: Date.now() + CODEX_LOGIN_TTL_MS };
    const url = String(flow.authorize_url || '');
    if (popup && url) {
      popup.opener = null;
      popup.location.replace(url);
    } else if (popup) {
      popup.close();
    }
    if (noBrowser) {
      for (const target of CODEX_TARGETS) {
        const manual = $(target.manual);
        if (manual) manual.open = true;
      }
      setCodexProgress('Откройте ссылку, завершите вход и вставьте redirect URL ниже.', url);
    } else {
      setCodexProgress('Ожидаем завершения входа в браузере…', url);
    }
    startCodexPoll();
    renderLLMSettings();
  } catch (e) {
    popup?.close();
    toast(e.message || String(e), true);
    // 409 — занят порт 1455 под redirect: остаётся ручной путь без слушателя.
    if (e.status === 409 && !noBrowser) {
      await startCodexLogin(true);
    }
  }
}

async function submitCodexCode() {
  // Полей ввода кода два — в модалке и в мастере; отправляем то, которое видит
  // оператор. «Первое непустое» отправило бы текст, оставшийся на закрытой
  // площадке от прошлой неудачной попытки, вместо только что вставленного.
  const input = CODEX_TARGETS.map((t) => $(t.code)).find(isElementVisible) || $(CODEX_TARGETS[0].code);
  const code = (input?.value || '').trim();
  if (!state.codexFlow) {
    toast('Сначала нажмите «Войти через ChatGPT».', true);
    return;
  }
  if (!code) {
    toast('Вставьте redirect URL или код.', true);
    return;
  }
  const id = state.codexFlow.id;
  try {
    await api(`/llm/codex/login/${encodeURIComponent(id)}/code`, { method: 'POST', body: { code } });
    await finishCodexFlow(true);
  } catch (e) {
    toast(e.message || String(e), true);
  } finally {
    // Поле очищается и после отказа: иначе отклонённое значение осталось бы
    // лежать и ушло бы снова, откуда бы следующую отправку ни начали.
    if (input) input.value = '';
  }
}

async function cancelCodexLogin() {
  const flow = state.codexFlow;
  if (!flow) return;
  stopCodexPoll();
  state.codexFlow = null;
  setCodexProgress('');
  try {
    await api(`/llm/codex/login/${encodeURIComponent(flow.id)}`, { method: 'DELETE' });
  } catch {
    /* поток мог уже завершиться сам — отменять нечего */
  }
  toast('Вход отменён.');
  renderLLMSettings();
}

async function codexLogout() {
  if (!window.confirm('Выйти из аккаунта ChatGPT?')) return;
  try {
    const codex = await api('/llm/codex/logout', { method: 'POST' });
    if (state.llm) state.llm.codex = codex;
    renderLLMSettings();
    toast('Выход из ChatGPT выполнен.');
    await refreshLLMAfterAuth();
  } catch (e) {
    toast(e.message || String(e), true);
  }
}

function bindLLMSettings() {
  $('btn-llm-settings')?.addEventListener('click', () => void openLLMModal());
  $('btn-llm-close')?.addEventListener('click', () => closeLLMModal());
  $('btn-llm-cancel')?.addEventListener('click', () => closeLLMModal());
  $('llm-modal')?.addEventListener('mousedown', (e) => {
    if (e.target === e.currentTarget) closeLLMModal();
  });
  $('llm-tab-codex')?.addEventListener('click', () => selectLLMTab('codex'));
  $('llm-tab-apikey')?.addEventListener('click', () => selectLLMTab('apikey'));
  for (const id of ['llm-tab-codex', 'llm-tab-apikey']) {
    $(id)?.addEventListener('keydown', (e) => {
      if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
      e.preventDefault();
      selectLLMTab(state.llmAuth === 'codex' ? 'apikey' : 'codex');
      $(state.llmAuth === 'codex' ? 'llm-tab-codex' : 'llm-tab-apikey')?.focus();
    });
  }
  $('llm-base-url')?.addEventListener('input', () => {
    state.llmEdited.base_url = true;
  });
  $('llm-model')?.addEventListener('input', () => {
    state.llmEdited.model = true;
  });
  $('btn-llm-save')?.addEventListener('click', () => void saveLLMSettings());
  $('btn-llm-reset')?.addEventListener('click', () => void resetLLMSettings());
  $('btn-llm-clear-key')?.addEventListener('click', () => void clearLLMAPIKey());
  $('btn-codex-login')?.addEventListener('click', () => void startCodexLogin(false));
  $('btn-codex-logout')?.addEventListener('click', () => void codexLogout());
  $('btn-codex-cancel')?.addEventListener('click', () => void cancelCodexLogin());
  $('btn-codex-code-submit')?.addEventListener('click', () => void submitCodexCode());
  $('llm-codex-code')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      void submitCodexCode();
    }
  });
}

/* —— Мастер первого запуска —— */

const ONBOARDING_STEPS = ['welcome', 'llm', 'engine', 'auth'];

const ONBOARDING_COPY = {
  welcome: { title: 'Настройка encli', subtitle: 'Три шага до первого запроса агенту' },
  llm: { title: 'Подключение к модели', subtitle: 'Шаг 1 из 3 — как агент обращается к языковой модели' },
  engine: { title: 'Движок домена', subtitle: 'Шаг 2 из 3 — с каким API en.cx работать' },
  auth: { title: 'Вход в en.cx', subtitle: 'Шаг 3 из 3 — учётная запись для доступа к играм' },
};

/** В мастере плашки «задано снаружи» лежат в одном слоте на шаг, поэтому каждая
 * называет своё поле — в модалке слот отдельный на поле, и подпись не нужна. */
const LLM_OVERRIDE_FIELD_RU = {
  auth_method: 'способ подключения',
  base_url: 'base URL',
  model: 'модель',
  api_key: 'API-ключ',
};

const ENGINE_OVERRIDE_FLAGS = {
  engine: '-engine',
  api_base_url: '-api-base-url',
};

const ENGINE_OVERRIDE_FIELD_RU = {
  engine: 'движок',
  api_base_url: 'адрес API',
};

const ENGINE_MODE_RU = {
  auto: 'автоопределение',
  legacy: 'старый движок',
  new: 'новый движок',
};

let onboardingLastFocus = null;

function isOnboardingOpen() {
  const overlay = $('onboarding');
  return !!overlay && !overlay.hidden;
}

/** Ошибка мастера живёт в подвале диалога, а не в тосте: мастер модальный, и
 * тост за ним легко пропустить. Пустой текст прячет плашку. Ссылка собирается
 * через DOM: escapeHtml не экранирует кавычки, а значение попало бы в href. */
function setOnboardingError(text, linkURL) {
  const box = $('onboarding-error');
  if (!box) return;
  box.textContent = '';
  const msg = String(text || '').trim();
  box.hidden = !msg;
  if (!msg) return;
  const p = document.createElement('p');
  p.append(msg);
  if (/^https?:\/\//i.test(String(linkURL || ''))) {
    p.append(' ');
    const a = document.createElement('a');
    a.href = linkURL;
    a.target = '_blank';
    a.rel = 'noopener';
    a.textContent = 'Открыть страницу проверки';
    p.append(a);
  }
  box.appendChild(p);
}

/** Блок .onboarding-result скрыт, пока пуст, поэтому очистка — это пустой текст. */
function setOnboardingResult(id, text, tone) {
  const box = $(id);
  if (!box) return;
  box.className = 'onboarding-result' + (tone ? ` is-${tone}` : '');
  box.textContent = String(text || '');
}

function renderOverridesInto(slotId, list, flags, labels) {
  const slot = $(slotId);
  if (!slot) return;
  slot.textContent = '';
  for (const o of Array.isArray(list) ? list : []) {
    slot.append(...overrideBadgeNodes(o, flags, labels[o.field] || o.field));
  }
}

function onboardingStepIndex(step) {
  const idx = ONBOARDING_STEPS.indexOf(step);
  return idx < 0 ? 0 : idx;
}

/** Рисует всё, что не зависит от данных шага: индикатор, панели, заголовки и
 * кнопки подвала. Состояние шага в индикаторе — ровно два класса. */
function renderOnboardingChrome() {
  const step = state.onboarding.step;
  const idx = onboardingStepIndex(step);
  const busy = state.onboarding.busy;

  for (const li of document.querySelectorAll('#onboarding-steps li[data-step]')) {
    const at = onboardingStepIndex(li.dataset.step);
    li.classList.toggle('is-active', at === idx);
    li.classList.toggle('is-done', at < idx);
  }
  for (const name of ONBOARDING_STEPS) {
    const pane = $(`onboarding-pane-${name}`);
    if (pane) pane.hidden = name !== step;
  }

  const copy = ONBOARDING_COPY[step] || ONBOARDING_COPY.welcome;
  const title = $('onboarding-title');
  if (title) title.textContent = copy.title;
  const subtitle = $('onboarding-subtitle');
  if (subtitle) subtitle.textContent = copy.subtitle;

  const back = $('btn-onboarding-back');
  if (back) back.disabled = busy || idx === 0;
  const next = $('btn-onboarding-next');
  if (next) next.disabled = busy;
  const nextLabel = $('onboarding-next-label');
  if (nextLabel) nextLabel.textContent = idx === ONBOARDING_STEPS.length - 1 ? 'Готово' : 'Далее';
  const skipAll = $('btn-onboarding-skip-all');
  if (skipAll) skipAll.disabled = busy;
}

function setOnboardingBusy(busy) {
  state.onboarding.busy = !!busy;
  renderOnboardingChrome();
}

async function goToOnboardingStep(step) {
  state.onboarding.step = step;
  setOnboardingError('');
  renderOnboardingChrome();
  $(`onboarding-pane-${step}`)?.focus?.();
  if (step === 'llm') {
    await loadOnboardingLLM();
  } else if (step === 'engine') {
    await loadOnboardingEngine();
  } else if (step === 'auth') {
    prefillOnboardingAuth();
  }
}

/* —— Шаг «Модель» —— */

function onboardingLLMTab() {
  return state.onboarding.llmAuth === 'apikey' ? 'apikey' : 'codex';
}

function renderOnboardingLLMTabs() {
  const isCodex = onboardingLLMTab() === 'codex';
  setTabState($('onboarding-llm-tab-codex'), isCodex, false);
  setTabState($('onboarding-llm-tab-apikey'), !isCodex, false);
  const codexPane = $('onboarding-llm-pane-codex');
  if (codexPane) codexPane.hidden = !isCodex;
  const apikeyPane = $('onboarding-llm-pane-apikey');
  if (apikeyPane) apikeyPane.hidden = isCodex;
}

function selectOnboardingLLMTab(method) {
  state.onboarding.llmAuth = method;
  renderOnboardingLLMTabs();
  $(method === 'codex' ? 'onboarding-llm-pane-codex' : 'onboarding-llm-pane-apikey')?.focus?.();
}

function onboardingKeyHint(keyStatus) {
  const c = keyStatus || {};
  if (!c.has_value) {
    return 'Ключ пока не сохранён. Подойдёт любой провайдер с OpenAI-совместимым API — ключ остаётся на этой машине.';
  }
  const masked = String(c.masked || '').trim() || '••••';
  if (c.source === 'flag') return `Ключ ${masked} задан флагом командной строки — поле ниже его не перебьёт.`;
  if (c.source === 'env') {
    return `Ключ ${masked} задан переменной ${c.env_var || 'окружения'} — поле ниже его не перебьёт.`;
  }
  return `Ключ ${masked} уже сохранён. Пустое поле его не затирает.`;
}

/** Поля заполняются сохранённым (stored), как и модалка настроек: мастер
 * сохраняет ровно то, что в полях, и не должен вписывать в файл значение,
 * пришедшее из окружения или из умолчания, — иначе оно окажется прибитым
 * навсегда и переживёт и контейнер, и смену умолчания. Действующее значение
 * остаётся видимым как подсказка поля: пустое поле показывает то, что будет
 * использовано, и пустым же уходит на сервер — выбор возвращается окружению. */
function fillOnboardingLLM(data, opts = {}) {
  const effective = data?.effective || {};
  const stored = data?.stored || {};
  const defaults = data?.defaults || {};
  const keyStatus = effective.api_key || {};
  if (!opts.keepEdits) {
    const method = initialAuthMethod(data);
    state.onboarding.llmAuth = method === 'apikey' || (!method && keyStatus.has_value) ? 'apikey' : 'codex';
  }

  const base = $('onboarding-llm-base-url');
  if (base) {
    const shown = String(effective.base_url?.value || defaults.base_url || '').trim();
    if (shown) base.placeholder = shown;
    if (!opts.keepEdits) base.value = String(stored.base_url || '');
  }
  const model = $('onboarding-llm-model');
  if (model) {
    const shown = String(effective.model?.value || defaults.model || '').trim();
    if (shown) model.placeholder = shown;
    if (!opts.keepEdits) model.value = String(stored.model || '');
  }
  const key = $('onboarding-llm-api-key');
  if (key) {
    if (!opts.keepEdits) key.value = '';
    key.placeholder = keyStatus.has_value ? String(keyStatus.masked || '••••') : 'sk-…';
  }
  const hint = $('onboarding-llm-hint');
  if (hint) hint.textContent = onboardingKeyHint(keyStatus);

  renderOverridesInto('onboarding-llm-overrides', data?.env_overrides, LLM_OVERRIDE_FLAGS, LLM_OVERRIDE_FIELD_RU);
  renderOnboardingLLMTabs();
}

async function loadOnboardingLLM() {
  try {
    const data = await api('/llm/settings');
    // Снимок один на приложение: applyLLMSnapshot заодно рисует статус входа
    // через ChatGPT — и в модалке, и здесь.
    applyLLMSnapshot(data);
    fillOnboardingLLM(data);
  } catch (e) {
    setOnboardingError(`Настройки LLM: ${e.message || String(e)}`);
  }
}

/** Шаг обязан оставить настройку записанной, а не только показанной, поэтому
 * переход дальше начинается с сохранения. Для вкладки подписки поля
 * OpenAI-провайдера очищаются: подписка их не использует, а сохранённая модель
 * провайдера подменила бы модель подписки. */
async function saveOnboardingLLM() {
  const isCodex = onboardingLLMTab() === 'codex';
  try {
    const data = await api('/llm/settings', {
      method: 'PUT',
      body: {
        auth_method: isCodex ? 'codex' : 'apikey',
        base_url: isCodex ? '' : ($('onboarding-llm-base-url')?.value || '').trim(),
        model: isCodex ? '' : ($('onboarding-llm-model')?.value || '').trim(),
        api_key: isCodex ? '' : ($('onboarding-llm-api-key')?.value || '').trim(),
        clear_api_key: false,
      },
    });
    applyLLMSnapshot(data);
    fillOnboardingLLM(data);
    void loadAgentConfig();
    return true;
  } catch (e) {
    setOnboardingError(e.message || String(e));
    return false;
  }
}

/** Проверка ничего не сохраняет: она показывает, что резолвится из уже
 * сохранённого и окружения, поэтому введённое, но не сохранённое сюда не
 * попадёт — и поля не затираются, чтобы ввод не пропал. */
async function testOnboardingLLM() {
  setOnboardingResult('onboarding-llm-result', 'Проверяем подключение…', '');
  try {
    const data = await api('/llm/settings');
    applyLLMSnapshot(data, { keepEdits: true });
    const agent = data?.agent || {};
    const err = String(agent.error || '').trim();
    if (err) {
      setOnboardingResult('onboarding-llm-result', err, 'err');
      return;
    }
    const where = String(agent.base_url || '').trim() || 'адрес по умолчанию';
    const model = String(agent.model || '').trim() || '—';
    setOnboardingResult(
      'onboarding-llm-result',
      `${transportLabel(agent.auth_method)} · модель ${model} · ${where}`,
      'ok',
    );
  } catch (e) {
    setOnboardingResult('onboarding-llm-result', e.message || String(e), 'err');
  }
}

/* —— Шаг «Движок» —— */

function onboardingEngineValue() {
  const checked = document.querySelector('input[name="onboarding-engine"]:checked');
  return String(checked?.value || 'auto');
}

function setOnboardingEngineValue(value) {
  const radio = $(`onboarding-engine-${value}`);
  if (!radio) return false;
  radio.checked = true;
  return true;
}

async function loadOnboardingEngine() {
  try {
    const data = await api('/engine/settings');
    state.onboarding.engine = data;
    const effective = data?.effective || {};
    const stored = data?.stored || {};
    // Как и на шаге модели: форма правит сохранённое, а действующее показывается
    // подсказкой — чтобы движок и адрес из окружения не переехали в файл.
    setOnboardingEngineValue(String(stored.engine || 'auto'));
    const base = $('onboarding-engine-base-url');
    if (base) {
      const shown = String(effective.api_base_url?.value || '').trim();
      if (shown) base.placeholder = shown;
      base.value = String(stored.api_base_url || '');
    }
    renderOverridesInto(
      'onboarding-engine-overrides',
      data?.env_overrides,
      ENGINE_OVERRIDE_FLAGS,
      ENGINE_OVERRIDE_FIELD_RU,
    );
    const err = String(data?.error || '').trim();
    if (err) setOnboardingError(`Настройки движка: ${err}`);
  } catch (e) {
    setOnboardingError(`Настройки движка: ${e.message || String(e)}`);
  }
}

async function saveOnboardingEngine() {
  try {
    const data = await api('/engine/settings', {
      method: 'PUT',
      body: {
        engine: onboardingEngineValue(),
        api_base_url: ($('onboarding-engine-base-url')?.value || '').trim(),
      },
    });
    state.onboarding.engine = data;
    return true;
  } catch (e) {
    setOnboardingError(e.message || String(e));
    return false;
  }
}

/** Домен для проверки: введённый на шаге входа, иначе выбранный в основном
 * окне, иначе первый известный. Без домена endpoint ответит 400, поэтому мастер
 * говорит об этом сам, а не ходит за отказом. */
function onboardingProbeDomain() {
  const typed = ($('onboarding-auth-domain')?.value || '').trim();
  if (typed) return typed;
  const selected = getSelectedDomain();
  if (selected) return selected;
  const known = state.authDomains.find((d) => String(d.domain || '').trim());
  if (known) return String(known.domain).trim();
  const option = [...($('field-domain')?.options || [])].find((o) => o.value.trim());
  return option ? option.value.trim() : '';
}

async function probeOnboardingEngine() {
  const domain = onboardingProbeDomain();
  if (!domain) {
    setOnboardingResult(
      'onboarding-engine-result',
      'Проверять нечего: укажите домен на шаге «Вход» или выберите его в основном окне.',
      'warn',
    );
    return;
  }
  setOnboardingResult('onboarding-engine-result', `Определяем движок ${domain}…`, '');
  try {
    const data = await api(`/engine/probe?domain=${encodeURIComponent(domain)}`);
    if (!data?.ok) {
      setOnboardingResult(
        'onboarding-engine-result',
        `Не удалось определить движок ${domain}: ${data?.error || 'неизвестная ошибка'}`,
        'err',
      );
      return;
    }
    const detected = String(data.detected || '');
    const configured = String(data.configured || '');
    const detectedRU = ENGINE_MODE_RU[detected] || detected || '—';
    const configuredRU = ENGINE_MODE_RU[configured] || configured || '—';
    let text = `${domain}: определён ${detectedRU}, сейчас настроен ${configuredRU}.`;
    let tone = 'ok';
    if (detected && detected !== onboardingEngineValue() && setOnboardingEngineValue(detected)) {
      text += ` Выбран вариант «${detectedRU}».`;
      tone = 'warn';
    }
    setOnboardingResult('onboarding-engine-result', text, tone);
  } catch (e) {
    setOnboardingResult('onboarding-engine-result', e.message || String(e), 'err');
  }
}

/* —— Шаг «Вход» —— */

function prefillOnboardingAuth() {
  const input = $('onboarding-auth-domain');
  if (input && !input.value.trim()) input.value = getSelectedDomain();
}

async function onOnboardingAuthSubmit(ev) {
  ev.preventDefault();
  const fd = new FormData(ev.target);
  const domain = String(fd.get('domain') || '').trim();
  const login = String(fd.get('login') || '').trim();
  const password = String(fd.get('password') || '');
  if (!domain || !login || !password) return;
  const submit = $('btn-onboarding-auth-submit');
  if (submit) submit.disabled = true;
  setOnboardingError('');
  setOnboardingResult('onboarding-auth-status', 'Входим…', '');
  try {
    await api('/auth/login', { method: 'POST', body: { domain, login, password } });
    const pwd = $('onboarding-auth-password');
    if (pwd) pwd.value = '';
    await loadAuthStatus();
    renderAuth();
    setOnboardingResult('onboarding-auth-status', `Вход выполнен: ${domain}`, 'ok');
  } catch (e) {
    setOnboardingResult('onboarding-auth-status', '', '');
    // Антиспам отдаёт адрес страницы проверки: без ссылки сообщение нечем
    // закрыть, поэтому она идёт рядом с текстом ошибки.
    setOnboardingError(e.message || String(e), e.data?.antispam ? e.data.url : '');
  } finally {
    if (submit) submit.disabled = false;
  }
}

/* —— Переходы, открытие и завершение —— */

async function onboardingNext() {
  if (state.onboarding.busy) return;
  const step = state.onboarding.step;
  setOnboardingError('');
  setOnboardingBusy(true);
  try {
    if (step === 'llm' && !(await saveOnboardingLLM())) return;
    if (step === 'engine' && !(await saveOnboardingEngine())) return;
  } finally {
    setOnboardingBusy(false);
  }
  if (step === 'auth') {
    await finishOnboarding(false);
    return;
  }
  await goToOnboardingStep(ONBOARDING_STEPS[onboardingStepIndex(step) + 1]);
}

async function onboardingBack() {
  if (state.onboarding.busy) return;
  const idx = onboardingStepIndex(state.onboarding.step);
  if (idx === 0) return;
  await goToOnboardingStep(ONBOARDING_STEPS[idx - 1]);
}

function onOnboardingKeydown(e) {
  if (!isOnboardingOpen()) return;
  if (e.key === 'Escape') {
    // Первый запуск закрывается только кнопкой «Пропустить настройку»:
    // случайный Escape не должен молча оставить приложение ненастроенным.
    if (state.onboarding.status?.required) return;
    e.preventDefault();
    e.stopPropagation();
    closeOnboarding();
    return;
  }
  if (e.key === 'Tab') {
    e.stopPropagation();
    trapModalFocus(e, $('onboarding')?.querySelector('.onboarding-dialog'));
  }
}

async function openOnboarding() {
  const overlay = $('onboarding');
  if (!overlay || !overlay.hidden) return;
  onboardingLastFocus = document.activeElement;
  overlay.hidden = false;
  document.body.classList.add('is-modal-open');
  document.addEventListener('keydown', onOnboardingKeydown, true);
  for (const id of ['onboarding-llm-result', 'onboarding-engine-result', 'onboarding-auth-status']) {
    setOnboardingResult(id, '', '');
  }
  await goToOnboardingStep('welcome');
}

function closeOnboarding() {
  const overlay = $('onboarding');
  if (!overlay || overlay.hidden) return;
  overlay.hidden = true;
  // Модалка настроек LLM могла открыться поверх: прокрутку возвращает тот, кто
  // закрывается последним.
  if (!isModalOpen()) document.body.classList.remove('is-modal-open');
  document.removeEventListener('keydown', onOnboardingKeydown, true);
  setOnboardingError('');
  onboardingLastFocus?.focus?.();
  onboardingLastFocus = null;
}

async function finishOnboarding(skipped) {
  setOnboardingBusy(true);
  try {
    state.onboarding.status = await api('/onboarding/complete', {
      method: 'POST',
      body: { skipped: !!skipped },
    });
  } catch (e) {
    // Мастер пройден, а отметку записать не удалось: держать оператора внутри
    // диалога незачем — мастер просто появится снова на следующем запуске.
    toast(`Не удалось сохранить отметку о настройке: ${e.message || String(e)}`, true);
  } finally {
    setOnboardingBusy(false);
  }
  closeOnboarding();
  await bootstrapWorkspace();
}

/** Статус мастера на старте. Ни сеть, ни битый файл состояния не должны запирать
 * приложение: мастер просто не открывается, а консоль остаётся рабочей. */
async function initOnboarding() {
  try {
    const status = await api('/onboarding');
    state.onboarding.status = status;
    if (status?.required) await openOnboarding();
  } catch (e) {
    toast(`Мастер настройки: ${e.message || String(e)}`, true);
  }
}

function bindOnboarding() {
  $('btn-onboarding-open')?.addEventListener('click', () => void openOnboarding());
  $('btn-onboarding-next')?.addEventListener('click', () => void onboardingNext());
  $('btn-onboarding-back')?.addEventListener('click', () => void onboardingBack());
  $('btn-onboarding-skip-all')?.addEventListener('click', () => void finishOnboarding(true));
  $('onboarding-llm-tab-codex')?.addEventListener('click', () => selectOnboardingLLMTab('codex'));
  $('onboarding-llm-tab-apikey')?.addEventListener('click', () => selectOnboardingLLMTab('apikey'));
  $('btn-onboarding-llm-test')?.addEventListener('click', () => void testOnboardingLLM());
  $('btn-onboarding-codex-login')?.addEventListener('click', () => void startCodexLogin(false));
  $('btn-onboarding-codex-code-submit')?.addEventListener('click', () => void submitCodexCode());
  $('onboarding-codex-code')?.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      void submitCodexCode();
    }
  });
  $('btn-onboarding-engine-probe')?.addEventListener('click', () => void probeOnboardingEngine());
  $('onboarding-auth-form')?.addEventListener('submit', (e) => void onOnboardingAuthSubmit(e));
  $('btn-onboarding-auth-skip')?.addEventListener('click', () => void finishOnboarding(false));
}

function bindUI() {
  $('btn-new-chat').addEventListener('click', () => createChat());
  $('btn-patch-chat').addEventListener('click', () => patchActiveChat());
  $('btn-send').addEventListener('click', () => sendMessage());
  $('btn-attach')?.addEventListener('click', () => $('file-input').click());
  $('file-input')?.addEventListener('change', onFilesSelected);
  $('btn-logout').addEventListener('click', () => logout());
  $('btn-export').addEventListener('click', () => exportChat('markdown'));
  $('btn-cancel').addEventListener('click', () => cancelAgent());
  $('btn-theme').addEventListener('click', () => toggleTheme());
  $('btn-sidebar-toggle')?.addEventListener('click', () => toggleSidebar());
  $('sidebar-backdrop')?.addEventListener('click', () => setSidebarOpen(false));
  $('btn-auth-toggle')?.addEventListener('click', () => toggleAuthPanel());
  window.addEventListener('resize', () => {
    const backdrop = $('sidebar-backdrop');
    if (backdrop) backdrop.hidden = !(document.documentElement.dataset.sidebar === 'open' && isMobileViewport());
  });
  $('btn-approval-yes')?.addEventListener('click', () => postApproval('yes'));
  $('btn-approval-no')?.addEventListener('click', () => postApproval('no'));
  $('btn-approval-quit')?.addEventListener('click', () => postApproval('quit'));
  $('login-form').addEventListener('submit', onLoginSubmit);
  bindLLMSettings();
  bindOnboarding();
  $('field-domain')?.addEventListener('change', () => {
    void loadGamesForDomain(getSelectedDomain()).then(() => onChatContextChanged());
  });
  $('field-game-id')?.addEventListener('change', () => {
    void onChatContextChanged();
  });
  $('field-security-mode')?.addEventListener('change', () => {
    void applySecurityMode();
  });
  let searchTimer;
  $('chat-search')?.addEventListener('input', (e) => {
    state.searchQuery = e.target.value;
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => void loadChats(), 200);
  });
  $('message-input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendMessage();
    }
  });
  document.addEventListener('keydown', (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key === 'n') {
      e.preventDefault();
      void createChat();
    }
    if ((e.metaKey || e.ctrlKey) && e.key === 'f') {
      e.preventDefault();
      $('chat-search')?.focus();
    }
    if (e.key === 'Escape' && state.agentRunning) {
      void cancelAgent();
    }
  });
}

/** Приводит приложение в рабочее состояние: модель агента, сессии, домены,
 * чаты и активный чат. Вызывается и на старте, и после закрытия мастера —
 * перезагружать страницу ради этого незачем. */
async function bootstrapWorkspace() {
  void loadAgentConfig();
  try {
    await loadAuthStatus();
  } catch (e) {
    toast(`Статус авторизации: ${e.message || String(e)}`, true);
  }
  try {
    await loadChats();
  } catch (e) {
    toast(`Чаты: ${e.message || String(e)}`, true);
  }
  if (state.chats.length && !state.activeId) {
    await selectChat(String(state.chats[0].id));
  } else if (!state.activeId && getSelectedDomain() && getSelectedGameId()) {
    await onChatContextChanged();
  } else {
    renderAuth();
    refreshSendState();
  }
}

async function boot() {
  const savedTheme = localStorage.getItem('encli-theme');
  if (savedTheme) document.documentElement.dataset.theme = savedTheme;
  initSidebarState();
  bindUI();
  syncSecurityModeVisual();
  window.addEventListener('scroll', () => {
    if (toolTipAnchor) positionToolChipTooltip(toolTipAnchor);
  }, true);
  window.addEventListener('resize', () => {
    if (toolTipAnchor) positionToolChipTooltip(toolTipAnchor);
  });
  requestAnimationFrame(() => document.body.classList.add('is-ready'));
  await bootstrapWorkspace();
  await initOnboarding();
}

boot();
