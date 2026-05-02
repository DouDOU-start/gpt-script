export type Account = {
  id: number;
  email: string;
  status: string;
  last_mode: string;
  last_error?: string;
  last_run_id?: string;
  updated_at?: string;
  created_at?: string;
};

export type WebJob = {
  run_id: string;
  run_dir: string;
  mode: string;
  status: string;
  accounts: number;
  started_at: string;
  completed_at?: string;
  error?: string;
};

export type RunEvent = {
  timestamp?: string;
  stage?: string;
  status?: string;
  detail?: string;
  email?: string;
};

export type LogLine = {
  timestamp?: string;
  line: string;
};

export type ProxyConfig = {
  enabled: boolean;
  mode: string;
  proxy: string;
  pool_file: string;
  proxies?: string[];
};

export type ProxyStatus = 'active' | 'inactive';

export type ProxyProtocol = 'http' | 'https' | 'socks5';

export type ManagedProxy = {
  id: number;
  host: string;
  port: number;
  protocol: ProxyProtocol;
  enabled: boolean;
  status: ProxyStatus;
  fail_count: number;
  last_used_at: string | null;
  last_checked_at: string | null;
  created_at: string;
};

export type ProxiesResponse = {
  items: ManagedProxy[];
  page: number;
  page_size: number;
  total: number;
};

export type ProxyTestResponse = {
  proxy_id: number;
  success: boolean;
  response_time_ms?: number;
  ip?: string;
  error?: string;
};

export type ProxyImportResponse = {
  success_count: number;
  failed_count: number;
  errors?: string[];
};

export type WebConfig = {
  config_path: string;
  browser_backend: string;
  browser_headless: boolean;
  include_secrets: boolean;
  runs_dir?: string;
  proxy: ProxyConfig;
};

export type RunResponse = {
  job?: WebJob;
  summary?: Record<string, number>;
};

export type RunRequest = {
  mode: string;
  workers: number;
  backend: string;
  headless: boolean;
  include_secrets: boolean;
  proxy_enabled: boolean;
  proxy_mode: string;
  proxy: string;
  proxy_pool_file: string;
  workspace_id: string;
  organization_id: string;
  account_ids?: number[];
  status?: string;
  limit?: number;
};

export type ConfigUpdateRequest = {
  browser_backend: string;
  browser_headless: boolean;
  include_secrets: boolean;
  proxy_enabled: boolean;
  proxy_mode: string;
  proxy: string;
  proxy_pool_file: string;
  proxies: string[];
};

export type AccountsResponse = {
  accounts: Account[];
  page: number;
  page_size: number;
  total: number;
};

export type RuntimeSettings = {
  backend: string;
  headless: boolean;
  includeSecrets: boolean;
  proxyEnabled: boolean;
  proxyMode: string;
  proxy: string;
  proxyPoolFile: string;
};

export type RunFormState = RuntimeSettings & {
  mode: string;
  sourceMode: string;
  runStatus: string;
  runLimit: number;
  workers: number;
  workspaceId: string;
  organizationId: string;
};

export type ConfigFormState = RuntimeSettings & {
  proxiesText: string;
};

export type MessageType = 'default' | 'error' | 'success';
