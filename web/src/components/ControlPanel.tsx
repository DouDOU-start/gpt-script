import type { MessageType, RunFormState, WebJob } from '../types';
import { StatusPill } from './StatusPill';

export function ControlPanel(props: {
  form: RunFormState;
  selectedCount: number;
  activeJob?: WebJob;
  message: string;
  messageType: MessageType;
  starting: boolean;
  canStop: boolean;
  onChange: <K extends keyof RunFormState>(key: K, value: RunFormState[K]) => void;
  onStart: () => void;
  onStop: () => void;
}) {
  const proxyLabel = props.form.proxyEnabled ? (props.form.proxyMode === 'pool' ? '代理池' : '单代理') : '直连';

  return (
    <aside className="panel control-panel reveal delay-1">
      <header className="page-hero run-hero">
        <div>
          <span className="proxy-state">任务启动</span>
          <h2>启动任务</h2>
          <p>确认账号来源、运行环境和代理。</p>
        </div>
        <StatusPill status={props.activeJob?.status || 'idle'} />
      </header>

      <div className="run-summary-strip">
        <span><strong>{props.selectedCount}</strong> 已选</span>
        <span>{props.form.mode}</span>
        <span>{props.form.backend}</span>
        <span>{proxyLabel}</span>
      </div>

      <section className="config-group">
        <div className="group-title">
          <h3>账号来源</h3>
          <span>必填</span>
        </div>
        <div className="form-grid">
          <label>
            <span>模式</span>
            <select value={props.form.mode} onChange={(event) => props.onChange('mode', event.target.value)}>
              <option value="oauth">OAuth</option>
              <option value="login">登录</option>
              <option value="register">注册</option>
            </select>
          </label>
          <label>
            <span>来源</span>
            <select value={props.form.sourceMode} onChange={(event) => props.onChange('sourceMode', event.target.value)}>
              <option value="selected">已选账号</option>
              <option value="status">按状态批量</option>
            </select>
          </label>
          {props.form.sourceMode === 'status' && (
            <>
              <label>
                <span>状态</span>
                <select value={props.form.runStatus} onChange={(event) => props.onChange('runStatus', event.target.value)}>
                  <option value="active">active</option>
                  <option value="banned">banned</option>
                </select>
              </label>
              <label>
                <span>数量</span>
                <input value={props.form.runLimit} min={1} onChange={(event) => props.onChange('runLimit', Number(event.target.value || 1))} type="number" />
              </label>
            </>
          )}
        </div>
      </section>

      <section className="config-group">
        <div className="group-title">
          <h3>运行环境</h3>
          <span>执行</span>
        </div>
        <div className="form-grid">
          <label>
            <span>并发</span>
            <input value={props.form.workers} min={1} onChange={(event) => props.onChange('workers', Number(event.target.value || 1))} type="number" />
          </label>
          <label>
            <span>浏览器</span>
            <select value={props.form.backend} onChange={(event) => props.onChange('backend', event.target.value)}>
              <option value="local">本机浏览器</option>
              <option value="docker">Docker 浏览器</option>
            </select>
          </label>
        </div>
        <div className="toggle-grid">
          <label className="toggle-line">
            <input checked={props.form.headless} onChange={(event) => props.onChange('headless', event.target.checked)} type="checkbox" />
            <span>无头浏览器</span>
          </label>
          <label className="toggle-line">
            <input checked={props.form.includeSecrets} onChange={(event) => props.onChange('includeSecrets', event.target.checked)} type="checkbox" />
            <span>写入 secrets</span>
          </label>
        </div>
      </section>

      <section className="config-group optional-group">
        <div className="group-title">
          <h3>代理</h3>
          <span>{proxyLabel}</span>
        </div>
        <div className="toggle-grid">
          <label className="toggle-line">
            <input checked={props.form.proxyEnabled} onChange={(event) => props.onChange('proxyEnabled', event.target.checked)} type="checkbox" />
            <span>启用代理</span>
          </label>
        </div>
        {props.form.proxyEnabled ? (
          <div className="form-grid">
            <label>
              <span>代理模式</span>
              <select value={props.form.proxyMode} onChange={(event) => props.onChange('proxyMode', event.target.value)}>
                <option value="single">单代理</option>
                <option value="pool">代理池文件</option>
              </select>
            </label>
            {props.form.proxyMode === 'pool' ? (
              <label>
                <span>代理池文件</span>
                <input value={props.form.proxyPoolFile} onChange={(event) => props.onChange('proxyPoolFile', event.target.value)} placeholder="proxies.txt" />
              </label>
            ) : (
              <label>
                <span>代理地址</span>
                <input value={props.form.proxy} onChange={(event) => props.onChange('proxy', event.target.value)} placeholder="http://user:pass@host:port" />
              </label>
            )}
          </div>
        ) : (
          <div className="config-empty-state">本次任务将直连运行。</div>
        )}
      </section>

      <section className="config-group optional-group">
        <div className="group-title">
          <h3>OAuth</h3>
          <span>可空</span>
        </div>
        <div className="form-grid">
          <label>
            <span>Workspace</span>
            <input value={props.form.workspaceId} onChange={(event) => props.onChange('workspaceId', event.target.value)} />
          </label>
          <label>
            <span>Organization</span>
            <input value={props.form.organizationId} onChange={(event) => props.onChange('organizationId', event.target.value)} />
          </label>
        </div>
      </section>

      <div className="action-bar">
        <button className="button primary" onClick={props.onStart} disabled={props.starting}>
          {props.starting ? '启动中...' : '启动任务'}
        </button>
        <button className="button danger" onClick={props.onStop} disabled={!props.canStop}>停止</button>
      </div>
      <p className={`message ${props.messageType}`}>{props.message || '等待操作'}</p>
    </aside>
  );
}
