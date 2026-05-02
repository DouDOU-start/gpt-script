const state = {
  accounts: [],
  selected: new Set(),
  currentRunId: '',
  pollTimer: 0,
};

const $ = (id) => document.getElementById(id);

async function api(path, options = {}) {
  const response = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || response.statusText);
  return data;
}

function statusClass(status) {
  return `pill ${String(status || 'unknown').toLowerCase()}`;
}

function visibleAccounts() {
  const keyword = $('searchEmail').value.trim().toLowerCase();
  if (!keyword) return state.accounts;
  return state.accounts.filter((account) => account.email.toLowerCase().includes(keyword));
}

function renderAccounts() {
  const rows = visibleAccounts();
  $('accountCount').textContent = `${rows.length} / ${state.accounts.length} 个账号，已选 ${state.selected.size} 个`;
  $('accountsBody').innerHTML = rows.map((account) => {
    const checked = state.selected.has(account.id) ? 'checked' : '';
    const error = account.last_error ? escapeHtml(account.last_error) : '';
    return `<tr data-id="${account.id}">
      <td><input class="row-check" type="checkbox" ${checked}></td>
      <td>${account.id}</td>
      <td class="email">${escapeHtml(account.email)}</td>
      <td><span class="${statusClass(account.status)}">${escapeHtml(account.status || 'unknown')}</span></td>
      <td>${escapeHtml(account.last_mode || '')}</td>
      <td class="error" title="${error}">${error}</td>
      <td>${formatTime(account.updated_at)}</td>
    </tr>`;
  }).join('');
  document.querySelectorAll('.row-check').forEach((checkbox) => {
    checkbox.addEventListener('change', (event) => {
      const id = Number(event.target.closest('tr').dataset.id);
      if (event.target.checked) state.selected.add(id);
      else state.selected.delete(id);
      renderAccounts();
    });
  });
}

async function loadAccounts() {
  const params = new URLSearchParams();
  const status = $('statusFilter').value.trim();
  const limit = $('limitFilter').value.trim();
  if (status) params.set('status', status);
  if (limit) params.set('limit', limit);
  $('accountCount').textContent = '加载中...';
  const data = await api(`/api/accounts?${params.toString()}`);
  state.accounts = data.accounts || [];
  const ids = new Set(state.accounts.map((account) => account.id));
  state.selected = new Set([...state.selected].filter((id) => ids.has(id)));
  renderAccounts();
}

async function startRun() {
  const sourceMode = $('sourceMode').value;
  const payload = {
    mode: $('mode').value,
    workers: Number($('workers').value || 1),
    backend: $('backend').value,
    headless: $('headless').checked,
    include_secrets: $('includeSecrets').checked,
    proxy: $('proxy').value.trim(),
    workspace_id: $('workspaceId').value.trim(),
    organization_id: $('organizationId').value.trim(),
  };
  if (sourceMode === 'selected') {
    payload.account_ids = [...state.selected];
    if (payload.account_ids.length === 0) {
      setMessage('请先选择账号', true);
      return;
    }
  } else {
    payload.status = $('runStatus').value.trim();
    payload.limit = Number($('runLimit').value || 10);
  }
  $('startRun').disabled = true;
  setMessage('正在启动...', false);
  try {
    const job = await api('/api/runs', { method: 'POST', body: JSON.stringify(payload) });
    state.currentRunId = job.run_id;
    setMessage(`已启动 ${job.run_id}`, false);
    $('stopRun').disabled = false;
    pollRun();
  } catch (error) {
    setMessage(error.message, true);
  } finally {
    $('startRun').disabled = false;
  }
}

async function pollRun() {
  if (!state.currentRunId) return;
  clearTimeout(state.pollTimer);
  try {
    const [run, events] = await Promise.all([
      api(`/api/runs/${encodeURIComponent(state.currentRunId)}`),
      api(`/api/runs/${encodeURIComponent(state.currentRunId)}/events?limit=80`),
    ]);
    renderRun(run);
    renderEvents(events.events || []);
    const status = run.job?.status || '';
    if (status === 'running' || status === 'stopping') {
      state.pollTimer = setTimeout(pollRun, 1500);
    } else {
      $('stopRun').disabled = true;
      await loadAccounts();
    }
  } catch (error) {
    $('runMeta').textContent = error.message;
  }
}

async function stopRun() {
  if (!state.currentRunId) return;
  await api(`/api/runs/${encodeURIComponent(state.currentRunId)}/stop`, { method: 'POST' });
  pollRun();
}

function renderRun(run) {
  const job = run.job || {};
  const summary = run.summary || {};
  $('runMeta').textContent = job.run_id ? `${job.run_id} · ${job.status} · ${job.run_dir}` : '历史运行';
  const fields = ['requested', 'planned', 'registered', 'authorized', 'failed'];
  $('summary').innerHTML = fields.map((field) => `<div><strong>${summary[field] ?? 0}</strong><span>${field}</span></div>`).join('');
}

function renderEvents(events) {
  const template = $('eventTemplate');
  $('events').innerHTML = '';
  events.slice().reverse().forEach((event) => {
    const node = template.content.cloneNode(true);
    node.querySelector('.event-time').textContent = formatTime(event.timestamp || '');
    node.querySelector('.event-stage').textContent = event.stage || '';
    node.querySelector('.event-status').textContent = event.status || '';
    node.querySelector('.event-detail').textContent = event.detail || event.email || '';
    $('events').appendChild(node);
  });
}

function setMessage(message, isError) {
  $('launcherMessage').textContent = message;
  $('launcherMessage').className = isError ? 'message error-text' : 'message';
}

function formatTime(value) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function escapeHtml(value) {
  return String(value ?? '').replace(/[&<>'"]/g, (char) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    "'": '&#39;',
    '"': '&quot;',
  }[char]));
}

$('refreshAccounts').addEventListener('click', loadAccounts);
$('statusFilter').addEventListener('change', loadAccounts);
$('limitFilter').addEventListener('change', loadAccounts);
$('searchEmail').addEventListener('input', renderAccounts);
$('startRun').addEventListener('click', startRun);
$('stopRun').addEventListener('click', stopRun);
$('selectAll').addEventListener('change', (event) => {
  visibleAccounts().forEach((account) => {
    if (event.target.checked) state.selected.add(account.id);
    else state.selected.delete(account.id);
  });
  renderAccounts();
});

loadAccounts().catch((error) => setMessage(error.message, true));
