import type { MouseEvent } from 'react';
import { type PageKey, pageRoutes } from '../routes';

export function Sidebar({ activePage, onNavigate }: { activePage: PageKey; onNavigate: (page: PageKey) => void }) {
  function navigate(event: MouseEvent<HTMLAnchorElement>, page: PageKey) {
    event.preventDefault();
    onNavigate(page);
  }

  return (
    <aside className="sidebar">
      <div className="brand-mark">
        <span className="brand-icon">◎</span>
        <strong>Register</strong>
      </div>
      <nav className="side-nav" aria-label="主导航">
        <a className={activePage === 'accounts' ? 'active' : ''} href={pageRoutes.accounts} onClick={(event) => navigate(event, 'accounts')}>
          <span>▣</span>
          账号
        </a>
        <a className={activePage === 'run' ? 'active' : ''} href={pageRoutes.run} onClick={(event) => navigate(event, 'run')}>
          <span>▶</span>
          运行
        </a>
        <a className={activePage === 'logs' ? 'active' : ''} href={pageRoutes.logs} onClick={(event) => navigate(event, 'logs')}>
          <span>≡</span>
          日志
        </a>
        <a className={activePage === 'proxy' ? 'active' : ''} href={pageRoutes.proxy} onClick={(event) => navigate(event, 'proxy')}>
          <span>⟐</span>
          代理管理
        </a>
        <a className={activePage === 'config' ? 'active' : ''} href={pageRoutes.config} onClick={(event) => navigate(event, 'config')}>
          <span>◇</span>
          配置
        </a>
      </nav>
    </aside>
  );
}
