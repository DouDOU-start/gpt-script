import type { ReactNode } from 'react';

export function DataBar({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="data-bar reveal page-hero">
      <div>
        <span className="proxy-state">控制台</span>
        <h1>{title}</h1>
      </div>
      <div className="data-actions">{children}</div>
    </section>
  );
}

export function Metric({ label, value }: { label: string; value: number }) {
  return (
    <div className="metric">
      <strong>{value}</strong>
      <span>{label}</span>
    </div>
  );
}
