import { useEffect, useState } from 'react';
import { fetchRun, fetchRunEvents, startRun as startRunRequest, stopRun as stopRunRequest } from '../api';
import type { RunEvent, RunFormState, RunRequest, RunResponse } from '../types';

export function useRunController(
  form: RunFormState,
  selected: Set<number>,
  loadAccounts: () => Promise<void>,
  showMessage: (value: unknown, type: 'default' | 'error' | 'success') => void,
) {
  const [runId, setRunId] = useState('');
  const [run, setRun] = useState<RunResponse>({});
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [starting, setStarting] = useState(false);

  const activeJob = run.job;
  const isRunning = activeJob?.status === 'running' || activeJob?.status === 'stopping';

  async function pollRun(targetRunId = runId) {
    if (!targetRunId) return;
    try {
      const [nextRun, nextEvents] = await Promise.all([
        fetchRun(targetRunId),
        fetchRunEvents(targetRunId, 80),
      ]);
      setRun(nextRun);
      setEvents(nextEvents);
      if (nextRun.job?.status && !['running', 'stopping'].includes(nextRun.job.status)) {
        await loadAccounts();
      }
    } catch (error) {
      showMessage(error, 'error');
    }
  }

  async function startRun() {
    const payload: RunRequest = {
      mode: form.mode,
      workers: Number(form.workers || 1),
      backend: form.backend,
      headless: form.headless,
      include_secrets: form.includeSecrets,
      proxy_enabled: form.proxyEnabled,
      proxy_mode: form.proxyMode,
      proxy: form.proxy.trim(),
      proxy_pool_file: form.proxyPoolFile.trim(),
      workspace_id: form.workspaceId.trim(),
      organization_id: form.organizationId.trim(),
    };
    if (form.sourceMode === 'selected') {
      payload.account_ids = [...selected];
      if (payload.account_ids.length === 0) {
        showMessage('请先选择账号', 'error');
        return;
      }
    } else {
      payload.status = form.runStatus.trim();
      payload.limit = Number(form.runLimit || 10);
    }
    setStarting(true);
    showMessage('正在启动任务...', 'default');
    try {
      const job = await startRunRequest(payload);
      setRunId(job.run_id);
      setRun({ job });
      showMessage(`已启动 ${job.run_id}`, 'success');
      await pollRun(job.run_id);
    } catch (error) {
      showMessage(error, 'error');
    } finally {
      setStarting(false);
    }
  }

  async function stopRun() {
    if (!runId) return;
    try {
      const job = await stopRunRequest(runId);
      setRun((current) => ({ ...current, job }));
      showMessage('停止信号已发送', 'default');
    } catch (error) {
      showMessage(error, 'error');
    }
  }

  useEffect(() => {
    if (!isRunning || !runId) return;
    const timer = window.setInterval(() => pollRun(runId), 1500);
    return () => window.clearInterval(timer);
  }, [isRunning, runId]);

  return {
    runId,
    run,
    events,
    starting,
    activeJob,
    isRunning,
    startRun,
    stopRun,
    pollRun,
  };
}
