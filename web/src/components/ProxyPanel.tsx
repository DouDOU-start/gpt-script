import { useEffect, useMemo, useState } from 'react';
import {
  batchDeleteProxies,
  batchToggleProxies,
  createProxy,
  deleteProxy,
  fetchProxies,
  importProxies,
  setProxyEnabled,
  testProxy,
} from '../api';
import type { ConfigFormState, ManagedProxy, MessageType, ProxyProtocol, ProxyStatus, ProxyTestResponse, WebConfig } from '../types';

const pageSize = 20;

export function ProxyPanel(props: {
  config: WebConfig | null;
  form: ConfigFormState;
  message: string;
  messageType: MessageType;
  saving: boolean;
  onChange: <K extends keyof ConfigFormState>(key: K, value: ConfigFormState[K]) => void;
  onSave: () => void;
}) {
  const [proxies, setProxies] = useState<ManagedProxy[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [status, setStatus] = useState<ProxyStatus | 'all'>('all');
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [showAdd, setShowAdd] = useState(false);
  const [showImport, setShowImport] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [tests, setTests] = useState<Map<number, ProxyTestResponse>>(new Map());
  const [testing, setTesting] = useState<Set<number>>(new Set());
  const [importText, setImportText] = useState('');
  const [newProxy, setNewProxy] = useState({ protocol: 'http' as ProxyProtocol, host: '', port: '', username: '', password: '' });

  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const proxyLines = props.form.proxiesText.split('\n').map((value) => value.trim()).filter(Boolean);
  const proxyModeLabel = props.form.proxyMode === 'pool' ? '代理池' : '单代理';
  const activeCount = proxies.filter((proxy) => proxy.status === 'active').length;
  const inactiveCount = proxies.filter((proxy) => proxy.status === 'inactive').length;
  const allVisibleSelected = proxies.length > 0 && selected.size === proxies.length;

  async function loadProxies() {
    const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
    if (status !== 'all') params.set('status', status);
    setLoading(true);
    setError('');
    try {
      const data = await fetchProxies(params);
      setProxies(data.items || []);
      setTotal(data.total || 0);
    } catch (err) {
      setError(err instanceof Error ? err.message : '代理列表加载失败');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadProxies();
  }, [page, status]);

  useEffect(() => {
    setSelected(new Set());
  }, [page, status]);

  const selectedIds = useMemo(() => Array.from(selected), [selected]);

  function setNewProxyField<K extends keyof typeof newProxy>(key: K, value: (typeof newProxy)[K]) {
    setNewProxy((current) => ({ ...current, [key]: value }));
  }

  function toggleSelected(id: number) {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleAllVisible() {
    setSelected(allVisibleSelected ? new Set() : new Set(proxies.map((proxy) => proxy.id)));
  }

  async function runAction(label: string, action: () => Promise<void>) {
    setBusy(label);
    setError('');
    try {
      await action();
    } catch (err) {
      setError(err instanceof Error ? err.message : '操作失败');
    } finally {
      setBusy('');
    }
  }

  async function handleCreateProxy() {
    const port = Number(newProxy.port);
    await runAction('add', async () => {
      await createProxy({
        host: newProxy.host.trim(),
        port,
        protocol: newProxy.protocol,
        username: newProxy.username.trim() || undefined,
        password: newProxy.password.trim() || undefined,
      });
      setNewProxy({ protocol: 'http', host: '', port: '', username: '', password: '' });
      setShowAdd(false);
      await loadProxies();
    });
  }

  async function handleImport() {
    const lines = importText.split('\n').map((line) => line.trim()).filter(Boolean);
    await runAction('import', async () => {
      const result = await importProxies({ proxies: lines });
      if (result.failed_count > 0) {
        setError(`导入成功 ${result.success_count} 个，失败 ${result.failed_count} 个`);
      }
      setImportText('');
      setShowImport(false);
      await loadProxies();
    });
  }

  async function handleDelete(id: number) {
    await runAction(`delete-${id}`, async () => {
      await deleteProxy(id);
      setSelected((current) => {
        const next = new Set(current);
        next.delete(id);
        return next;
      });
      await loadProxies();
    });
  }

  async function handleToggle(proxy: ManagedProxy) {
    await runAction(`toggle-${proxy.id}`, async () => {
      await setProxyEnabled(proxy.id, !proxy.enabled);
      await loadProxies();
    });
  }

  async function handleBatchDelete() {
    await runAction('batch-delete', async () => {
      await batchDeleteProxies(selectedIds);
      setSelected(new Set());
      await loadProxies();
    });
  }

  async function handleBatchToggle(enabled: boolean) {
    await runAction(`batch-toggle-${enabled}`, async () => {
      await batchToggleProxies(selectedIds, enabled);
      setSelected(new Set());
      await loadProxies();
    });
  }

  async function handleTest(id: number) {
    setTesting((current) => new Set(current).add(id));
    setTests((current) => {
      const next = new Map(current);
      next.delete(id);
      return next;
    });
    try {
      const result = await testProxy(id);
      setTests((current) => new Map(current).set(id, result));
      await loadProxies();
    } catch (err) {
      setTests((current) => new Map(current).set(id, { proxy_id: id, success: false, error: err instanceof Error ? err.message : '测试失败' }));
    } finally {
      setTesting((current) => {
        const next = new Set(current);
        next.delete(id);
        return next;
      });
    }
  }

  async function handleTestMany(ids: number[]) {
    await Promise.all(ids.map((id) => handleTest(id)));
  }

  return (
    <section className="panel proxy-panel reveal delay-1">
      <header className="proxy-hero">
        <div className="proxy-hero-copy">
          <span className={props.form.proxyEnabled ? 'proxy-state is-on' : 'proxy-state'}>{props.form.proxyEnabled ? proxyModeLabel : '直连'}</span>
          <h2>代理</h2>
          <p>策略、导入、测试、状态。</p>
        </div>
        <div className="proxy-hero-stats" aria-label="代理统计">
          <div><strong>{total}</strong><span>总数</span></div>
          <div><strong>{activeCount}</strong><span>可用</span></div>
          <div><strong>{inactiveCount}</strong><span>异常</span></div>
        </div>
      </header>

      <section className="proxy-policy-card">
        <label className="proxy-switch">
          <input checked={props.form.proxyEnabled} onChange={(event) => props.onChange('proxyEnabled', event.target.checked)} type="checkbox" />
          <span>运行时使用代理</span>
        </label>
        <label>
          <span>模式</span>
          <select value={props.form.proxyMode} onChange={(event) => props.onChange('proxyMode', event.target.value)}>
            <option value="single">单代理</option>
            <option value="pool">代理池</option>
          </select>
        </label>
        <label className="proxy-policy-address">
          <span>{props.form.proxyMode === 'pool' ? '代理池文件' : '代理地址'}</span>
          <input
            value={props.form.proxyMode === 'pool' ? props.form.proxyPoolFile : props.form.proxy}
            onChange={(event) => props.onChange(props.form.proxyMode === 'pool' ? 'proxyPoolFile' : 'proxy', event.target.value)}
            placeholder={props.form.proxyMode === 'pool' ? 'proxies.txt' : 'http://user:pass@host:port'}
          />
        </label>
        <button className="button primary compact" onClick={props.onSave} disabled={props.saving}>{props.saving ? '保存中...' : '保存'}</button>
        {props.form.proxyMode === 'pool' && (
          <details className="proxy-inline-pool">
            <summary>内联代理池 · {proxyLines.length} 条</summary>
            <textarea value={props.form.proxiesText} onChange={(event) => props.onChange('proxiesText', event.target.value)} placeholder="每行一个代理，可留空" />
          </details>
        )}
      </section>

      <section className="proxy-toolbar">
        <div className="proxy-filters" aria-label="代理状态筛选">
          {(['all', 'active', 'inactive'] as const).map((value) => (
            <button key={value} className={status === value ? 'chip active' : 'chip'} onClick={() => { setStatus(value); setPage(1); }}>
              {value === 'all' ? '全部' : value === 'active' ? '可用' : '异常'}
            </button>
          ))}
        </div>
        <div className="proxy-actions">
          <button className="button compact" onClick={loadProxies} disabled={loading}>{loading ? '刷新中...' : '刷新'}</button>
          <button className="button compact" onClick={() => handleTestMany(proxies.map((proxy) => proxy.id))} disabled={proxies.length === 0}>测试全部</button>
          <button className="button compact" onClick={() => setShowImport((value) => !value)}>{showImport ? '收起导入' : '导入'}</button>
          <button className="button primary compact" onClick={() => setShowAdd((value) => !value)}>{showAdd ? '收起新增' : '新增'}</button>
        </div>
      </section>

      {error && <p className="message error proxy-inline-message">{error}</p>}
      {props.message && <p className={`message ${props.messageType} proxy-inline-message`}>{props.message}</p>}

      {showAdd && (
        <section className="proxy-form-card">
          <strong>新增代理</strong>
          <select value={newProxy.protocol} onChange={(event) => setNewProxyField('protocol', event.target.value as ProxyProtocol)}>
            <option value="http">HTTP</option>
            <option value="https">HTTPS</option>
            <option value="socks5">SOCKS5</option>
          </select>
          <input value={newProxy.host} onChange={(event) => setNewProxyField('host', event.target.value)} placeholder="主机地址" />
          <input value={newProxy.port} onChange={(event) => setNewProxyField('port', event.target.value)} placeholder="端口" type="number" />
          <input value={newProxy.username} onChange={(event) => setNewProxyField('username', event.target.value)} placeholder="用户名，可选" />
          <input value={newProxy.password} onChange={(event) => setNewProxyField('password', event.target.value)} placeholder="密码，可选" type="password" />
          <button className="button primary" onClick={handleCreateProxy} disabled={busy === 'add'}>{busy === 'add' ? '添加中...' : '确认'}</button>
        </section>
      )}

      {showImport && (
        <section className="proxy-import-card">
          <strong>批量导入</strong>
          <p>每行一个代理，支持 host:port、host:port:user:pass、protocol://host:port、protocol://user:pass@host:port。</p>
          <textarea value={importText} onChange={(event) => setImportText(event.target.value)} placeholder={'127.0.0.1:8080\nhttp://user:pass@example.com:3128\nsocks5://10.0.0.1:1080'} />
          <button className="button primary compact" onClick={handleImport} disabled={busy === 'import'}>{busy === 'import' ? '导入中...' : '导入代理'}</button>
        </section>
      )}

      {proxies.length > 0 && (
        <section className={selected.size > 0 ? 'proxy-bulk-bar is-active' : 'proxy-bulk-bar'}>
          <label>
            <input type="checkbox" checked={allVisibleSelected} onChange={toggleAllVisible} />
            全选当前页
          </label>
          <span>已选 {selected.size} 项</span>
          <button className="button compact" onClick={() => handleTestMany(selectedIds)} disabled={selected.size === 0}>测试</button>
          <button className="button compact" onClick={() => handleBatchToggle(true)} disabled={selected.size === 0 || busy === 'batch-toggle-true'}>启用</button>
          <button className="button compact" onClick={() => handleBatchToggle(false)} disabled={selected.size === 0 || busy === 'batch-toggle-false'}>禁用</button>
          <button className="button danger compact" onClick={handleBatchDelete} disabled={selected.size === 0 || busy === 'batch-delete'}>删除</button>
        </section>
      )}

      <section className="proxy-list">
        {loading && proxies.length === 0 ? (
          <div className="config-empty-state">正在加载代理...</div>
        ) : proxies.length === 0 ? (
          <div className="proxy-empty-state">
            <strong>还没有代理</strong>
            <span>点“新增”录入一个，或用“导入”一次贴入多行。</span>
          </div>
        ) : (
          proxies.map((proxy) => (
            <ProxyRow
              key={proxy.id}
              proxy={proxy}
              selected={selected.has(proxy.id)}
              test={tests.get(proxy.id)}
              testing={testing.has(proxy.id)}
              busy={busy}
              onSelect={() => toggleSelected(proxy.id)}
              onToggle={() => handleToggle(proxy)}
              onTest={() => handleTest(proxy.id)}
              onDelete={() => handleDelete(proxy.id)}
            />
          ))
        )}
      </section>

      <footer className="proxy-pagination">
        <button className="button compact" onClick={() => setPage((value) => Math.max(1, value - 1))} disabled={page <= 1 || loading}>上一页</button>
        <span>第 {page} / {totalPages} 页</span>
        <button className="button compact" onClick={() => setPage((value) => Math.min(totalPages, value + 1))} disabled={page >= totalPages || loading}>下一页</button>
      </footer>
    </section>
  );
}

function ProxyRow(props: {
  proxy: ManagedProxy;
  selected: boolean;
  test?: ProxyTestResponse;
  testing: boolean;
  busy: string;
  onSelect: () => void;
  onToggle: () => void;
  onTest: () => void;
  onDelete: () => void;
}) {
  const address = `${props.proxy.host}:${props.proxy.port}`;
  const checkedAt = props.proxy.last_checked_at ? new Date(props.proxy.last_checked_at).toLocaleString('zh-CN') : '未检测';
  return (
    <article className={props.proxy.enabled ? 'proxy-row' : 'proxy-row disabled'}>
      <label className="proxy-row-check" aria-label={`选择代理 ${props.proxy.id}`}>
        <input type="checkbox" checked={props.selected} onChange={props.onSelect} />
      </label>
      <div className="proxy-address">
        <div>
          <span className="proxy-protocol">{props.proxy.protocol.toUpperCase()}</span>
          <strong>{address}</strong>
        </div>
        <small>#{props.proxy.id} · 最后检测：{checkedAt}</small>
      </div>
      <div className="proxy-health">
        <span className={props.proxy.status === 'active' ? 'proxy-status active' : 'proxy-status inactive'}>{props.proxy.status === 'active' ? '可用' : '异常'}</span>
        {props.proxy.fail_count > 0 && <span className="proxy-fails">失败 {props.proxy.fail_count}</span>}
        <span className="proxy-test-result">
          {props.testing ? '测试中...' : props.test ? props.test.success ? `${props.test.ip || 'OK'} · ${props.test.response_time_ms || 0}ms` : props.test.error || '失败' : '未测试'}
        </span>
      </div>
      <div className="proxy-row-actions">
        <button className={props.proxy.enabled ? 'proxy-toggle on' : 'proxy-toggle'} onClick={props.onToggle} disabled={props.busy === `toggle-${props.proxy.id}`}>
          {props.proxy.enabled ? '启用' : '禁用'}
        </button>
        <button className="button compact" onClick={props.onTest} disabled={props.testing}>测试</button>
        <button className="button danger compact" onClick={props.onDelete} disabled={props.busy === `delete-${props.proxy.id}`}>删除</button>
      </div>
    </article>
  );
}
