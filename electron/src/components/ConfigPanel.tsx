import { useState, useEffect } from 'react';

interface Props {
  onClose: () => void;
}

interface ConfigState {
  provider: string;            // legacy, not shown in UI
  'anthropic-key': string;
  'openai-key': string;
  'sense.model': string;
  'reflect.model': string;
  'learn.model': string;
  verbosity: string;
}

type Tab = 'models' | 'keys';

const CONFIG_KEYS: (keyof ConfigState)[] = [
  'provider', 'anthropic-key', 'openai-key', 'sense.model', 'reflect.model', 'learn.model', 'verbosity',
];

type ModelRoute = {
  value: string;
  label: string;
  requires: '' | 'openai-key' | 'anthropic-key';
};

// Shared model menu for Sense and Reflect. Explicit CLI/API prefix in the
// label. Stored values use a prefix scheme (api:<provider>:<model>,
// cli:<tool>[:<model>]) so the backend can route unambiguously. Handled by
// resolveModel() in cmd/heb/pipeline.go.
const MODEL_OPTIONS: ModelRoute[] = [
  { value: '',                                label: 'Default (auto)',                        requires: '' },
  { value: 'api:anthropic:claude-haiku-4-5',  label: 'API (Anthropic claude-haiku-4-5)',      requires: 'anthropic-key' },
  { value: 'api:anthropic:claude-sonnet-4-6', label: 'API (Anthropic claude-sonnet-4-6)',     requires: 'anthropic-key' },
  { value: 'api:anthropic:claude-opus-4-7',   label: 'API (Anthropic claude-opus-4-7)',       requires: 'anthropic-key' },
  { value: 'api:openai:gpt-5.4',              label: 'API (OpenAI gpt-5.4)',                  requires: 'openai-key' },
  { value: 'api:openai:gpt-5.4-mini',         label: 'API (OpenAI gpt-5.4-mini)',             requires: 'openai-key' },
  { value: 'api:openai:gpt-5.4-nano',         label: 'API (OpenAI gpt-5.4-nano)',             requires: 'openai-key' },
  { value: 'api:openai:gpt-4.1',              label: 'API (OpenAI gpt-4.1)',                  requires: 'openai-key' },
  { value: 'api:openai:gpt-4.1-mini',         label: 'API (OpenAI gpt-4.1-mini)',             requires: 'openai-key' },
  { value: 'cli:claude',                      label: 'CLI (claude default)',                  requires: '' },
  { value: 'cli:claude:haiku-4-5',            label: 'CLI (claude --model haiku-4-5)',        requires: '' },
  { value: 'cli:claude:sonnet-4-6',           label: 'CLI (claude --model sonnet-4-6)',       requires: '' },
  { value: 'cli:claude:opus-4-7',             label: 'CLI (claude --model opus-4-7)',         requires: '' },
  { value: 'cli:gemini',                      label: 'CLI (gemini)',                          requires: '' },
];

// Learn menu — restricted to what learn.go actually routes today. No prefix
// scheme here because the existing backend reads raw values. Default is
// "resume" (CLI session replay, no API key required).
const LEARN_MODELS: ModelRoute[] = [
  { value: 'resume',         label: 'CLI (claude --resume)  — default', requires: '' },
  { value: 'gpt-5.4',        label: 'API (OpenAI gpt-5.4)',             requires: 'openai-key' },
  { value: 'gpt-5.4-mini',   label: 'API (OpenAI gpt-5.4-mini)',        requires: 'openai-key' },
  { value: 'gpt-5.4-nano',   label: 'API (OpenAI gpt-5.4-nano)',        requires: 'openai-key' },
  { value: 'gpt-4.1',        label: 'API (OpenAI gpt-4.1)',             requires: 'openai-key' },
  { value: 'gpt-4.1-mini',   label: 'API (OpenAI gpt-4.1-mini)',        requires: 'openai-key' },
];

export function ConfigPanel({ onClose }: Props) {
  const [tab, setTab] = useState<Tab>('models');
  const [config, setConfig] = useState<ConfigState>({
    provider: '',
    'anthropic-key': '',
    'openai-key': '',
    'sense.model': '',
    'reflect.model': '',
    'learn.model': '',
    verbosity: '',
  });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState<Set<string>>(new Set());

  useEffect(() => {
    loadConfig();
  }, []);

  async function loadConfig() {
    setLoading(true);
    const values: Partial<ConfigState> = {};
    for (const key of CONFIG_KEYS) {
      try {
        values[key] = await window.heb.configGet(key);
      } catch {
        values[key] = '';
      }
    }
    setConfig(values as ConfigState);
    setLoading(false);
  }

  function handleChange(key: keyof ConfigState, value: string) {
    setConfig(prev => ({ ...prev, [key]: value }));
    setDirty(prev => new Set(prev).add(key));
  }

  async function handleSave() {
    setSaving(true);
    try {
      for (const key of dirty) {
        await window.heb.configSet(key, config[key as keyof ConfigState]);
      }
      setDirty(new Set());
      onClose();
    } catch (err: any) {
      alert(`Failed to save: ${err.message}`);
    } finally {
      setSaving(false);
    }
  }

  const handleOverlayClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) onClose();
  };

  // ── Warnings ──────────────────────────────────────────────────────
  const warnings: string[] = [];
  const phaseWarn = (phase: string, choice: ModelRoute | undefined) => {
    if (!choice) return;
    if (choice.requires === 'anthropic-key' && !config['anthropic-key']) {
      warnings.push(`${phase} is set to "${choice.label}" but no Anthropic key is configured.`);
    }
    if (choice.requires === 'openai-key' && !config['openai-key']) {
      warnings.push(`${phase} is set to "${choice.label}" but no OpenAI key is configured.`);
    }
  };
  phaseWarn('Sense',   MODEL_OPTIONS.find(m => m.value === config['sense.model']));
  phaseWarn('Reflect', MODEL_OPTIONS.find(m => m.value === config['reflect.model']));
  // Normalize empty/unknown learn.model to the default ("resume") so the
  // dropdown always has a valid selection.
  const learnModelValue = LEARN_MODELS.some(m => m.value === config['learn.model'])
    ? config['learn.model']
    : 'resume';
  const learnChoice = LEARN_MODELS.find(m => m.value === learnModelValue);
  if (learnChoice?.requires === 'openai-key' && !config['openai-key']) {
    warnings.push(`Learn is set to "${learnChoice.label}" but no OpenAI key is configured — falls back to the Claude CLI.`);
  }
  if (learnChoice?.requires === 'anthropic-key' && !config['anthropic-key']) {
    warnings.push(`Learn is set to "${learnChoice.label}" but no Anthropic key is configured.`);
  }

  return (
    <div className="config-overlay" onClick={handleOverlayClick}>
      <div className="config-panel">
        <h2>Configuration</h2>

        {loading ? (
          <div style={{ textAlign: 'center', padding: '40px', color: 'var(--text-muted)' }}>
            <span className="spinner" /> Loading...
          </div>
        ) : (
          <>
            <div className="config-tabs">
              <button
                className={`config-tab ${tab === 'models' ? 'active' : ''}`}
                onClick={() => setTab('models')}
              >
                Models
              </button>
              <button
                className={`config-tab ${tab === 'keys' ? 'active' : ''}`}
                onClick={() => setTab('keys')}
              >
                API Keys
              </button>
            </div>

            {warnings.length > 0 && (
              <div className="config-warnings">
                {warnings.map((w, i) => (
                  <div key={i} className="config-warning">⚠ {w}</div>
                ))}
              </div>
            )}

            {tab === 'models' && (
              <>
                <div className="config-section">
                  <h3>Sense</h3>
                  <div className="config-row">
                    <label>Model</label>
                    <select
                      value={config['sense.model']}
                      onChange={e => handleChange('sense.model', e.target.value)}
                    >
                      {MODEL_OPTIONS.map(m => (
                        <option key={m.value} value={m.value}>{m.label}</option>
                      ))}
                    </select>
                  </div>
                  <div className="config-hint">
                    Sense classifies intent — favour a small/fast model (haiku, gpt-5.4-nano).
                  </div>
                </div>

                <div className="config-section">
                  <h3>Reflect</h3>
                  <div className="config-row">
                    <label>Model</label>
                    <select
                      value={config['reflect.model']}
                      onChange={e => handleChange('reflect.model', e.target.value)}
                    >
                      {MODEL_OPTIONS.map(m => (
                        <option key={m.value} value={m.value}>{m.label}</option>
                      ))}
                    </select>
                  </div>
                  <div className="config-hint">
                    Reflect reconciles memory and predicts risk — favour a stronger model
                    (sonnet, opus, gpt-5.4).
                  </div>
                </div>

                <div className="config-section">
                  <h3>Learn</h3>
                  <div className="config-row">
                    <label>Model</label>
                    <select
                      value={learnModelValue}
                      onChange={e => handleChange('learn.model', e.target.value)}
                    >
                      {LEARN_MODELS.map(m => (
                        <option key={m.value} value={m.value}>{m.label}</option>
                      ))}
                    </select>
                  </div>
                  <div className="config-hint">
                    --resume replays the current Claude Code session via the CLI — no API key needed.
                  </div>
                </div>

                <div className="config-section">
                  <h3>Execute</h3>
                  <div className="config-row">
                    <label>Model</label>
                    <input value="CLI (claude --print)" disabled style={{ opacity: 0.5 }} />
                  </div>
                  <div className="config-hint">
                    Execute is pinned to the Claude CLI and is not configurable.
                  </div>
                </div>

                <div className="config-section">
                  <h3>Output</h3>
                  <div className="config-row">
                    <label>Verbosity</label>
                    <select
                      value={config.verbosity}
                      onChange={e => handleChange('verbosity', e.target.value)}
                    >
                      <option value="quiet">Quiet</option>
                      <option value="loud">Loud</option>
                      <option value="mute">Mute</option>
                    </select>
                  </div>
                </div>
              </>
            )}

            {tab === 'keys' && (
              <>
                <div className="config-section">
                  <h3>API Keys</h3>
                  <div className="config-row">
                    <label>Anthropic</label>
                    <input
                      type="password"
                      value={config['anthropic-key']}
                      onChange={e => handleChange('anthropic-key', e.target.value)}
                      placeholder="sk-ant-..."
                    />
                  </div>
                  <div className="config-row">
                    <label>OpenAI</label>
                    <input
                      type="password"
                      value={config['openai-key']}
                      onChange={e => handleChange('openai-key', e.target.value)}
                      placeholder="sk-..."
                    />
                  </div>
                  <div className="config-hint">
                    Keys are stored in the global heb store. Any missing key triggers a
                    fallback to the Claude CLI for phases that support it.
                  </div>
                </div>
              </>
            )}

            <div className="config-footer">
              <button className="btn btn-secondary" onClick={onClose}>
                Cancel
              </button>
              <button
                className="btn btn-primary"
                onClick={handleSave}
                disabled={dirty.size === 0 || saving}
              >
                {saving ? 'Saving...' : 'Save'}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
