import { useState, useEffect, useCallback } from 'react'
import { useAppStore } from '../../stores/app'
import { get, post } from '../../api/client'
import { ONBOARDING_COPY } from '../../lib/constants'
import '../../styles/onboarding.css'

/* ═══════════════════════════════════════════
   Types
   ═══════════════════════════════════════════ */

interface BlueprintTemplate {
  id: string
  name: string
  description: string
  emoji?: string
  agents?: BlueprintAgent[]
}

interface BlueprintAgent {
  slug: string
  name: string
  role: string
  emoji?: string
  checked?: boolean
  // built_in marks the lead agent — always included, never removable.
  // The backend also refuses to disable or remove a BuiltIn member, so
  // even if someone bypassed this UI, the broker would reject the write.
  built_in?: boolean
}

interface TaskTemplate {
  id: string
  name: string
  description: string
  emoji?: string
  prompt?: string
}

type WizardStep = 'welcome' | 'templates' | 'identity' | 'team' | 'setup' | 'task'

// Step order: company info before blueprint. The blueprint picker is a
// decision about how the office starts; it makes more sense after the
// user has anchored who they are than as the very first question.
const STEP_ORDER: readonly WizardStep[] = [
  'welcome',
  'identity',
  'templates',
  'team',
  'setup',
  'task',
] as const

// Each runtime has a display label, the binary name the broker's prereqs
// check looks for, a canonical install page to link to when missing, and
// — for the runtimes the broker can actually dispatch agents to — the
// provider id the broker expects on POST /config.
interface RuntimeSpec {
  label: string
  binary: string
  installUrl: string
  provider: 'claude-code' | 'codex' | null
}

const RUNTIMES: readonly RuntimeSpec[] = [
  { label: 'Claude Code', binary: 'claude', installUrl: 'https://claude.ai/code', provider: 'claude-code' },
  { label: 'Codex', binary: 'codex', installUrl: 'https://github.com/openai/codex', provider: 'codex' },
  { label: 'Cursor', binary: 'cursor', installUrl: 'https://cursor.com/', provider: null },
  { label: 'Windsurf', binary: 'windsurf', installUrl: 'https://codeium.com/windsurf', provider: null },
] as const

interface PrereqResult {
  name: string
  required: boolean
  found: boolean
  ok?: boolean
  version?: string
  install_url?: string
}

const API_KEY_FIELDS = [
  { key: 'ANTHROPIC_API_KEY', label: 'Anthropic', hint: 'Powers Claude-based agents' },
  { key: 'OPENAI_API_KEY', label: 'OpenAI', hint: 'Powers GPT-based agents' },
  { key: 'GOOGLE_API_KEY', label: 'Google', hint: 'Powers Gemini-based agents' },
] as const

type MemoryBackend = 'nex' | 'gbrain' | 'none'

const MEMORY_BACKEND_OPTIONS: ReadonlyArray<{
  value: MemoryBackend
  label: string
  hint: string
}> = [
  {
    value: 'nex',
    label: 'Nex',
    hint: 'Hosted memory graph. Ships with free tier. Needs NEX_API_KEY.',
  },
  {
    value: 'gbrain',
    label: 'GBrain',
    hint: 'Local graph over Postgres. Needs an LLM key for embeddings.',
  },
  {
    value: 'none',
    label: 'None',
    hint: 'Skip shared memory. Agents work with only per-turn context.',
  },
] as const

/* ═══════════════════════════════════════════
   Arrow icon reused across buttons
   ═══════════════════════════════════════════ */

function ArrowIcon() {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M5 12h14" />
      <path d="m12 5 7 7-7 7" />
    </svg>
  )
}

function CheckIcon() {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="3"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <polyline points="20 6 9 17 4 12" />
    </svg>
  )
}

/* ═══════════════════════════════════════════
   Sub-components
   ═══════════════════════════════════════════ */

function ProgressDots({ current }: { current: WizardStep }) {
  return (
    <div className="wizard-progress">
      {STEP_ORDER.map((step) => (
        <div
          key={step}
          className={`wizard-progress-dot ${step === current ? 'active' : 'inactive'}`}
        />
      ))}
    </div>
  )
}

/* ─── Step 1: Welcome ─── */

interface WelcomeStepProps {
  onNext: () => void
}

function WelcomeStep({ onNext }: WelcomeStepProps) {
  return (
    <div className="wizard-step">
      <div className="wizard-hero">
        <div className="wizard-eyebrow">
          <span className="status-dot active pulse" />
          Ready to set up
        </div>
        <h1 className="wizard-headline">{ONBOARDING_COPY.step1_headline}</h1>
        <p className="wizard-subhead">{ONBOARDING_COPY.step1_subhead}</p>
      </div>
      <div style={{ display: 'flex', justifyContent: 'center' }}>
        <button className="btn btn-primary btn-lg" onClick={onNext}>
          {ONBOARDING_COPY.step1_cta}
          <ArrowIcon />
        </button>
      </div>
    </div>
  )
}

/* ─── Step 2: Templates ─── */

interface TemplatesStepProps {
  templates: BlueprintTemplate[]
  loading: boolean
  selected: string | null
  onSelect: (id: string | null) => void
  onNext: () => void
  onBack: () => void
}

function TemplatesStep({
  templates,
  loading,
  selected,
  onSelect,
  onNext,
  onBack,
}: TemplatesStepProps) {
  return (
    <div className="wizard-step">
      <div className="wizard-hero">
        <div className="wizard-eyebrow">
          <span className="status-dot active pulse" />
          Pick the operating model the office starts with
        </div>
        <h1 className="wizard-headline">Choose a blueprint</h1>
        <p className="wizard-subhead">
          Blueprints set the team, stages, and workflows this office will run.
          Start from a preset or from scratch.
        </p>
      </div>

      <div className="wizard-panel">
        {loading ? (
          <div style={{ color: 'var(--text-tertiary)', fontSize: 13, textAlign: 'center', padding: 20 }}>
            Loading blueprints&hellip;
          </div>
        ) : (
          <div className="template-grid">
            <button
              className={`template-card ${selected === null ? 'selected' : ''}`}
              onClick={() => onSelect(null)}
              type="button"
            >
              <div className="template-card-emoji">&#x1F4DD;</div>
              <div className="template-card-name">From scratch</div>
              <div className="template-card-desc">
                Start with an empty office and add agents manually.
              </div>
            </button>
            {templates.map((t) => (
              <button
                key={t.id}
                className={`template-card ${selected === t.id ? 'selected' : ''}`}
                onClick={() => onSelect(t.id)}
                type="button"
              >
                {t.emoji && <div className="template-card-emoji">{t.emoji}</div>}
                <div className="template-card-name">{t.name}</div>
                <div className="template-card-desc">{t.description}</div>
              </button>
            ))}
          </div>
        )}
      </div>

      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onBack} type="button">
          Back
        </button>
        <button className="btn btn-primary" onClick={onNext} type="button">
          Review the team
          <ArrowIcon />
        </button>
      </div>
    </div>
  )
}

/* ─── Step 3: Identity ─── */

interface IdentityStepProps {
  company: string
  description: string
  priority: string
  onChangeCompany: (v: string) => void
  onChangeDescription: (v: string) => void
  onChangePriority: (v: string) => void
  onNext: () => void
  onBack: () => void
}

function IdentityStep({
  company,
  description,
  priority,
  onChangeCompany,
  onChangeDescription,
  onChangePriority,
  onNext,
  onBack,
}: IdentityStepProps) {
  const canContinue = company.trim().length > 0 && description.trim().length > 0

  return (
    <div className="wizard-step">
      <div className="wizard-panel">
        <p className="wizard-panel-title">Tell us about this office</p>
        <div className="form-group">
          <label className="label" htmlFor="wiz-company">
            Company or project name <span style={{ color: 'var(--red)' }}>*</span>
          </label>
          <input
            className="input"
            id="wiz-company"
            placeholder="Acme Operations, or your real project name"
            autoComplete="organization"
            value={company}
            onChange={(e) => onChangeCompany(e.target.value)}
          />
        </div>
        <div className="form-group">
          <label className="label" htmlFor="wiz-description">
            One-liner description <span style={{ color: 'var(--red)' }}>*</span>
          </label>
          <input
            className="input"
            id="wiz-description"
            placeholder="What real business or workflow should this office run?"
            value={description}
            onChange={(e) => onChangeDescription(e.target.value)}
          />
        </div>
        <div className="form-group">
          <label className="label" htmlFor="wiz-priority">
            Top priority right now
          </label>
          <input
            className="input"
            id="wiz-priority"
            placeholder="Win the first real customer loop"
            value={priority}
            onChange={(e) => onChangePriority(e.target.value)}
          />
        </div>
      </div>

      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onBack} type="button">
          Back
        </button>
        <button
          className="btn btn-primary"
          onClick={onNext}
          disabled={!canContinue}
          type="button"
        >
          Choose a blueprint
          <ArrowIcon />
        </button>
      </div>
    </div>
  )
}

/* ─── Step 4: Team Review ─── */

interface TeamStepProps {
  agents: BlueprintAgent[]
  onToggle: (slug: string) => void
  onNext: () => void
  onBack: () => void
}

function TeamStep({ agents, onToggle, onNext, onBack }: TeamStepProps) {
  return (
    <div className="wizard-step">
      <div className="wizard-panel">
        <p className="wizard-panel-title">Your team</p>
        <p style={{ fontSize: 12, color: 'var(--text-secondary)', margin: '-8px 0 12px 0' }}>
          These are the specialists your blueprint assembled. Toggle anyone you
          don&apos;t need.
        </p>

        {agents.length === 0 ? (
          <div className="wiz-team-empty">
            No teammates yet. Go back and pick a blueprint, or open the office and
            add agents from the team panel.
          </div>
        ) : (
          <div className="wiz-team-grid">
            {agents.map((a) => {
              // Lead agent is always included and cannot be unchecked here.
              // The backend also refuses to remove or disable any BuiltIn
              // member, so this is UI belt + server-side braces.
              const locked = a.built_in === true
              return (
                <button
                  key={a.slug}
                  className={`wiz-team-tile ${a.checked ? 'selected' : ''} ${locked ? 'locked' : ''}`}
                  onClick={() => !locked && onToggle(a.slug)}
                  type="button"
                  disabled={locked}
                  aria-disabled={locked}
                  title={locked ? 'Lead agent — always included' : undefined}
                >
                  <div className="wiz-team-check">
                    {a.checked && <CheckIcon />}
                  </div>
                  <div>
                    {a.emoji && (
                      <span style={{ marginRight: 6 }}>{a.emoji}</span>
                    )}
                    <span className="wiz-team-name">{a.name}</span>
                    {locked && (
                      <span className="wiz-team-lead-badge" aria-label="Lead">
                        Lead
                      </span>
                    )}
                    {a.role && <div className="wiz-team-role">{a.role}</div>}
                  </div>
                </button>
              )
            })}
          </div>
        )}
      </div>

      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onBack} type="button">
          Back
        </button>
        <button className="btn btn-primary" onClick={onNext} type="button">
          Continue
          <ArrowIcon />
        </button>
      </div>
    </div>
  )
}

/* ─── Step 5: Setup ─── */

interface SetupStepProps {
  prereqs: PrereqResult[]
  prereqsLoading: boolean
  runtimePriority: string[]
  onToggleRuntime: (label: string) => void
  onReorderRuntime: (label: string, direction: -1 | 1) => void
  apiKeys: Record<string, string>
  onChangeApiKey: (key: string, value: string) => void
  memoryBackend: MemoryBackend
  onChangeMemoryBackend: (value: MemoryBackend) => void
  onNext: () => void
  onBack: () => void
}

function detectedBinary(prereqs: PrereqResult[], binary: string): PrereqResult | undefined {
  return prereqs.find((p) => p.name === binary)
}

function SetupStep({
  prereqs,
  prereqsLoading,
  runtimePriority,
  onToggleRuntime,
  onReorderRuntime,
  apiKeys,
  onChangeApiKey,
  memoryBackend,
  onChangeMemoryBackend,
  onNext,
  onBack,
}: SetupStepProps) {
  // A runtime is usable only when its binary is actually present on PATH.
  // "Selected and installed" drives whether we can continue without keys.
  const hasInstalledSelection = runtimePriority.some((label) => {
    const spec = RUNTIMES.find((r) => r.label === label)
    if (!spec) return false
    const detection = detectedBinary(prereqs, spec.binary)
    return Boolean(detection?.found)
  })
  const hasAtLeastOneKey = Object.values(apiKeys).some((v) => v.trim().length > 0)
  const canContinue = hasInstalledSelection || hasAtLeastOneKey

  return (
    <div className="wizard-step">
      <div className="wizard-panel">
        <p className="wizard-panel-title">How should agents run?</p>
        <p style={{ fontSize: 12, color: 'var(--text-secondary)', margin: '-8px 0 12px 0' }}>
          Pick the CLIs you have installed. Each CLI&apos;s login handles its
          own provider auth, so no API keys are needed. Select multiple to set
          a fallback order — if the first one fails, agents fall through to
          the next.
        </p>

        {prereqsLoading ? (
          <div style={{ color: 'var(--text-tertiary)', fontSize: 13, padding: '8px 0' }}>
            Checking which CLIs are installed&hellip;
          </div>
        ) : (
          <div className="runtime-grid">
            {RUNTIMES.map((spec) => {
              const detection = detectedBinary(prereqs, spec.binary)
              const installed = Boolean(detection?.found)
              const priorityIdx = runtimePriority.indexOf(spec.label)
              const selected = priorityIdx >= 0
              const classes = [
                'runtime-tile',
                selected ? 'selected' : '',
                installed ? '' : 'disabled',
              ]
                .filter(Boolean)
                .join(' ')
              return (
                <button
                  key={spec.label}
                  className={classes}
                  onClick={() => {
                    if (!installed) return
                    onToggleRuntime(spec.label)
                  }}
                  type="button"
                  disabled={!installed}
                  aria-disabled={!installed}
                  aria-pressed={selected}
                  title={
                    installed
                      ? detection?.version
                        ? `${spec.label} — ${detection.version}`
                        : spec.label
                      : `${spec.label} — not installed`
                  }
                >
                  {selected && (
                    <span className="runtime-priority-badge" aria-label={`Priority ${priorityIdx + 1}`}>
                      {priorityIdx + 1}
                    </span>
                  )}
                  <div className="runtime-tile-head">
                    <span
                      className={`runtime-tile-status ${installed ? 'installed' : ''}`}
                      aria-hidden="true"
                    />
                    {spec.label}
                  </div>
                  <div className="runtime-tile-meta">
                    {installed ? (
                      detection?.version ? detection.version : 'Installed'
                    ) : (
                      <>
                        Not installed{' · '}
                        <a
                          className="runtime-tile-install-link"
                          href={spec.installUrl}
                          target="_blank"
                          rel="noopener noreferrer"
                          onClick={(e) => e.stopPropagation()}
                        >
                          install
                        </a>
                      </>
                    )}
                  </div>
                </button>
              )
            })}
          </div>
        )}

        {runtimePriority.length > 1 && (
          <div className="runtime-priority-controls">
            <p className="runtime-priority-title">Fallback order</p>
            <p className="runtime-priority-hint">
              Agents try these in order. Use the arrows to reorder.
            </p>
            {runtimePriority.map((label, idx) => (
              <div key={label} className="runtime-priority-row">
                <span className="runtime-priority-row-rank">#{idx + 1}</span>
                <span className="runtime-priority-row-label">{label}</span>
                <button
                  type="button"
                  className="runtime-priority-btn"
                  onClick={() => onReorderRuntime(label, -1)}
                  disabled={idx === 0}
                  aria-label={`Move ${label} up`}
                >
                  ↑
                </button>
                <button
                  type="button"
                  className="runtime-priority-btn"
                  onClick={() => onReorderRuntime(label, 1)}
                  disabled={idx === runtimePriority.length - 1}
                  aria-label={`Move ${label} down`}
                >
                  ↓
                </button>
                <button
                  type="button"
                  className="runtime-priority-btn"
                  onClick={() => onToggleRuntime(label)}
                  aria-label={`Remove ${label}`}
                >
                  ✕
                </button>
              </div>
            ))}
          </div>
        )}

        <div style={{ marginTop: 16, paddingTop: 16, borderTop: '1px solid var(--border)' }}>
          <p
            style={{
              fontSize: 13,
              fontWeight: 600,
              margin: '0 0 4px 0',
              color: 'var(--text-primary)',
            }}
          >
            API keys {hasInstalledSelection ? '(optional fallback)' : '(required)'}
          </p>
          <p style={{ fontSize: 12, color: 'var(--text-secondary)', margin: '0 0 12px 0' }}>
            {hasInstalledSelection
              ? 'Only used if every selected CLI fails. Leave blank to rely on the CLI login.'
              : 'No installed CLI selected. Add at least one key so agents can reason.'}
          </p>
          {API_KEY_FIELDS.map((field) => (
            <div className="key-row" key={field.key}>
              <div className="key-label-wrap">
                <span className="key-label">{field.label}</span>
                <span className="key-hint">{field.hint}</span>
              </div>
              <div className="key-input-wrap">
                <input
                  className="input"
                  type="password"
                  placeholder={field.key}
                  value={apiKeys[field.key] ?? ''}
                  onChange={(e) => onChangeApiKey(field.key, e.target.value)}
                  autoComplete="off"
                />
              </div>
            </div>
          ))}
        </div>
      </div>

      <div className="wizard-panel">
        <p className="wizard-panel-title">Organizational memory</p>
        <p style={{ fontSize: 12, color: 'var(--text-secondary)', margin: '-8px 0 12px 0' }}>
          Where agents store shared context, relationships, and learnings across
          sessions. You can change this later in Settings or via{' '}
          <code>--memory-backend</code>.
        </p>
        <div className="runtime-grid">
          {MEMORY_BACKEND_OPTIONS.map((opt) => (
            <button
              key={opt.value}
              className={`runtime-tile ${memoryBackend === opt.value ? 'selected' : ''}`}
              onClick={() => onChangeMemoryBackend(opt.value)}
              type="button"
              title={opt.hint}
            >
              <div style={{ fontWeight: 600 }}>{opt.label}</div>
              <div
                style={{
                  fontSize: 11,
                  color: 'var(--text-tertiary)',
                  marginTop: 4,
                  fontWeight: 400,
                }}
              >
                {opt.hint}
              </div>
            </button>
          ))}
        </div>
      </div>

      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onBack} type="button">
          Back
        </button>
        <button
          className="btn btn-primary"
          onClick={onNext}
          disabled={!canContinue}
          type="button"
        >
          {ONBOARDING_COPY.step2_cta}
          <ArrowIcon />
        </button>
      </div>
    </div>
  )
}

/* ─── Step 6: First Task ─── */

interface TaskStepProps {
  taskTemplates: TaskTemplate[]
  selectedTaskTemplate: string | null
  onSelectTaskTemplate: (id: string | null) => void
  taskText: string
  onChangeTaskText: (v: string) => void
  onSkip: () => void
  onSubmit: () => void
  onBack: () => void
  submitting: boolean
}

function TaskStep({
  taskTemplates,
  selectedTaskTemplate,
  onSelectTaskTemplate,
  taskText,
  onChangeTaskText,
  onSkip,
  onSubmit,
  onBack,
  submitting,
}: TaskStepProps) {
  return (
    <div className="wizard-step">
      <div>
        <h2
          style={{
            fontSize: 18,
            fontWeight: 700,
            textAlign: 'left',
            marginBottom: 4,
          }}
        >
          {ONBOARDING_COPY.step3_title}
        </h2>
      </div>

      {taskTemplates.length > 0 && (
        <div className="template-grid">
          {taskTemplates.map((t) => (
            <button
              key={t.id}
              className={`template-card ${selectedTaskTemplate === t.id ? 'selected' : ''}`}
              onClick={() => {
                onSelectTaskTemplate(selectedTaskTemplate === t.id ? null : t.id)
                if (t.prompt) onChangeTaskText(t.prompt)
              }}
              type="button"
            >
              {t.emoji && <div className="template-card-emoji">{t.emoji}</div>}
              <div className="template-card-name">{t.name}</div>
              <div className="template-card-desc">{t.description}</div>
            </button>
          ))}
        </div>
      )}

      <div>
        <label
          className="label"
          htmlFor="wiz-task-input"
          style={{ marginBottom: 8, display: 'block' }}
        >
          Or describe the first real business loop
        </label>
        <textarea
          className="task-textarea"
          id="wiz-task-input"
          placeholder={ONBOARDING_COPY.step3_placeholder}
          value={taskText}
          onChange={(e) => onChangeTaskText(e.target.value)}
        />
      </div>

      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onBack} type="button">
          Back
        </button>
        <div className="wizard-nav-right">
          <button
            className="task-skip"
            onClick={onSkip}
            disabled={submitting}
            type="button"
          >
            {ONBOARDING_COPY.step3_skip}
          </button>
          <button
            className="btn btn-primary"
            onClick={onSubmit}
            disabled={submitting || taskText.trim().length === 0}
            type="button"
          >
            {submitting ? 'Starting...' : ONBOARDING_COPY.step3_cta}
          </button>
        </div>
      </div>
    </div>
  )
}

/* ═══════════════════════════════════════════
   Main Wizard
   ═══════════════════════════════════════════ */

interface WizardProps {
  onComplete?: () => void
}

export function Wizard({ onComplete }: WizardProps) {
  const setOnboardingComplete = useAppStore((s) => s.setOnboardingComplete)

  // Navigation
  const [step, setStep] = useState<WizardStep>('welcome')

  // Step 2: templates
  const [blueprints, setBlueprints] = useState<BlueprintTemplate[]>([])
  const [blueprintsLoading, setBlueprintsLoading] = useState(true)
  const [selectedBlueprint, setSelectedBlueprint] = useState<string | null>(null)

  // Step 3: identity
  const [company, setCompany] = useState('')
  const [description, setDescription] = useState('')
  const [priority, setPriority] = useState('')

  // Step 4: team
  const [agents, setAgents] = useState<BlueprintAgent[]>([])

  // Step 5: setup
  const [prereqs, setPrereqs] = useState<PrereqResult[]>([])
  const [prereqsLoading, setPrereqsLoading] = useState(true)
  // Ordered list of runtime labels (matches RUNTIMES[].label). Position in
  // the array is the fallback priority. Initially empty — we auto-populate
  // with the first installed CLI once prereqs land so the happy path still
  // works with zero clicks.
  const [runtimePriority, setRuntimePriority] = useState<string[]>([])
  const [apiKeys, setApiKeys] = useState<Record<string, string>>({})
  const [memoryBackend, setMemoryBackend] = useState<MemoryBackend>('nex')

  // Step 6: first task
  const [taskTemplates, setTaskTemplates] = useState<TaskTemplate[]>([])
  const [selectedTaskTemplate, setSelectedTaskTemplate] = useState<string | null>(null)
  const [taskText, setTaskText] = useState('')
  const [submitting, setSubmitting] = useState(false)

  // Fetch blueprints on mount
  useEffect(() => {
    let cancelled = false
    setBlueprintsLoading(true)

    get<{ templates?: BlueprintTemplate[] }>('/onboarding/blueprints')
      .then((data) => {
        if (cancelled) return
        const tpls = data.templates ?? []
        setBlueprints(tpls)

        // Also extract task templates if present
        const tasks: TaskTemplate[] = []
        for (const t of tpls) {
          if ((t as unknown as Record<string, unknown>).tasks) {
            const arr = (t as unknown as Record<string, TaskTemplate[]>).tasks
            tasks.push(...arr)
          }
        }
        if (tasks.length > 0) {
          setTaskTemplates(tasks)
        }
      })
      .catch(() => {
        // Endpoint may not exist yet; continue with empty list
      })
      .finally(() => {
        if (!cancelled) setBlueprintsLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [])

  // Fetch prereqs on mount so the runtime picker shows which CLIs are
  // actually installed. Auto-select the first detected runtime so users
  // with a single CLI installed don't have to click.
  useEffect(() => {
    let cancelled = false
    setPrereqsLoading(true)

    get<{ prereqs?: PrereqResult[] } | PrereqResult[]>('/onboarding/prereqs')
      .then((data) => {
        if (cancelled) return
        const list = Array.isArray(data) ? data : data.prereqs ?? []
        setPrereqs(list)
        setRuntimePriority((current) => {
          if (current.length > 0) return current
          const firstInstalled = RUNTIMES.find((spec) => {
            const det = list.find((p) => p.name === spec.binary)
            return Boolean(det?.found)
          })
          return firstInstalled ? [firstInstalled.label] : []
        })
      })
      .catch(() => {
        // Broker may not expose the endpoint yet; leave prereqs empty and
        // the user can still add API keys to proceed.
      })
      .finally(() => {
        if (!cancelled) setPrereqsLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [])

  const toggleRuntime = useCallback((label: string) => {
    setRuntimePriority((prev) => {
      if (prev.includes(label)) return prev.filter((l) => l !== label)
      return [...prev, label]
    })
  }, [])

  const reorderRuntime = useCallback((label: string, direction: -1 | 1) => {
    setRuntimePriority((prev) => {
      const idx = prev.indexOf(label)
      if (idx < 0) return prev
      const next = idx + direction
      if (next < 0 || next >= prev.length) return prev
      const out = [...prev]
      const [item] = out.splice(idx, 1)
      out.splice(next, 0, item)
      return out
    })
  }, [])

  // When a blueprint is selected, populate agents
  useEffect(() => {
    if (selectedBlueprint === null) {
      setAgents([])
      return
    }
    const bp = blueprints.find((b) => b.id === selectedBlueprint)
    if (bp?.agents) {
      setAgents(
        bp.agents.map((a) => ({
          ...a,
          checked: a.checked !== false,
        })),
      )
    } else {
      setAgents([])
    }
  }, [selectedBlueprint, blueprints])

  // Navigation helpers
  const goTo = useCallback((target: WizardStep) => {
    setStep(target)
  }, [])

  const nextStep = useCallback(() => {
    const idx = STEP_ORDER.indexOf(step)
    if (idx < STEP_ORDER.length - 1) {
      setStep(STEP_ORDER[idx + 1])
    }
  }, [step])

  const prevStep = useCallback(() => {
    const idx = STEP_ORDER.indexOf(step)
    if (idx > 0) {
      setStep(STEP_ORDER[idx - 1])
    }
  }, [step])

  // Toggle agent selection. The lead agent (built_in) is locked: TeamStep
  // disables its button, and this guard prevents any programmatic path
  // (keyboard, devtools, future bulk toggle) from unchecking it.
  const toggleAgent = useCallback((slug: string) => {
    setAgents((prev) =>
      prev.map((a) => {
        if (a.slug !== slug) return a
        if (a.built_in === true) return a
        return { ...a, checked: !a.checked }
      }),
    )
  }, [])

  // API key handler
  const handleApiKeyChange = useCallback((key: string, value: string) => {
    setApiKeys((prev) => ({ ...prev, [key]: value }))
  }, [])

  // Complete onboarding
  const finishOnboarding = useCallback(
    async (skipTask: boolean) => {
      setSubmitting(true)
      try {
        // Translate UI labels to the provider ids the broker validates. Only
        // labels that map to a supported provider ("claude-code", "codex")
        // are persisted — aspirational runtimes (Cursor, Windsurf) are shown
        // in the UI but can't yet be dispatched, so we drop them from the
        // priority list we send to the server.
        const providerPriority = runtimePriority
          .map((label) => RUNTIMES.find((r) => r.label === label)?.provider)
          .filter((p): p is 'claude-code' | 'codex' => p != null)

        // Persist memory backend + LLM provider choice + priority fallback
        // list so the broker reads them on next launch. Fire-and-forget —
        // failures here should not block completing onboarding.
        post('/config', { memory_backend: memoryBackend }).catch(() => {})
        if (providerPriority.length > 0) {
          post('/config', {
            llm_provider: providerPriority[0],
            llm_provider_priority: providerPriority,
          }).catch(() => {})
        }

        // Primary runtime label for the onboarding payload (best-effort;
        // the broker only acts on {task, skip_task} today, but the extra
        // fields are forward-compatible).
        const primaryRuntime = runtimePriority[0] ?? ''

        await post('/onboarding/complete', {
          company,
          description,
          priority,
          runtime: primaryRuntime,
          runtime_priority: runtimePriority,
          memory_backend: memoryBackend,
          blueprint: selectedBlueprint,
          agents: agents.filter((a) => a.checked).map((a) => a.slug),
          api_keys: apiKeys,
          task: skipTask ? '' : taskText.trim(),
          skip_task: skipTask,
        })
      } catch {
        // Best-effort — the broker may not support this endpoint yet.
        // Continue to mark onboarding complete locally.
      }

      setOnboardingComplete(true)
      onComplete?.()
    },
    [
      company,
      description,
      priority,
      runtimePriority,
      memoryBackend,
      selectedBlueprint,
      agents,
      apiKeys,
      taskText,
      setOnboardingComplete,
      onComplete,
    ],
  )

  return (
    <div className="wizard-container">
      <div className="wizard-body">
        <ProgressDots current={step} />

        {step === 'welcome' && (
          <WelcomeStep onNext={() => goTo('identity')} />
        )}

        {step === 'templates' && (
          <TemplatesStep
            templates={blueprints}
            loading={blueprintsLoading}
            selected={selectedBlueprint}
            onSelect={setSelectedBlueprint}
            onNext={nextStep}
            onBack={prevStep}
          />
        )}

        {step === 'identity' && (
          <IdentityStep
            company={company}
            description={description}
            priority={priority}
            onChangeCompany={setCompany}
            onChangeDescription={setDescription}
            onChangePriority={setPriority}
            onNext={nextStep}
            onBack={prevStep}
          />
        )}

        {step === 'team' && (
          <TeamStep
            agents={agents}
            onToggle={toggleAgent}
            onNext={nextStep}
            onBack={prevStep}
          />
        )}

        {step === 'setup' && (
          <SetupStep
            prereqs={prereqs}
            prereqsLoading={prereqsLoading}
            runtimePriority={runtimePriority}
            onToggleRuntime={toggleRuntime}
            onReorderRuntime={reorderRuntime}
            apiKeys={apiKeys}
            onChangeApiKey={handleApiKeyChange}
            memoryBackend={memoryBackend}
            onChangeMemoryBackend={setMemoryBackend}
            onNext={nextStep}
            onBack={prevStep}
          />
        )}

        {step === 'task' && (
          <TaskStep
            taskTemplates={taskTemplates}
            selectedTaskTemplate={selectedTaskTemplate}
            onSelectTaskTemplate={setSelectedTaskTemplate}
            taskText={taskText}
            onChangeTaskText={setTaskText}
            onSkip={() => finishOnboarding(true)}
            onSubmit={() => finishOnboarding(false)}
            onBack={prevStep}
            submitting={submitting}
          />
        )}
      </div>
    </div>
  )
}
