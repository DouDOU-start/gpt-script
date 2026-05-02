import type {
  AccountsResponse,
  ConfigUpdateRequest,
  LogLine,
  ProxiesResponse,
  ProxyImportResponse,
  ProxyProtocol,
  ProxyStatus,
  ProxyTestResponse,
  RunEvent,
  RunRequest,
  RunResponse,
  WebConfig,
  WebJob,
} from './types';

async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(typeof data.error === 'string' ? data.error : response.statusText);
  }
  return data as T;
}

export function fetchConfig() {
  return api<WebConfig>('/api/config');
}

export function saveConfig(payload: ConfigUpdateRequest) {
  return api<WebConfig>('/api/config', { method: 'PUT', body: JSON.stringify(payload) });
}

export function fetchAccounts(params: URLSearchParams) {
  return api<AccountsResponse>(`/api/accounts?${params.toString()}`);
}

export function startRun(payload: RunRequest) {
  return api<WebJob>('/api/runs', { method: 'POST', body: JSON.stringify(payload) });
}

export function stopRun(runId: string) {
  return api<WebJob>(`/api/runs/${encodeURIComponent(runId)}/stop`, { method: 'POST' });
}

export function fetchProxies(params: URLSearchParams) {
  return api<ProxiesResponse>(`/api/proxies?${params.toString()}`);
}

export function createProxy(payload: { host: string; port: number; protocol: ProxyProtocol; username?: string; password?: string }) {
  return api('/api/proxies', { method: 'POST', body: JSON.stringify(payload) });
}

export function importProxies(payload: { proxies: string[] }) {
  return api<ProxyImportResponse>('/api/proxies/import', { method: 'POST', body: JSON.stringify(payload) });
}

export function deleteProxy(id: number) {
  return api(`/api/proxies/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

export function testProxy(id: number) {
  return api<ProxyTestResponse>(`/api/proxies/${encodeURIComponent(id)}/test`, { method: 'POST' });
}

export function setProxyEnabled(id: number, enabled: boolean) {
  return api<{ enabled: boolean }>(`/api/proxies/${encodeURIComponent(id)}/enabled`, { method: 'PUT', body: JSON.stringify({ enabled }) });
}

export function batchDeleteProxies(ids: number[]) {
  return api<{ deleted_count: number }>('/api/proxies/batch-delete', { method: 'POST', body: JSON.stringify({ ids }) });
}

export function batchToggleProxies(ids: number[], enabled: boolean) {
  return api<{ updated_count: number }>('/api/proxies/batch-toggle', { method: 'POST', body: JSON.stringify({ ids, enabled }) });
}

export function fetchRun(runId: string) {
  return api<RunResponse>(`/api/runs/${encodeURIComponent(runId)}`);
}

export async function fetchRunEvents(runId: string, limit = 80) {
  const data = await api<{ events: RunEvent[] }>(`/api/runs/${encodeURIComponent(runId)}/events?limit=${limit}`);
  return data.events || [];
}

export async function fetchLogs(limit = 180) {
  const data = await api<{ logs: LogLine[] }>(`/api/logs?limit=${limit}`);
  return data.logs || [];
}
