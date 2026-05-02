export type PageKey = 'accounts' | 'run' | 'logs' | 'proxy' | 'config';

export const pageRoutes: Record<PageKey, string> = {
  accounts: '/accounts',
  run: '/run',
  logs: '/logs',
  proxy: '/proxy',
  config: '/config',
};

const routePages = new Map<string, PageKey>([
  ['/', 'accounts'],
  ['/accounts', 'accounts'],
  ['/run', 'run'],
  ['/logs', 'logs'],
  ['/proxy', 'proxy'],
  ['/config', 'config'],
]);

export function pageFromPath(pathname: string): PageKey {
  const normalized = pathname.replace(/\/+$/, '') || '/';
  return routePages.get(normalized) || 'accounts';
}
