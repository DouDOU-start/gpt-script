import type { LogLine } from '../types';
import { formatTime } from '../utils';

export function LogMonitor({ logs }: { logs: LogLine[] }) {
  return (
    <section className="panel log-card reveal delay-1">
      <header className="page-hero log-hero">
        <div>
          <span className="proxy-state">实时输出</span>
          <h2>后端日志</h2>
          <p>Web 进程标准日志。</p>
        </div>
        <div className="proxy-hero-stats" aria-label="日志统计">
          <div><strong>{logs.length}</strong><span>行数</span></div>
        </div>
      </header>
      <div className="log-list">
        {logs.length === 0 ? (
          <div className="empty-state">暂无日志</div>
        ) : (
          logs.slice().reverse().map((line, index) => <LogRow log={line} key={`${line.timestamp || ''}-${index}`} />)
        )}
      </div>
    </section>
  );
}

function LogRow({ log }: { log: LogLine }) {
  return (
    <article className="log-row">
      <span>{formatTime(log.timestamp)}</span>
      <p>{log.line || '-'}</p>
    </article>
  );
}
