import { useEffect, useState } from 'react';
import { fetchLogs } from '../api';
import type { PageKey } from '../routes';
import type { LogLine } from '../types';

export function useLogs(activePage: PageKey, showError: (error: unknown) => void) {
  const [logs, setLogs] = useState<LogLine[]>([]);

  async function loadLogs() {
    try {
      setLogs(await fetchLogs(180));
    } catch (error) {
      showError(error);
    }
  }

  useEffect(() => {
    if (activePage !== 'logs') return;
    loadLogs();
    const timer = window.setInterval(loadLogs, 1500);
    return () => window.clearInterval(timer);
  }, [activePage]);

  return { logs, loadLogs };
}
