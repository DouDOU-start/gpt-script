import type { ConfigFormState, MessageType, WebConfig } from '../types';

export function ConfigPanel(props: {
  config: WebConfig | null;
  form: ConfigFormState;
  message: string;
  messageType: MessageType;
  saving: boolean;
  onChange: <K extends keyof ConfigFormState>(key: K, value: ConfigFormState[K]) => void;
  onSave: () => void;
}) {
  const proxyLabel = props.form.proxyEnabled ? (props.form.proxyMode === 'pool' ? '代理池' : '单代理') : '直连';
  const configPath = props.config?.config_path || 'config.yaml';

  return (
    <section className="panel config-panel reveal delay-1">
      <header className="page-hero config-hero">
        <div>
          <span className="proxy-state">默认参数</span>
          <h2>配置</h2>
          <p>{configPath}</p>
        </div>
        <div className="proxy-hero-stats" aria-label="配置状态">
          <div><strong>{props.form.backend}</strong><span>浏览器</span></div>
          <div><strong>{proxyLabel}</strong><span>代理</span></div>
        </div>
      </header>

      <section className="config-group primary-config-group">
        <div className="group-title">
          <h3>运行默认值</h3>
          <span>必看</span>
        </div>
        <div className="form-grid">
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
            <span>默认无头运行</span>
          </label>
          <label className="toggle-line">
            <input checked={props.form.includeSecrets} onChange={(event) => props.onChange('includeSecrets', event.target.checked)} type="checkbox" />
            <span>写入 secrets</span>
          </label>
        </div>
      </section>

      <section className="config-group proxy-config-group">
        <div className="group-title">
          <h3>代理策略</h3>
          <span>{proxyLabel}</span>
        </div>
        <div className="toggle-grid">
          <label className="toggle-line proxy-switch">
            <input checked={props.form.proxyEnabled} onChange={(event) => props.onChange('proxyEnabled', event.target.checked)} type="checkbox" />
            <span>默认启用代理</span>
          </label>
        </div>

        {props.form.proxyEnabled ? (
          <>
            <div className="form-grid">
              <label>
                <span>代理模式</span>
                <select value={props.form.proxyMode} onChange={(event) => props.onChange('proxyMode', event.target.value)}>
                  <option value="single">单代理</option>
                  <option value="pool">代理池</option>
                </select>
              </label>
              {props.form.proxyMode === 'single' ? (
                <label>
                  <span>代理地址</span>
                  <input value={props.form.proxy} onChange={(event) => props.onChange('proxy', event.target.value)} placeholder="http://user:pass@host:port" />
                </label>
              ) : (
                <label>
                  <span>代理池文件</span>
                  <input value={props.form.proxyPoolFile} onChange={(event) => props.onChange('proxyPoolFile', event.target.value)} placeholder="proxies.txt" />
                </label>
              )}
            </div>
            {props.form.proxyMode === 'pool' && (
              <label>
                <span>内联代理池</span>
                <textarea value={props.form.proxiesText} onChange={(event) => props.onChange('proxiesText', event.target.value)} placeholder="每行一个代理，可留空" />
              </label>
            )}
            <p className="hint-line">代理池文件按 config.yaml 所在目录解析相对路径；内联代理池为空时只使用文件。</p>
          </>
        ) : (
          <div className="config-empty-state">默认任务将直连运行。</div>
        )}
      </section>

      <div className="action-bar config-actions">
        <button className="button primary" onClick={props.onSave} disabled={props.saving}>
          {props.saving ? '保存中...' : '保存'}
        </button>
        <p className={`message ${props.messageType}`}>{props.message || '等待配置操作'}</p>
      </div>
    </section>
  );
}
