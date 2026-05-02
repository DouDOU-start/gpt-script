import { useEffect, useState } from 'react';
import { fetchConfig, saveConfig } from '../api';
import type { ConfigFormState, ConfigUpdateRequest, RunFormState, RuntimeSettings, WebConfig } from '../types';

const defaultRuntimeSettings: RuntimeSettings = {
  backend: 'docker',
  headless: true,
  includeSecrets: true,
  proxyEnabled: false,
  proxyMode: 'single',
  proxy: '',
  proxyPoolFile: '',
};

export const defaultRunForm: RunFormState = {
  ...defaultRuntimeSettings,
  mode: 'oauth',
  sourceMode: 'selected',
  runStatus: 'active',
  runLimit: 10,
  workers: 1,
  workspaceId: '',
  organizationId: '',
};

export const defaultConfigForm: ConfigFormState = {
  ...defaultRuntimeSettings,
  proxiesText: '',
};

function configToForm(data: WebConfig): ConfigFormState {
  const proxyMode = data.proxy?.mode === 'pool' ? 'pool' : 'single';
  return {
    backend: data.browser_backend || 'docker',
    headless: Boolean(data.browser_headless),
    includeSecrets: Boolean(data.include_secrets),
    proxyEnabled: Boolean(data.proxy?.enabled),
    proxyMode,
    proxy: data.proxy?.proxy || '',
    proxyPoolFile: data.proxy?.pool_file || '',
    proxiesText: (data.proxy?.proxies || []).join('\n'),
  };
}

export function applyRuntimeSettings(form: RunFormState, settings: RuntimeSettings): RunFormState {
  return {
    ...form,
    backend: settings.backend,
    headless: settings.headless,
    includeSecrets: settings.includeSecrets,
    proxyEnabled: settings.proxyEnabled,
    proxyMode: settings.proxyMode,
    proxy: settings.proxy,
    proxyPoolFile: settings.proxyPoolFile,
  };
}

export function useWebConfig(showMessage: (value: unknown, type: 'default' | 'error' | 'success') => void) {
  const [webConfig, setWebConfig] = useState<WebConfig | null>(null);
  const [configForm, setConfigForm] = useState<ConfigFormState>(defaultConfigForm);
  const [savingConfig, setSavingConfig] = useState(false);

  function updateConfigForm<K extends keyof ConfigFormState>(key: K, value: ConfigFormState[K]) {
    setConfigForm((current) => ({ ...current, [key]: value }));
  }

  function applyConfig(data: WebConfig) {
    setWebConfig(data);
    const nextConfigForm = configToForm(data);
    setConfigForm(nextConfigForm);
    return nextConfigForm;
  }

  async function loadConfig() {
    try {
      return applyConfig(await fetchConfig());
    } catch (error) {
      showMessage(error, 'error');
      return null;
    }
  }

  async function persistConfig() {
    const payload: ConfigUpdateRequest = {
      browser_backend: configForm.backend,
      browser_headless: configForm.headless,
      include_secrets: configForm.includeSecrets,
      proxy_enabled: configForm.proxyEnabled,
      proxy_mode: configForm.proxyMode,
      proxy: configForm.proxy.trim(),
      proxy_pool_file: configForm.proxyPoolFile.trim(),
      proxies: configForm.proxiesText.split('\n').map((value) => value.trim()).filter(Boolean),
    };
    setSavingConfig(true);
    showMessage('正在保存配置...', 'default');
    try {
      applyConfig(await saveConfig(payload));
      showMessage('配置已保存', 'success');
    } catch (error) {
      showMessage(error, 'error');
    } finally {
      setSavingConfig(false);
    }
  }

  useEffect(() => {
    loadConfig();
  }, []);

  return {
    webConfig,
    configForm,
    savingConfig,
    setConfigForm,
    updateConfigForm,
    loadConfig,
    persistConfig,
  };
}
