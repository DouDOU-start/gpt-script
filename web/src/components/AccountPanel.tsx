import type { Account } from '../types';
import { formatTime } from '../utils';
import { StatusPill } from './StatusPill';

export function AccountPanel(props: {
  accounts: Account[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
  selected: Set<number>;
  selectedVisibleCount: number;
  search: string;
  statusFilter: string;
  loading: boolean;
  onSearch: (value: string) => void;
  onStatus: (value: string) => void;
  onPage: (value: number) => void;
  onPageSize: (value: number) => void;
  onToggleAll: (checked: boolean) => void;
  onToggleAccount: (id: number, checked: boolean) => void;
}) {
  const allVisibleSelected = props.accounts.length > 0 && props.selectedVisibleCount === props.accounts.length;
  const safePage = Math.min(props.page, props.totalPages);
  const pageStart = props.total === 0 ? 0 : (safePage - 1) * props.pageSize + 1;
  const pageEnd = Math.min(safePage * props.pageSize, props.total);
  const activeCount = props.accounts.filter((account) => account.status === 'active').length;
  const bannedCount = props.accounts.filter((account) => account.status === 'banned').length;

  return (
    <section className="panel account-panel reveal delay-1">
      <header className="page-hero account-hero">
        <div>
          <span className="proxy-state">账号库</span>
          <h2>账号</h2>
          <p>筛选、选择、查看状态。</p>
        </div>
        <div className="proxy-hero-stats" aria-label="账号统计">
          <div><strong>{props.total}</strong><span>总数</span></div>
          <div><strong>{activeCount}</strong><span>可用</span></div>
          <div><strong>{bannedCount}</strong><span>禁用</span></div>
        </div>
      </header>

      <section className="toolbar-card account-toolbar">
        <label>
          <span>邮箱</span>
          <input value={props.search} onChange={(event) => props.onSearch(event.target.value)} placeholder="name@example.com" type="search" />
        </label>
        <label>
          <span>状态</span>
          <select value={props.statusFilter} onChange={(event) => props.onStatus(event.target.value)}>
            <option value="">全部</option>
            <option value="active">active</option>
            <option value="banned">banned</option>
          </select>
        </label>
        <label>
          <span>每页</span>
          <select value={props.pageSize} onChange={(event) => props.onPageSize(Number(event.target.value))}>
            <option value={25}>25</option>
            <option value={50}>50</option>
            <option value={100}>100</option>
            <option value={200}>200</option>
            <option value={500}>500</option>
          </select>
        </label>
      </section>

      <div className="table-shell">
        <table>
          <thead>
            <tr>
              <th>
                <input checked={allVisibleSelected} onChange={(event) => props.onToggleAll(event.target.checked)} type="checkbox" />
              </th>
              <th>ID</th>
              <th>邮箱</th>
              <th>状态</th>
              <th>操作</th>
              <th>错误</th>
              <th>时间</th>
            </tr>
          </thead>
          <tbody>
            {props.accounts.length === 0 ? (
              <tr>
                <td className="empty-row" colSpan={7}>暂无账号</td>
              </tr>
            ) : (
              props.accounts.map((account) => (
                <tr key={account.id} className={props.selected.has(account.id) ? 'selected-row' : ''}>
                  <td>
                    <input checked={props.selected.has(account.id)} onChange={(event) => props.onToggleAccount(account.id, event.target.checked)} type="checkbox" />
                  </td>
                  <td>{account.id}</td>
                  <td className="email-cell">{account.email}</td>
                  <td><StatusPill status={account.status || 'unknown'} /></td>
                  <td>{account.last_mode || '-'}</td>
                  <td className="error-cell" title={account.last_error || ''}>{account.last_error || '-'}</td>
                  <td>{formatTime(account.updated_at)}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      <div className="pagination-bar">
        <span>{pageStart}-{pageEnd} / {props.total}</span>
        <div className="pagination-controls">
          <button className="button compact" disabled={props.loading || safePage <= 1} onClick={() => props.onPage(safePage - 1)}>上一页</button>
          <strong>{safePage} / {props.totalPages}</strong>
          <button className="button compact" disabled={props.loading || safePage >= props.totalPages} onClick={() => props.onPage(safePage + 1)}>下一页</button>
        </div>
      </div>
    </section>
  );
}
