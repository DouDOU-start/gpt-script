import { useEffect, useState } from 'react';
import { AccountPanel } from './components/AccountPanel';
import { ConfigPanel } from './components/ConfigPanel';
import { ControlPanel } from './components/ControlPanel';
import { DataBar, Metric } from './components/DataBar';
import { LogMonitor } from './components/LogMonitor';
import { ProxyPanel } from './components/ProxyPanel';
import { RunMonitor } from './components/RunMonitor';
import { Sidebar } from './components/Sidebar';
import { StatusPill } from './components/StatusPill';
import { useAccounts } from './hooks/useAccounts';
import { useLogs } from './hooks/useLogs';
import { useMessage } from './hooks/useMessage';
import { useRunController } from './hooks/useRunController';
import { applyRuntimeSettings, defaultRunForm, useWebConfig } from './hooks/useWebConfig';
import { type PageKey, pageFromPath, pageRoutes } from './routes';
import type { RunFormState } from './types';

const summaryFields = [
  ['requested', '请求'],
  ['planned', '计划'],
  ['registered', '注册'],
  ['authorized', '授权'],
  ['failed', '失败'],
] as const;

export function App() {
  const [activePage, setActivePage] = useState<PageKey>(() => pageFromPath(window.location.pathname));
  const [form, setForm] = useState<RunFormState>(defaultRunForm);
  const { message, messageType, showMessage } = useMessage();
  const accountsState = useAccounts((error) => showMessage(error, 'error'));
  const configState = useWebConfig(showMessage);
  const runState = useRunController(form, accountsState.selected, accountsState.loadAccounts, showMessage);
  const { logs } = useLogs(activePage, (error) => showMessage(error, 'error'));

  const summary = runState.run.summary || {};

  function updateForm<K extends keyof RunFormState>(key: K, value: RunFormState[K]) {
    setForm((current) => ({ ...current, [key]: value }));
  }

  function navigate(page: PageKey) {
    const nextPath = pageRoutes[page];
    if (window.location.pathname !== nextPath) {
      window.history.pushState({}, '', nextPath);
    }
    setActivePage(page);
  }

  useEffect(() => {
    function handlePopState() {
      setActivePage(pageFromPath(window.location.pathname));
    }
    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, []);

  useEffect(() => {
    if (activePage !== 'config' && activePage !== 'proxy') return;
    configState.loadConfig();
  }, [activePage]);

  useEffect(() => {
    if (!configState.webConfig) return;
    setForm((current) => applyRuntimeSettings(current, configState.configForm));
  }, [configState.webConfig]);

  return (
    <main className="app-shell">
      <Sidebar activePage={activePage} onNavigate={navigate} />

      <section className={activePage === 'accounts' ? 'page-shell accounts-page' : 'page-shell'}>
        {activePage === 'accounts' && (
          <AccountPanel
            accounts={accountsState.accounts}
            total={accountsState.accountTotal}
            page={accountsState.page}
            pageSize={accountsState.pageSize}
            totalPages={accountsState.totalPages}
            selected={accountsState.selected}
            selectedVisibleCount={accountsState.selectedVisibleCount}
            search={accountsState.search}
            statusFilter={accountsState.statusFilter}
            loading={accountsState.loadingAccounts}
            onSearch={accountsState.setSearch}
            onStatus={accountsState.setStatusFilter}
            onPage={accountsState.setPage}
            onPageSize={accountsState.setPageSize}
            onToggleAll={accountsState.toggleAllVisible}
            onToggleAccount={accountsState.toggleAccount}
          />
        )}
        {activePage === 'run' && (
          <>
            <DataBar title="运行">
              <Metric label="选择" value={accountsState.selected.size} />
              <Metric label="事件" value={runState.events.length} />
              <Metric label="失败" value={summary.failed ?? 0} />
              <StatusPill status={runState.activeJob?.status || 'idle'} />
            </DataBar>
            <section className="run-layout">
              <ControlPanel
                form={form}
                selectedCount={accountsState.selected.size}
                activeJob={runState.activeJob}
                message={message}
                messageType={messageType}
                starting={runState.starting}
                canStop={Boolean(runState.runId && runState.isRunning)}
                onChange={updateForm}
                onStart={runState.startRun}
                onStop={runState.stopRun}
              />
              <RunMonitor activeJob={runState.activeJob} events={runState.events} summary={summary} summaryFields={summaryFields} />
            </section>
          </>
        )}
        {activePage === 'config' && (
          <ConfigPanel
            config={configState.webConfig}
            form={configState.configForm}
            message={message}
            messageType={messageType}
            saving={configState.savingConfig}
            onChange={configState.updateConfigForm}
            onSave={configState.persistConfig}
          />
        )}
        {activePage === 'logs' && (
          <>
            <DataBar title="日志监控">
              <Metric label="日志" value={logs.length} />
              <Metric label="事件" value={runState.events.length} />
              <StatusPill status={runState.activeJob?.status || 'idle'} />
            </DataBar>
            <LogMonitor logs={logs} />
          </>
        )}
        {activePage === 'proxy' && (
          <ProxyPanel
            config={configState.webConfig}
            form={configState.configForm}
            message={message}
            messageType={messageType}
            saving={configState.savingConfig}
            onChange={configState.updateConfigForm}
            onSave={configState.persistConfig}
          />
        )}
      </section>
    </main>
  );
}
