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

const RUNTIME_OPTIONS = ['Claude Code', 'Codex', 'Cursor', 'Windsurf', 'Other'] as const

// Map UI runtime labels to the provider enum the broker validates on POST /config.
// Labels without a backend equivalent return null and are skipped on persist —
// the broker keeps its existing default (claude-code) until the user picks a
// supported runtime. Keeping this local to the wizard so the UI can list
// aspirational runtimes without lying about what will actually run.
const RUNTIME_LABEL_TO_PROVIDER: Record<string, 'claude-code' | 'codex' | null> = {
  'Claude Code': 'claude-code',
  Codex: 'codex',
  Cursor: null,
  Windsurf: null,
  Other: null,
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
            {agents.map((a) => (
              <button
                key={a.slug}
                className={`wiz-team-tile ${a.checked ? 'selected' : ''}`}
                onClick={() => onToggle(a.slug)}
                type="button"
              >
                <div className="wiz-team-check">
                  {a.checked && <CheckIcon />}
                </div>
                <div>
                  {a.emoji && (
                    <span style={{ marginRight: 6 }}>{a.emoji}</span>
                  )}
                  <span className="wiz-team-name">{a.name}</span>
                  {a.role && <div className="wiz-team-role">{a.role}</div>}
                </div>
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
          Continue
          <ArrowIcon />
        </button>
      </div>
    </div>
  )
}

/* ─── Step 5: Setup ─── */

interface SetupStepProps {
  runtime: string
  onChangeRuntime: (v: string) => void
  apiKeys: Record<string, string>
  onChangeApiKey: (key: string, value: string) => void
  memoryBackend: MemoryBackend
  onChangeMemoryBackend: (value: MemoryBackend) => void
  onNext: () => void
  onBack: () => void
}

function SetupStep({
  runtime,
  onChangeRuntime,
  apiKeys,
  onChangeApiKey,
  memoryBackend,
  onChangeMemoryBackend,
  onNext,
  onBack,
}: SetupStepProps) {
  const hasAtLeastOneKey = Object.values(apiKeys).some((v) => v.trim().length > 0)

  return (
    <div className="wizard-step">
      <div className="wizard-panel">
        <p className="wizard-panel-title">{ONBOARDING_COPY.step2_prereqs_title}</p>
        <p style={{ fontSize: 12, color: 'var(--text-secondary)', margin: '-8px 0 12px 0' }}>
          The CLI that runs your agents. Pick the one you already use.
        </p>
        <div className="runtime-grid">
          {RUNTIME_OPTIONS.map((opt) => (
            <button
              key={opt}
              className={`runtime-tile ${runtime === opt ? 'selected' : ''}`}
              onClick={() => onChangeRuntime(opt)}
              type="button"
            >
              {opt}
            </button>
          ))}
        </div>
      </div>

      <div className="wizard-panel">
        <p className="wizard-panel-title">{ONBOARDING_COPY.step2_keys_title}</p>
        <p style={{ fontSize: 12, color: 'var(--text-secondary)', margin: '-8px 0 12px 0' }}>
          At least one API key is needed so agents can reason. You can add more
          later.
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
          disabled={!hasAtLeastOneKey}
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
  const [runtime, setRuntime] = useState<string>(RUNTIME_OPTIONS[0])
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

  // Toggle agent selection
  const toggleAgent = useCallback((slug: string) => {
    setAgents((prev) =>
      prev.map((a) =>
        a.slug === slug ? { ...a, checked: !a.checked } : a,
      ),
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
        // Persist memory backend + LLM provider selection first so the broker
        // reads them on next launch. Fire-and-forget — a failure here should
        // not block completing onboarding. Unsupported runtime labels (Cursor,
        // Windsurf, Other) are skipped; only claude-code and codex are wired.
        post('/config', { memory_backend: memoryBackend }).catch(() => {})
        const llmProvider = RUNTIME_LABEL_TO_PROVIDER[runtime]
        if (llmProvider) {
          post('/config', { llm_provider: llmProvider }).catch(() => {})
        }

        // Post the onboarding payload. Body shape is historical; the broker
        // currently only acts on {task, skip_task} but the extra fields are
        // forward-compatible.
        await post('/onboarding/complete', {
          company,
          description,
          priority,
          runtime,
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
      runtime,
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
            runtime={runtime}
            onChangeRuntime={setRuntime}
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
