import { useState, useEffect, type ReactNode } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  getConfig,
  resetWorkspace,
  shredWorkspace,
  updateConfig,
  type ConfigSnapshot,
  type ConfigUpdate,
  type WorkspaceWipeResult,
} from '../../api/client'
import { showNotice } from '../ui/Toast'

type SectionId =
  | 'general'
  | 'company'
  | 'keys'
  | 'integrations'
  | 'intervals'
  | 'flags'
  | 'danger'

interface Section {
  id: SectionId
  icon: string
  name: string
}

const SECTIONS: Section[] = [
  { id: 'general', icon: '\u2699', name: 'General' },
  { id: 'company', icon: '\uD83C\uDFE2', name: 'Company' },
  { id: 'keys', icon: '\uD83D\uDD11', name: 'API Keys' },
  { id: 'integrations', icon: '\uD83D\uDD0C', name: 'Integrations' },
  { id: 'intervals', icon: '\u23F1', name: 'Polling' },
  { id: 'flags', icon: '\uD83D\uDDA5', name: 'CLI Flags' },
  { id: 'danger', icon: '\u26A0\uFE0F', name: 'Danger Zone' },
]

// ─── Styles ─────────────────────────────────────────────────────────────

const styles = {
  shell: {
    display: 'flex',
    height: '100%',
    minHeight: 0,
    flex: 1,
    overflow: 'hidden',
  } as const,
  nav: {
    width: 200,
    flexShrink: 0,
    borderRight: '1px solid var(--border)',
    padding: '16px 0',
    overflowY: 'auto' as const,
  } as const,
  navItem: (active: boolean) => ({
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '7px 16px',
    fontSize: 13,
    color: active ? 'var(--accent)' : 'var(--text-secondary)',
    cursor: 'pointer',
    border: 'none',
    background: active ? 'var(--accent-bg)' : 'none',
    width: '100%',
    textAlign: 'left' as const,
    fontFamily: 'var(--font-sans)',
    fontWeight: active ? 600 : 400,
  }),
  body: {
    flex: 1,
    overflowY: 'auto' as const,
    padding: '24px 32px',
    maxWidth: 680,
  } as const,
  sectionTitle: { fontSize: 18, fontWeight: 700, marginBottom: 4 } as const,
  sectionDesc: { fontSize: 13, color: 'var(--text-secondary)', marginBottom: 20, lineHeight: 1.5 } as const,
  banner: {
    display: 'flex',
    gap: 10,
    alignItems: 'flex-start',
    padding: '10px 14px',
    marginBottom: 16,
    background: 'var(--yellow-bg)',
    border: '1px solid var(--yellow)',
    borderRadius: 'var(--radius-md)',
    fontSize: 12,
    lineHeight: 1.5,
    color: 'var(--text)',
  } as const,
  row: { display: 'flex', alignItems: 'flex-start', gap: 12, marginBottom: 14 } as const,
  rowLabel: { width: 160, flexShrink: 0, paddingTop: 10 } as const,
  rowLabelName: { fontSize: 13, fontWeight: 500, color: 'var(--text)' } as const,
  rowLabelHint: { fontSize: 11, color: 'var(--text-tertiary)', marginTop: 2 } as const,
  rowField: { flex: 1, minWidth: 0 } as const,
  input: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    color: 'var(--text)',
    borderRadius: 'var(--radius-sm)',
    height: 36,
    fontSize: 13,
    padding: '0 10px',
    outline: 'none',
    width: '100%',
    fontFamily: 'var(--font-sans)',
  } as const,
  textarea: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    color: 'var(--text)',
    borderRadius: 'var(--radius-sm)',
    minHeight: 60,
    fontSize: 13,
    padding: '8px 10px',
    outline: 'none',
    width: '100%',
    fontFamily: 'var(--font-sans)',
    lineHeight: 1.5,
    resize: 'vertical' as const,
  },
  keyStatus: (set: boolean) => ({
    display: 'inline-flex',
    alignItems: 'center',
    fontSize: 11,
    fontWeight: 500,
    padding: '2px 8px',
    borderRadius: 'var(--radius-full)',
    whiteSpace: 'nowrap' as const,
    background: set ? 'var(--green-bg)' : 'var(--bg-warm)',
    color: set ? 'var(--green)' : 'var(--text-tertiary)',
  }),
  saveRow: { display: 'flex', gap: 8, marginTop: 20, paddingTop: 16, borderTop: '1px solid var(--border-light)' } as const,
  filePath: {
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
    color: 'var(--text-tertiary)',
    padding: '6px 10px',
    background: 'var(--bg-warm)',
    borderRadius: 'var(--radius-sm)',
    border: '1px solid var(--border-light)',
    userSelect: 'all' as const,
    wordBreak: 'break-all' as const,
  },
  table: { width: '100%', borderCollapse: 'collapse' as const, fontSize: 12 } as const,
  th: {
    textAlign: 'left' as const,
    fontWeight: 600,
    fontSize: 11,
    textTransform: 'uppercase' as const,
    letterSpacing: '0.06em',
    color: 'var(--text-tertiary)',
    padding: '6px 8px',
    borderBottom: '1px solid var(--border)',
  } as const,
  td: { padding: '6px 8px', borderBottom: '1px solid var(--border-light)', verticalAlign: 'top' as const } as const,
  tdFlag: {
    padding: '6px 8px',
    borderBottom: '1px solid var(--border-light)',
    verticalAlign: 'top' as const,
    fontFamily: 'var(--font-mono)',
    color: 'var(--accent)',
    whiteSpace: 'nowrap' as const,
  } as const,
  tdDesc: {
    padding: '6px 8px',
    borderBottom: '1px solid var(--border-light)',
    verticalAlign: 'top' as const,
    color: 'var(--text-secondary)',
  } as const,
  groupTitle: {
    fontSize: 11,
    fontWeight: 600,
    textTransform: 'uppercase' as const,
    letterSpacing: '0.06em',
    color: 'var(--text-tertiary)',
    marginBottom: 10,
    paddingBottom: 6,
    borderBottom: '1px solid var(--border-light)',
  } as const,
}

// ─── Small components ───────────────────────────────────────────────────

interface FieldProps {
  label: string
  hint?: string
  children: ReactNode
}

function Field({ label, hint, children }: FieldProps) {
  return (
    <div style={styles.row}>
      <div style={styles.rowLabel}>
        <div style={styles.rowLabelName}>{label}</div>
        {hint && <div style={styles.rowLabelHint}>{hint}</div>}
      </div>
      <div style={styles.rowField}>{children}</div>
    </div>
  )
}

interface SaveButtonProps {
  label: string
  onSave: () => Promise<void> | void
}

function SaveButton({ label, onSave }: SaveButtonProps) {
  const [state, setState] = useState<'idle' | 'saving' | 'saved'>('idle')

  const handle = async () => {
    if (state === 'saving') return
    setState('saving')
    try {
      await onSave()
      setState('saved')
      setTimeout(() => setState('idle'), 1500)
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err)
      showNotice('Save failed: ' + msg, 'error')
      setState('idle')
    }
  }

  return (
    <div style={styles.saveRow}>
      <button
        className="btn btn-primary btn-sm"
        onClick={handle}
        disabled={state === 'saving'}
      >
        {state === 'saving' ? 'Saving...' : state === 'saved' ? 'Saved' : label}
      </button>
    </div>
  )
}

interface KeyFieldProps {
  hasValue: boolean
  placeholder: string
  value: string
  onChange: (v: string) => void
}

function KeyField({ hasValue, placeholder, value, onChange }: KeyFieldProps) {
  return (
    <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
      <input
        type="password"
        className="input"
        style={{ ...styles.input, flex: 1, fontFamily: 'var(--font-mono)', fontSize: 12 }}
        placeholder={hasValue ? '\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022 (set)' : placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      <span style={styles.keyStatus(hasValue)}>{hasValue ? 'Set' : 'Not set'}</span>
    </div>
  )
}

// ─── Section components ─────────────────────────────────────────────────

interface SectionProps {
  cfg: ConfigSnapshot
  save: (patch: ConfigUpdate) => Promise<void>
}

function GeneralSection({ cfg, save }: SectionProps) {
  const [provider, setProvider] = useState(cfg.llm_provider ?? 'claude-code')
  const [memory, setMemory] = useState(cfg.memory_backend ?? 'nex')
  const [teamLead, setTeamLead] = useState(cfg.team_lead_slug ?? '')
  const [maxConcurrent, setMaxConcurrent] = useState(
    cfg.max_concurrent_agents ? String(cfg.max_concurrent_agents) : '',
  )
  const [format, setFormat] = useState(cfg.default_format ?? 'text')
  const [timeout, setTimeout] = useState(
    cfg.default_timeout ? String(cfg.default_timeout) : '',
  )
  const [blueprint, setBlueprint] = useState(cfg.blueprint ?? '')
  const [email, setEmail] = useState(cfg.email ?? '')
  const [devUrl, setDevUrl] = useState(cfg.dev_url ?? '')

  const onSave = async () => {
    const patch: ConfigUpdate = {
      llm_provider: provider as ConfigUpdate['llm_provider'],
      memory_backend: memory as ConfigUpdate['memory_backend'],
      default_format: format,
      blueprint,
      email,
      dev_url: devUrl,
      team_lead_slug: teamLead,
    }
    if (maxConcurrent) patch.max_concurrent_agents = parseInt(maxConcurrent, 10)
    if (timeout) patch.default_timeout = parseInt(timeout, 10)
    await save(patch)
  }

  return (
    <div>
      <h2 style={styles.sectionTitle}>General</h2>
      <p style={styles.sectionDesc}>Core runtime settings. These map to CLI flags and config file entries.</p>

      <div style={styles.banner}>
        <span style={{ fontSize: 14, flexShrink: 0 }}>{'\u26A0'}</span>
        <div>
          <strong>Restart required for LLM Provider and Memory Backend changes. </strong>
          New values save immediately, but agents already running keep their launch-time settings. Run{' '}
          <code style={{ fontFamily: 'var(--font-mono)', padding: '1px 4px', background: 'var(--bg-warm)', borderRadius: 3 }}>
            wuphf shred
          </code>{' '}
          then relaunch to apply.
        </div>
      </div>

      <Field label="LLM Provider" hint="--provider">
        <select style={styles.input} value={provider} onChange={(e) => setProvider(e.target.value as typeof provider)}>
          <option value="claude-code">Claude Code</option>
          <option value="codex">Codex</option>
        </select>
      </Field>

      <Field label="Memory Backend" hint="--memory-backend">
        <select style={styles.input} value={memory} onChange={(e) => setMemory(e.target.value as typeof memory)}>
          <option value="nex">Nex</option>
          <option value="gbrain">GBrain</option>
          <option value="none">None (local only)</option>
        </select>
      </Field>

      <Field label="Team Lead" hint="Default agent that leads operations">
        <input style={styles.input} placeholder="e.g. ceo" value={teamLead} onChange={(e) => setTeamLead(e.target.value)} />
      </Field>

      <Field label="Max Concurrent" hint="Parallel agent limit">
        <input
          style={styles.input}
          type="number"
          min={1}
          placeholder="Unlimited"
          value={maxConcurrent}
          onChange={(e) => setMaxConcurrent(e.target.value)}
        />
      </Field>

      <Field label="Output Format" hint="--format">
        <select style={styles.input} value={format} onChange={(e) => setFormat(e.target.value)}>
          <option value="text">Text</option>
          <option value="json">JSON</option>
        </select>
      </Field>

      <Field label="Timeout (ms)" hint="Default command timeout">
        <input
          style={styles.input}
          type="number"
          min={1000}
          placeholder="120000"
          value={timeout}
          onChange={(e) => setTimeout(e.target.value)}
        />
      </Field>

      <Field label="Blueprint" hint="--blueprint">
        <input style={styles.input} placeholder="Operation blueprint ID" value={blueprint} onChange={(e) => setBlueprint(e.target.value)} />
      </Field>

      <Field label="Email" hint="Identity scope for integrations">
        <input style={styles.input} type="email" placeholder="you@company.com" value={email} onChange={(e) => setEmail(e.target.value)} />
      </Field>

      <Field label="Dev URL" hint="API base URL override">
        <input style={styles.input} placeholder="https://app.nex.ai" value={devUrl} onChange={(e) => setDevUrl(e.target.value)} />
      </Field>

      <SaveButton label="Save general settings" onSave={onSave} />

      {cfg.config_path && (
        <div style={{ marginTop: 24 }}>
          <div style={styles.groupTitle}>Config file</div>
          <div style={styles.filePath}>{cfg.config_path}</div>
        </div>
      )}
    </div>
  )
}

function CompanySection({ cfg, save }: SectionProps) {
  const [name, setName] = useState(cfg.company_name ?? '')
  const [description, setDescription] = useState(cfg.company_description ?? '')
  const [goals, setGoals] = useState(cfg.company_goals ?? '')
  const [size, setSize] = useState(cfg.company_size ?? '')
  const [priority, setPriority] = useState(cfg.company_priority ?? '')

  const onSave = () =>
    save({
      company_name: name,
      company_description: description,
      company_goals: goals,
      company_size: size,
      company_priority: priority,
    })

  return (
    <div>
      <h2 style={styles.sectionTitle}>Company</h2>
      <p style={styles.sectionDesc}>
        Organizational context injected into agent system prompts. The more you fill in, the better agents understand your business.
      </p>

      <Field label="Name" hint="Your company or project name">
        <input style={styles.input} placeholder="Acme Corp" value={name} onChange={(e) => setName(e.target.value)} />
      </Field>

      <Field label="Description" hint="One-liner about the business">
        <textarea style={styles.textarea} placeholder="What does your company do?" value={description} onChange={(e) => setDescription(e.target.value)} />
      </Field>

      <Field label="Goals" hint="What the team is working toward">
        <textarea style={styles.textarea} placeholder="Current organizational goals" value={goals} onChange={(e) => setGoals(e.target.value)} />
      </Field>

      <Field label="Size" hint="Team or company size">
        <input style={styles.input} placeholder="e.g. 5, 50, 500" value={size} onChange={(e) => setSize(e.target.value)} />
      </Field>

      <Field label="Priority" hint="What matters most right now">
        <textarea style={styles.textarea} placeholder="Immediate priority focus" value={priority} onChange={(e) => setPriority(e.target.value)} />
      </Field>

      <SaveButton label="Save company info" onSave={onSave} />
    </div>
  )
}

interface KeyDef {
  field: keyof ConfigUpdate
  flag: keyof ConfigSnapshot
  label: string
  placeholder: string
  env: string
}

const KEY_DEFS: KeyDef[] = [
  { field: 'api_key', flag: 'api_key_set', label: 'Nex API Key', placeholder: 'nex_...', env: 'WUPHF_API_KEY' },
  { field: 'anthropic_api_key', flag: 'anthropic_key_set', label: 'Anthropic', placeholder: 'sk-ant-...', env: 'ANTHROPIC_API_KEY' },
  { field: 'openai_api_key', flag: 'openai_key_set', label: 'OpenAI', placeholder: 'sk-...', env: 'OPENAI_API_KEY' },
  { field: 'gemini_api_key', flag: 'gemini_key_set', label: 'Gemini', placeholder: 'AI...', env: 'GEMINI_API_KEY' },
  { field: 'minimax_api_key', flag: 'minimax_key_set', label: 'Minimax', placeholder: 'mm-...', env: 'MINIMAX_API_KEY' },
  { field: 'one_api_key', flag: 'one_key_set', label: 'One (integration)', placeholder: 'one_...', env: 'ONE_SECRET' },
  { field: 'composio_api_key', flag: 'composio_key_set', label: 'Composio', placeholder: 'cmp_...', env: 'COMPOSIO_API_KEY' },
  { field: 'telegram_bot_token', flag: 'telegram_token_set', label: 'Telegram Bot', placeholder: '123456:ABC...', env: 'WUPHF_TELEGRAM_BOT_TOKEN' },
]

function KeysSection({ cfg, save }: SectionProps) {
  const [values, setValues] = useState<Record<string, string>>({})

  const onSave = async () => {
    const entries = Object.entries(values).filter(([, v]) => v.trim() !== '')
    if (entries.length === 0) {
      showNotice('No keys entered. Leave blank to keep existing keys.', 'info')
      throw new Error('no_keys_entered')
    }
    const patch: ConfigUpdate = {}
    for (const [k, v] of entries) {
      ;(patch as Record<string, string>)[k] = v
    }
    await save(patch)
    setValues({})
  }

  return (
    <div>
      <h2 style={styles.sectionTitle}>API Keys</h2>
      <p style={styles.sectionDesc}>
        Authentication credentials for external services. Keys are stored in your local config file and never transmitted to WUPHF servers.
        Enter a new value to update, or leave blank to keep the current key.
      </p>

      {KEY_DEFS.map((def) => (
        <Field key={def.field} label={def.label} hint={`Env: ${def.env}`}>
          <KeyField
            hasValue={Boolean(cfg[def.flag])}
            placeholder={def.placeholder}
            value={values[def.field] ?? ''}
            onChange={(v) => setValues((prev) => ({ ...prev, [def.field]: v }))}
          />
        </Field>
      ))}

      <SaveButton label="Save API keys" onSave={onSave} />
    </div>
  )
}

function IntegrationsSection({ cfg, save }: SectionProps) {
  const [actionProvider, setActionProvider] = useState<string>(cfg.action_provider ?? 'auto')
  const [gatewayUrl, setGatewayUrl] = useState(cfg.openclaw_gateway_url ?? '')
  const [openclawToken, setOpenclawToken] = useState('')

  const onSave = async () => {
    const patch: ConfigUpdate = {
      action_provider: actionProvider as ConfigUpdate['action_provider'],
    }
    if (gatewayUrl) patch.openclaw_gateway_url = gatewayUrl
    if (openclawToken) patch.openclaw_token = openclawToken
    await save(patch)
    setOpenclawToken('')
  }

  return (
    <div>
      <h2 style={styles.sectionTitle}>Integrations</h2>
      <p style={styles.sectionDesc}>External service connections and action providers.</p>

      <Field label="Action Provider" hint="External action routing">
        <select style={styles.input} value={actionProvider} onChange={(e) => setActionProvider(e.target.value)}>
          <option value="auto">Auto</option>
          <option value="one">One CLI</option>
          <option value="composio">Composio</option>
        </select>
      </Field>

      <div style={{ marginTop: 20 }}>
        <div style={styles.groupTitle}>OpenClaw</div>
        <Field label="Gateway URL" hint="WebSocket endpoint">
          <input
            style={{ ...styles.input, fontFamily: 'var(--font-mono)', fontSize: 12 }}
            placeholder="ws://127.0.0.1:18789"
            value={gatewayUrl}
            onChange={(e) => setGatewayUrl(e.target.value)}
          />
        </Field>
        <Field label="Token" hint="Gateway auth token">
          <KeyField
            hasValue={Boolean(cfg.openclaw_token_set)}
            placeholder="oc_..."
            value={openclawToken}
            onChange={setOpenclawToken}
          />
        </Field>
      </div>

      <div style={{ marginTop: 20 }}>
        <div style={styles.groupTitle}>Workspace</div>
        <Field label="Workspace ID" hint="Read-only">
          <input
            style={{ ...styles.input, opacity: 0.6, cursor: 'default' }}
            readOnly
            placeholder="(set via Nex registration)"
            value={cfg.workspace_id ?? ''}
          />
        </Field>
        <Field label="Workspace Slug" hint="Read-only">
          <input
            style={{ ...styles.input, opacity: 0.6, cursor: 'default' }}
            readOnly
            placeholder="(set via Nex registration)"
            value={cfg.workspace_slug ?? ''}
          />
        </Field>
      </div>

      <SaveButton label="Save integration settings" onSave={onSave} />
    </div>
  )
}

function IntervalsSection({ cfg, save }: SectionProps) {
  const [insights, setInsights] = useState(String(cfg.insights_poll_minutes ?? 15))
  const [followUp, setFollowUp] = useState(String(cfg.task_follow_up_minutes ?? 60))
  const [reminder, setReminder] = useState(String(cfg.task_reminder_minutes ?? 30))
  const [recheck, setRecheck] = useState(String(cfg.task_recheck_minutes ?? 15))

  const onSave = () =>
    save({
      insights_poll_minutes: parseInt(insights, 10) || 15,
      task_follow_up_minutes: parseInt(followUp, 10) || 60,
      task_reminder_minutes: parseInt(reminder, 10) || 30,
      task_recheck_minutes: parseInt(recheck, 10) || 15,
    })

  return (
    <div>
      <h2 style={styles.sectionTitle}>Polling Intervals</h2>
      <p style={styles.sectionDesc}>
        How often background processes check for updates. All values in minutes. Minimum 2 minutes.
      </p>

      <Field label="Insights" hint="Context graph polling">
        <input style={styles.input} type="number" min={2} placeholder="15" value={insights} onChange={(e) => setInsights(e.target.value)} />
      </Field>
      <Field label="Task Follow-up" hint="Post-completion check-in">
        <input style={styles.input} type="number" min={2} placeholder="60" value={followUp} onChange={(e) => setFollowUp(e.target.value)} />
      </Field>
      <Field label="Task Reminder" hint="Stalled task nudge">
        <input style={styles.input} type="number" min={2} placeholder="30" value={reminder} onChange={(e) => setReminder(e.target.value)} />
      </Field>
      <Field label="Task Recheck" hint="Progress re-evaluation">
        <input style={styles.input} type="number" min={2} placeholder="15" value={recheck} onChange={(e) => setRecheck(e.target.value)} />
      </Field>

      <SaveButton label="Save intervals" onSave={onSave} />
    </div>
  )
}

const CLI_FLAGS: [string, string][] = [
  ['--provider <name>', 'LLM provider (claude-code, codex)'],
  ['--memory-backend <name>', 'Memory backend (nex, gbrain, none)'],
  ['--blueprint <id>', 'Operation blueprint for this run'],
  ['--tui', 'Launch tmux TUI instead of web UI'],
  ['--web-port <port>', 'Web UI port (default: 7891)'],
  ['--broker-port <port>', 'Local broker port (default: 7890)'],
  ['--opus-ceo', 'Upgrade CEO agent to Opus model'],
  ['--collab', 'Collaborative mode (all agents see all messages)'],
  ['--1o1', 'Direct 1:1 session with a single agent'],
  ['--unsafe', 'Bypass agent permission checks (dev only)'],
  ['--no-nex', 'Disable Nex for this session'],
  ['--no-open', 'Skip auto-opening browser on launch'],
  ['--from-scratch', 'Start without saved blueprint'],
  ['--threads-collapsed', 'Start with threads collapsed'],
  ['--cmd <command>', 'Run a slash command non-interactively'],
  ['--format <fmt>', 'Output format (text, json)'],
  ['--api-key <key>', 'Nex API key override'],
  ['--version', 'Print version and exit'],
  ['--help-all', 'Show all flags including internal ones'],
]

const ENV_VARS: [string, string][] = [
  ['WUPHF_LLM_PROVIDER', 'LLM provider override'],
  ['WUPHF_MEMORY_BACKEND', 'Memory backend override'],
  ['WUPHF_API_KEY', 'Nex API key'],
  ['WUPHF_BROKER_PORT', 'Broker port'],
  ['WUPHF_CONFIG_PATH', 'Config file path override'],
  ['WUPHF_RUNTIME_HOME', 'Runtime state directory'],
  ['WUPHF_NO_NEX', 'Disable Nex (1/true/yes)'],
  ['WUPHF_START_FROM_SCRATCH', 'Start without blueprint (1)'],
  ['WUPHF_ONE_ON_ONE', 'Enable 1:1 mode (1)'],
  ['WUPHF_HEADLESS_PROVIDER', 'Headless provider override'],
  ['WUPHF_INSIGHTS_INTERVAL_MINUTES', 'Insights poll interval'],
  ['WUPHF_TASK_FOLLOWUP_MINUTES', 'Task follow-up interval'],
  ['WUPHF_TASK_REMINDER_MINUTES', 'Task reminder interval'],
  ['WUPHF_TASK_RECHECK_MINUTES', 'Task recheck interval'],
]

function FlagsSection() {
  return (
    <div>
      <h2 style={styles.sectionTitle}>CLI Flags</h2>
      <p style={styles.sectionDesc}>
        All flags available when launching wuphf from the terminal. These are runtime-only and not persisted in the config file.
      </p>

      <table style={styles.table}>
        <thead>
          <tr>
            <th style={styles.th}>Flag</th>
            <th style={styles.th}>Description</th>
          </tr>
        </thead>
        <tbody>
          {CLI_FLAGS.map(([flag, desc]) => (
            <tr key={flag}>
              <td style={styles.tdFlag}>{flag}</td>
              <td style={styles.tdDesc}>{desc}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <div style={{ marginTop: 24 }}>
        <div style={styles.groupTitle}>Environment Variables</div>
        <p style={{ fontSize: 12, color: 'var(--text-secondary)', lineHeight: 1.6, marginBottom: 12 }}>
          Settings resolve in order: CLI flag → environment variable → config file → default. Set these in your shell profile to override config file values.
        </p>
        <table style={styles.table}>
          <thead>
            <tr>
              <th style={styles.th}>Variable</th>
              <th style={styles.th}>Purpose</th>
            </tr>
          </thead>
          <tbody>
            {ENV_VARS.map(([v, p]) => (
              <tr key={v}>
                <td style={styles.tdFlag}>{v}</td>
                <td style={styles.tdDesc}>{p}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// ─── Danger Zone ────────────────────────────────────────────────────────

// dangerStyles lives next to the section because it's the only caller and the
// warning palette shouldn't bleed into the rest of the app's styling surface.
const dangerStyles = {
  card: (severity: 'warn' | 'critical') => ({
    marginBottom: 20,
    padding: 20,
    borderRadius: 'var(--radius-md)',
    border: `1px solid ${severity === 'critical' ? 'var(--red, #e5484d)' : 'var(--yellow, #e5a00d)'}`,
    background: severity === 'critical' ? 'var(--red-bg, rgba(229,72,77,0.08))' : 'var(--yellow-bg, rgba(229,160,13,0.08))',
  }),
  cardTitle: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    fontSize: 15,
    fontWeight: 700,
    color: 'var(--text)',
    marginBottom: 6,
  } as const,
  cardSubtitle: {
    fontSize: 13,
    color: 'var(--text-secondary)',
    marginBottom: 14,
    lineHeight: 1.5,
  } as const,
  listLabel: {
    fontSize: 11,
    fontWeight: 600,
    textTransform: 'uppercase' as const,
    letterSpacing: '0.06em',
    color: 'var(--text-tertiary)',
    marginTop: 8,
    marginBottom: 4,
  } as const,
  list: { margin: 0, paddingLeft: 20, fontSize: 12, lineHeight: 1.7, color: 'var(--text-secondary)' } as const,
  button: (severity: 'warn' | 'critical') => ({
    marginTop: 16,
    padding: '9px 16px',
    fontSize: 13,
    fontWeight: 600,
    border: 'none',
    borderRadius: 'var(--radius-sm)',
    cursor: 'pointer' as const,
    color: '#fff',
    background: severity === 'critical' ? 'var(--red, #e5484d)' : 'var(--yellow, #e5a00d)',
    fontFamily: 'var(--font-sans)',
  }),
  modalBackdrop: {
    position: 'fixed' as const,
    inset: 0,
    background: 'rgba(0,0,0,0.6)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    zIndex: 1000,
  },
  modalPanel: {
    width: 'min(520px, calc(100vw - 40px))',
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-md)',
    padding: 24,
    boxShadow: '0 20px 60px rgba(0,0,0,0.4)',
  } as const,
  modalTitle: { fontSize: 17, fontWeight: 700, color: 'var(--text)', marginBottom: 10 } as const,
  modalBody: { fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.55, marginBottom: 16 } as const,
  modalInputLabel: {
    fontSize: 11,
    fontWeight: 600,
    textTransform: 'uppercase' as const,
    letterSpacing: '0.06em',
    color: 'var(--text-tertiary)',
    marginBottom: 6,
    display: 'block',
  } as const,
  modalInput: {
    width: '100%',
    background: 'var(--bg-warm)',
    border: '1px solid var(--border)',
    color: 'var(--text)',
    borderRadius: 'var(--radius-sm)',
    height: 38,
    fontSize: 14,
    padding: '0 12px',
    outline: 'none',
    fontFamily: 'var(--font-mono)',
  } as const,
  modalRow: { display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 18 } as const,
  modalCancel: {
    padding: '9px 16px',
    fontSize: 13,
    fontWeight: 500,
    border: '1px solid var(--border)',
    borderRadius: 'var(--radius-sm)',
    cursor: 'pointer' as const,
    color: 'var(--text)',
    background: 'transparent',
    fontFamily: 'var(--font-sans)',
  } as const,
  modalConfirm: (severity: 'warn' | 'critical', enabled: boolean) => ({
    padding: '9px 16px',
    fontSize: 13,
    fontWeight: 600,
    border: 'none',
    borderRadius: 'var(--radius-sm)',
    cursor: enabled ? 'pointer' : ('not-allowed' as const),
    color: '#fff',
    background: enabled
      ? severity === 'critical'
        ? 'var(--red, #e5484d)'
        : 'var(--yellow, #e5a00d)'
      : 'var(--bg-warm)',
    opacity: enabled ? 1 : 0.6,
    fontFamily: 'var(--font-sans)',
  }),
}

const CONFIRM_PHRASE = 'i can spell responsibility'

interface WipeModalProps {
  title: string
  severity: 'warn' | 'critical'
  intro: ReactNode
  confirmLabel: string
  busy: boolean
  onConfirm: () => void
  onCancel: () => void
}

// WipeModal gates a destructive action behind a type-the-exact-phrase confirm.
// The placeholder and the body copy both surface the full phrase so there's no mystery
// about what to type — we want the friction, not the guesswork.
function WipeModal({ title, severity, intro, confirmLabel, busy, onConfirm, onCancel }: WipeModalProps) {
  const [value, setValue] = useState('')
  const enabled = !busy && value.trim().toLowerCase() === CONFIRM_PHRASE

  return (
    <div style={dangerStyles.modalBackdrop} onClick={busy ? undefined : onCancel}>
      <div style={dangerStyles.modalPanel} onClick={(e) => e.stopPropagation()}>
        <div style={dangerStyles.modalTitle}>{title}</div>
        <div style={dangerStyles.modalBody}>{intro}</div>
        <label style={dangerStyles.modalInputLabel}>
          Type <code>{CONFIRM_PHRASE}</code> to confirm
        </label>
        <input
          type="text"
          style={dangerStyles.modalInput}
          placeholder={CONFIRM_PHRASE}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          autoFocus
          disabled={busy}
        />
        <div style={dangerStyles.modalRow}>
          <button type="button" style={dangerStyles.modalCancel} onClick={onCancel} disabled={busy}>
            Cancel
          </button>
          <button
            type="button"
            style={dangerStyles.modalConfirm(severity, enabled)}
            onClick={enabled ? onConfirm : undefined}
            disabled={!enabled}
          >
            {busy ? 'Working…' : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}

type DangerAction = 'reset' | 'shred'

function DangerZoneSection() {
  const [open, setOpen] = useState<DangerAction | null>(null)
  const [busy, setBusy] = useState(false)

  const handleReset = async () => {
    setBusy(true)
    try {
      const result: WorkspaceWipeResult = await resetWorkspace()
      if (!result.ok) {
        showNotice(result.error || 'Reset failed', 'error')
        setBusy(false)
        return
      }
      showNotice('Broker state cleared. Reloading…', 'success')
      setTimeout(() => window.location.reload(), 400)
    } catch (err) {
      showNotice(err instanceof Error ? err.message : 'Reset failed', 'error')
      setBusy(false)
    }
  }

  const handleShred = async () => {
    setBusy(true)
    try {
      const result: WorkspaceWipeResult = await shredWorkspace()
      if (!result.ok) {
        showNotice(result.error || 'Shred failed', 'error')
        setBusy(false)
        return
      }
      showNotice('Workspace shredded. Returning to onboarding…', 'success')
      setTimeout(() => window.location.reload(), 400)
    } catch (err) {
      showNotice(err instanceof Error ? err.message : 'Shred failed', 'error')
      setBusy(false)
    }
  }

  return (
    <div>
      <div style={styles.sectionTitle}>Danger Zone</div>
      <div style={styles.sectionDesc}>
        Irreversible operations on this workspace. Read each card carefully — the web UI does not
        kill the running broker process, so after either action you may need to re-launch
        <code style={{ margin: '0 4px' }}>wuphf</code> from your terminal for the change to fully
        take effect.
      </div>

      {/* RESET — narrow: broker runtime state only */}
      <div style={dangerStyles.card('warn')}>
        <div style={dangerStyles.cardTitle}>
          <span>{'\u{1F501}'}</span>
          <span>Reset broker state</span>
        </div>
        <div style={dangerStyles.cardSubtitle}>
          Use this when something is stuck — an agent wedged, the queue won't drain, messages stop
          flowing — and you want a clean restart without losing your team or work.
        </div>
        <div style={dangerStyles.listLabel}>Clears</div>
        <ul style={dangerStyles.list}>
          <li>Broker runtime state (<code>~/.wuphf/team/broker-state.json</code>)</li>
          <li>Office PID file and in-memory snapshot</li>
        </ul>
        <div style={dangerStyles.listLabel}>Preserved</div>
        <ul style={dangerStyles.list}>
          <li>Your team roster, company identity, tasks, workflows</li>
          <li>All on-disk history (logs, sessions, artifacts)</li>
          <li>API keys and config</li>
        </ul>
        <button
          type="button"
          style={dangerStyles.button('warn')}
          onClick={() => setOpen('reset')}
          disabled={busy}
        >
          Reset broker state…
        </button>
      </div>

      {/* SHRED — full wipe */}
      <div style={dangerStyles.card('critical')}>
        <div style={dangerStyles.cardTitle}>
          <span>{'\u{1F4A5}'}</span>
          <span>Shred workspace</span>
        </div>
        <div style={dangerStyles.cardSubtitle}>
          Full wipe. Deletes your team, company identity, office task receipts, and saved
          workflows, then reopens the onboarding wizard on the next load. Use this to start
          completely fresh or to try a different blueprint.
        </div>
        <div style={dangerStyles.listLabel}>Deletes</div>
        <ul style={dangerStyles.list}>
          <li>
            Onboarding flag (<code>~/.wuphf/onboarded.json</code>) so the wizard reopens
          </li>
          <li>Company identity (<code>~/.wuphf/company.json</code>)</li>
          <li>Team, office, workflows directories under <code>~/.wuphf/</code></li>
          <li>Broker runtime state (same as Reset)</li>
        </ul>
        <div style={dangerStyles.listLabel}>Preserved</div>
        <ul style={dangerStyles.list}>
          <li>
            <strong>Task worktrees</strong> — uncommitted work on branches stays on disk
          </li>
          <li>Conversation logs and session history</li>
          <li>LLM provider caches (codex-headless, providers/, openclaw/)</li>
          <li>Your global config (<code>config.json</code>) and API keys</li>
        </ul>
        <button
          type="button"
          style={dangerStyles.button('critical')}
          onClick={() => setOpen('shred')}
          disabled={busy}
        >
          Shred workspace…
        </button>
      </div>

      {open === 'reset' && (
        <WipeModal
          title="Reset broker state?"
          severity="warn"
          intro={
            <>
              This clears the broker's on-disk runtime state and reboots the office from a clean
              slate. Your team, company, tasks, and workflows are all kept. If this doesn't
              unblock things, try <strong>Shred workspace</strong> instead.
            </>
          }
          confirmLabel="Reset broker state"
          busy={busy}
          onConfirm={handleReset}
          onCancel={() => setOpen(null)}
        />
      )}

      {open === 'shred' && (
        <WipeModal
          title="Shred this workspace?"
          severity="critical"
          intro={
            <>
              This permanently deletes your team, company identity, office task receipts, and
              saved workflows. Onboarding will reopen on next load. Task worktrees, logs, and
              session history are kept. <strong>This cannot be undone.</strong>
            </>
          }
          confirmLabel="Shred workspace"
          busy={busy}
          onConfirm={handleShred}
          onCancel={() => setOpen(null)}
        />
      )}
    </div>
  )
}

// ─── Main component ─────────────────────────────────────────────────────

export function SettingsApp() {
  const [section, setSection] = useState<SectionId>('general')
  const queryClient = useQueryClient()

  const { data, isLoading, error } = useQuery({
    queryKey: ['config'],
    queryFn: getConfig,
    staleTime: 10_000,
  })

  const saveMutation = useMutation({
    mutationFn: (patch: ConfigUpdate) => updateConfig(patch),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['config'] })
      showNotice('Settings saved.', 'success')
    },
    onError: (err: unknown) => {
      const message = err instanceof Error ? err.message : 'Failed to save settings'
      showNotice(message, 'error')
    },
  })

  // Reset section state when data changes so form values pick up latest server state
  const [dataKey, setDataKey] = useState(0)
  useEffect(() => {
    setDataKey((k) => k + 1)
  }, [data])

  const save = async (patch: ConfigUpdate) => {
    await saveMutation.mutateAsync(patch)
  }

  if (isLoading) {
    return (
      <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 14 }}>
        Loading settings...
      </div>
    )
  }

  if (error || !data) {
    return (
      <div style={{ padding: 40, textAlign: 'center', color: 'var(--text-tertiary)', fontSize: 14 }}>
        Failed to load settings: {error instanceof Error ? error.message : String(error)}
      </div>
    )
  }

  return (
    <div style={styles.shell}>
      <nav style={styles.nav}>
        {SECTIONS.map((sec) => (
          <button key={sec.id} style={styles.navItem(sec.id === section)} onClick={() => setSection(sec.id)}>
            <span style={{ width: 16, textAlign: 'center', flexShrink: 0 }}>{sec.icon}</span>
            <span>{sec.name}</span>
          </button>
        ))}
      </nav>
      <div style={styles.body} key={dataKey}>
        {section === 'general' && <GeneralSection cfg={data} save={save} />}
        {section === 'company' && <CompanySection cfg={data} save={save} />}
        {section === 'keys' && <KeysSection cfg={data} save={save} />}
        {section === 'integrations' && <IntegrationsSection cfg={data} save={save} />}
        {section === 'intervals' && <IntervalsSection cfg={data} save={save} />}
        {section === 'flags' && <FlagsSection />}
        {section === 'danger' && <DangerZoneSection />}
      </div>
    </div>
  )
}
