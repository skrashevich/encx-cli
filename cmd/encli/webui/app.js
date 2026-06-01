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
      el.title = err || 'Задайте LLM_MODEL или OPENROUTER_MODEL';
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
    btn.addEventListener('click', () => selectChat(id));

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
      await selectChat(null);
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
  const pillLabel = $('running-pill-label');
  if (bar) bar.hidden = false;
  if (textEl) textEl.textContent = text;
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
    await selectChat(id);
    toast('Новый чат создан.');
  } catch (e) {
    toast(e.message || String(e), true);
  }
}

async function sendMessage() {
  const input = $('message-input');
  const text = input.value.trim();
  if (!text || state.agentRunning) return;
  if (!state.activeId) {
    const id = await ensureActiveChat();
    if (!id) {
      toast('Выберите игру и войдите на домен.', true);
      return;
    }
  }
  clearToolChips();
  try {
    await api(`/chats/${encodeURIComponent(state.activeId)}/messages`, {
      method: 'POST',
      body: { content: text },
    });
    input.value = '';
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

function bindUI() {
  $('btn-new-chat').addEventListener('click', () => createChat());
  $('btn-patch-chat').addEventListener('click', () => patchActiveChat());
  $('btn-send').addEventListener('click', () => sendMessage());
  $('btn-logout').addEventListener('click', () => logout());
  $('btn-export').addEventListener('click', () => exportChat('markdown'));
  $('btn-cancel').addEventListener('click', () => cancelAgent());
  $('btn-theme').addEventListener('click', () => toggleTheme());
  $('btn-approval-yes')?.addEventListener('click', () => postApproval('yes'));
  $('btn-approval-no')?.addEventListener('click', () => postApproval('no'));
  $('btn-approval-quit')?.addEventListener('click', () => postApproval('quit'));
  $('login-form').addEventListener('submit', onLoginSubmit);
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

async function boot() {
  const savedTheme = localStorage.getItem('encli-theme');
  if (savedTheme) document.documentElement.dataset.theme = savedTheme;
  bindUI();
  syncSecurityModeVisual();
  window.addEventListener('scroll', () => {
    if (toolTipAnchor) positionToolChipTooltip(toolTipAnchor);
  }, true);
  window.addEventListener('resize', () => {
    if (toolTipAnchor) positionToolChipTooltip(toolTipAnchor);
  });
  requestAnimationFrame(() => document.body.classList.add('is-ready'));
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

boot();
