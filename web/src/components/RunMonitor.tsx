import type { RunEvent, WebJob } from '../types';
import { formatTime } from '../utils';
import { StatusPill } from './StatusPill';

export function RunMonitor(props: {
  activeJob?: WebJob;
  events: RunEvent[];
  summary: Record<string, number>;
  summaryFields: readonly (readonly [string, string])[];
}) {
  return (
    <section className="panel monitor-card reveal delay-2">
      <header className="page-hero monitor-hero">
        <div>
          <span className="proxy-state">实时任务</span>
          <h2>监控</h2>
          <p>{props.activeJob?.run_id || '暂无运行任务'}</p>
        </div>
        <StatusPill status={props.activeJob?.status || 'idle'} />
      </header>
      <div className="run-meta">
        <span>{props.activeJob?.run_dir || '-'}</span>
      </div>
      <div className="summary-grid">
        {props.summaryFields.map(([field, label]) => (
          <div className="summary-tile" key={field}>
            <strong>{props.summary[field] ?? 0}</strong>
            <span>{label}</span>
          </div>
        ))}
      </div>
      <div className="event-list">
        {props.events.length === 0 ? (
          <div className="empty-state">暂无事件</div>
        ) : (
          props.events.slice().reverse().map((event, index) => <EventRow event={event} key={`${event.timestamp || ''}-${index}`} />)
        )}
      </div>
    </section>
  );
}

function EventRow({ event }: { event: RunEvent }) {
  return (
    <article className="event-row">
      <span>{formatTime(event.timestamp)}</span>
      <strong>{event.stage || '-'}</strong>
      <StatusPill status={event.status || 'event'} />
      <p>{event.detail || event.email || '-'}</p>
    </article>
  );
}
