import { useState, useEffect, useCallback } from 'react'
import { useAppStore } from '../../stores/app'
import { get, post } from '../../api/client'
import { ONBOARDING_COPY } from '../../lib/constants'
import { Kbd } from '../ui/Kbd'
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

type WizardStep = 'welcome' | 'templates' | 'identity' | 'team' | 'setup' | 'task' | 'ready'

// Step order: company info before blueprint. The blueprint picker is a
// decision about how the office starts; it makes more sense after the
// user has anchored who they are than as the very first question.
// `ready` is the final-step readiness summary matching the TUI's InitDone
// phase (see internal/tui/init_flow.go readinessChecks()) — shows the user
// exactly what's configured before we submit.
const STEP_ORDER: readonly WizardStep[] = [
  'welcome',
  'identity',
  'templates',
  'team',
  'setup',
  'task',
  'ready',
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

// "Start from scratch" starter roster. Mirrors scratchFoundingTeamBlueprint
// in internal/team/broker_onboarding.go — the broker seeds these exact slugs
// when the wizard POSTs blueprint:null. Kept in sync manually; backend is the
// source of truth, this is just the Team-step preview so users don't see an
// empty roster before confirming.
const SCRATCH_FOUNDING_TEAM: readonly BlueprintAgent[] = [
  { slug: 'ceo', name: 'CEO', role: 'lead', checked: true, built_in: true },
  { slug: 'gtm-lead', name: 'GTM Lead', role: 'go-to-market', checked: true },
  { slug: 'founding-engineer', name: 'Founding Engineer', role: 'engineering', checked: true },
  { slug: 'pm', name: 'Product Manager', role: 'product', checked: true },
  { slug: 'designer', name: 'Designer', role: 'design', checked: true },
]

// Display overrides for blueprints. Backend names/descriptions are long-form
// ("Bookkeeping and Invoicing Service", "Template for a bookkeeping operation
// that handles recurring books..."). For the onboarding picker we want short,
// scannable copy and visible categorization. Overrides are keyed by blueprint
// id (see templates/operations/*/blueprint.yaml). If a blueprint isn't in the
// map we fall back to the backend name + description, so new blueprints still
// render without frontend changes.
type BlueprintCategoryKey = 'services' | 'media' | 'product'

interface BlueprintDisplay {
  category: BlueprintCategoryKey
  shortDescription: string
  icon: string
}

const BLUEPRINT_CATEGORIES: ReadonlyArray<{
  key: BlueprintCategoryKey
  label: string
  hint: string
}> = [
  { key: 'services', label: 'Services', hint: 'Client work, done by your office' },
  { key: 'media', label: 'Media & Community', hint: 'Content or community as the business' },
  { key: 'product', label: 'Products', hint: 'Software you build and sell' },
] as const

const BLUEPRINT_DISPLAY: Record<string, BlueprintDisplay> = {
  'bookkeeping-invoicing-service': {
    category: 'services',
    shortDescription: 'Books · invoices · monthly close',
    icon: '📊',
  },
  'local-business-ai-package': {
    category: 'services',
    shortDescription: 'Intake · booking · follow-up',
    icon: '🏪',
  },
  'multi-agent-workflow-consulting': {
    category: 'services',
    shortDescription: 'Client engagements · workflow delivery',
    icon: '💼',
  },
  'niche-crm': {
    category: 'product',
    shortDescription: 'Build & launch a focused CRM',
    icon: '🎯',
  },
  'paid-discord-community': {
    category: 'media',
    shortDescription: 'Moderation · onboarding · engagement',
    icon: '💬',
  },
  'youtube-factory': {
    category: 'media',
    shortDescription: 'Script · film · publish · analyze',
    icon: '📹',
  },
}

const API_KEY_FIELDS = [
  { key: 'ANTHROPIC_API_KEY', label: 'Anthropic', hint: 'Powers Claude-based agents' },
  { key: 'OPENAI_API_KEY', label: 'OpenAI', hint: 'Powers GPT-based agents' },
  { key: 'GOOGLE_API_KEY', label: 'Google', hint: 'Powers Gemini-based agents' },
] as const

type MemoryBackend = 'markdown' | 'nex' | 'gbrain' | 'none'

const MEMORY_BACKEND_OPTIONS: ReadonlyArray<{
  value: MemoryBackend
  label: string
  hint: string
}> = [
  {
    value: 'markdown',
    label: 'Markdown (default)',
    hint: 'Git-native team wiki in ~/.wuphf/wiki. File-over-app, `git clone`-able. No API key needed.',
  },
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

/**
 * Inline "↵ Enter" hint for primary CTAs. Purely decorative — the real
 * Enter handling lives at the Wizard level so it works from anywhere on
 * the step, not just when the button has focus.
 */
function EnterHint({ label = 'Enter' }: { label?: string }) {
  return (
    <span className="kbd-hint" aria-hidden="true">
      <Kbd size="sm" variant="inverse">↵</Kbd>
      {label}
    </span>
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
        <button className="btn btn-primary" onClick={onNext}>
          {ONBOARDING_COPY.step1_cta}
          <ArrowIcon />
          <EnterHint />
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
  // Group templates by display category. Unknown blueprint ids (not in the
  // frontend catalog) land in a catch-all "Other" bucket so new backend
  // templates still render, just without the short-description and icon
  // treatment.
  const grouped = new Map<BlueprintCategoryKey | 'other', BlueprintTemplate[]>()
  for (const t of templates) {
    const display = BLUEPRINT_DISPLAY[t.id]
    const key: BlueprintCategoryKey | 'other' = display?.category ?? 'other'
    const list = grouped.get(key) ?? []
    list.push(t)
    grouped.set(key, list)
  }

  const renderTile = (t: BlueprintTemplate) => {
    const display = BLUEPRINT_DISPLAY[t.id]
    const icon = display?.icon ?? t.emoji
    const desc = display?.shortDescription ?? t.description
    return (
      <button
        key={t.id}
        className={`template-card ${selected === t.id ? 'selected' : ''}`}
        onClick={() => onSelect(t.id)}
        type="button"
      >
        {icon && <div className="template-card-emoji">{icon}</div>}
        <div className="template-card-name">{t.name}</div>
        <div className="template-card-desc">{desc}</div>
      </button>
    )
  }

  return (
    <div className="wizard-step">
      <div className="wizard-hero">
        <div className="wizard-eyebrow">
          <span className="status-dot active pulse" />
          Start with a preset, or build from scratch
        </div>
        <h1 className="wizard-headline">What should your office run?</h1>
        <p className="wizard-subhead">
          Pick the shape of work. We&apos;ll assemble the team, channels, and
          first tasks around it. You can change anything later.
        </p>
      </div>

      {loading ? (
        <div className="wizard-panel">
          <div style={{ color: 'var(--text-tertiary)', fontSize: 13, textAlign: 'center', padding: 20 }}>
            Loading blueprints&hellip;
          </div>
        </div>
      ) : (
        <>
          {BLUEPRINT_CATEGORIES.map((cat) => {
            const items = grouped.get(cat.key) ?? []
            if (items.length === 0) return null
            return (
              <div key={cat.key} className="wizard-panel template-group">
                <div className="template-group-head">
                  <p className="template-group-label">{cat.label}</p>
                  <p className="template-group-hint">{cat.hint}</p>
                </div>
                <div className="template-grid">{items.map(renderTile)}</div>
              </div>
            )
          })}

          {(grouped.get('other') ?? []).length > 0 && (
            <div className="wizard-panel template-group">
              <div className="template-group-head">
                <p className="template-group-label">Other</p>
              </div>
              <div className="template-grid">
                {(grouped.get('other') ?? []).map(renderTile)}
              </div>
            </div>
          )}

          <div className="template-from-scratch">
            <button
              className={`template-from-scratch-btn ${selected === null ? 'selected' : ''}`}
              onClick={() => onSelect(null)}
              type="button"
            >
              <span className="template-from-scratch-icon">+</span>
              Start from scratch
              <span className="template-from-scratch-sub">
                5-person founding team: CEO, GTM Lead, Founding Engineer, PM, Designer
              </span>
            </button>
          </div>
        </>
      )}

      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onBack} type="button">
          Back
        </button>
        <button className="btn btn-primary" onClick={onNext} type="button">
          Review the team
          <ArrowIcon />
          <EnterHint />
        </button>
      </div>
    </div>
  )
}

/* ─── Step 3: Identity ─── */

// NexSignupStatus tracks the state of the optional in-wizard Nex
// registration sub-flow. 'hidden' means the user hasn't opened the
// affordance yet; 'open' means they're entering their email; 'submitting'
// is the in-flight POST to /nex/register; 'ok' shows a green "sent, check
// your inbox" hint; 'fallback' flips to the external-link version when
// nex-cli is not installed (the broker responds 502 with ErrNotInstalled).
type NexSignupStatus = 'hidden' | 'open' | 'submitting' | 'ok' | 'fallback'

interface IdentityStepProps {
  company: string
  description: string
  priority: string
  nexEmail: string
  nexSignupStatus: NexSignupStatus
  nexSignupError: string
  onChangeCompany: (v: string) => void
  onChangeDescription: (v: string) => void
  onChangePriority: (v: string) => void
  onChangeNexEmail: (v: string) => void
  onSubmitNexSignup: () => void
  onOpenNexSignup: () => void
  onNext: () => void
  onBack: () => void
}

function IdentityStep({
  company,
  description,
  priority,
  nexEmail,
  nexSignupStatus,
  nexSignupError,
  onChangeCompany,
  onChangeDescription,
  onChangePriority,
  onChangeNexEmail,
  onSubmitNexSignup,
  onOpenNexSignup,
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

      {nexSignupStatus === 'hidden' ? (
        <div className="wiz-nex-trigger">
          <button
            type="button"
            className="wiz-nex-trigger-link"
            onClick={onOpenNexSignup}
          >
            Don&apos;t have a Nex account? Sign up here.
          </button>
        </div>
      ) : (
        <NexSignupPanel
          email={nexEmail}
          status={nexSignupStatus}
          error={nexSignupError}
          onChangeEmail={onChangeNexEmail}
          onSubmit={onSubmitNexSignup}
        />
      )}

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
          <EnterHint />
        </button>
      </div>
    </div>
  )
}

/* ─── Nex signup affordance (rendered inside IdentityStep) ─── */

interface NexSignupPanelProps {
  email: string
  status: NexSignupStatus
  error: string
  onChangeEmail: (v: string) => void
  onSubmit: () => void
}

// NexSignupPanel is the optional "don't have a Nex account yet?"
// affordance. It's compact by default (one-line link) so users with a key
// already aren't distracted. The primary path calls /nex/register on the
// broker, which shells out to `nex-cli setup <email>`. If nex-cli isn't
// installed, the broker returns 502 with ErrNotInstalled and we flip to
// the external-link fallback (open nex.ai/register + paste key on Setup
// step). Matches the TUI's InitNexRegister phase in init_flow.go.
function NexSignupPanel({
  email,
  status,
  error,
  onChangeEmail,
  onSubmit,
}: NexSignupPanelProps) {
  return (
    <div className="wizard-panel wiz-nex-signup">
      <p className="wizard-panel-title">Sign up for Nex (optional)</p>
      <p style={{ fontSize: 12, color: 'var(--text-secondary)', margin: '-8px 0 12px 0' }}>
        {status === 'fallback'
          ? 'nex-cli is not installed on this machine. Register in your browser, then paste the key on the Setup step.'
          : 'Register an email to get a free Nex API key. Powers shared memory, entity briefs, and integrations. You can also paste an existing key on the Setup step.'}
      </p>

      {status === 'fallback' ? (
        <a
          className="btn btn-secondary"
          href="https://nex.ai/register"
          target="_blank"
          rel="noopener noreferrer"
        >
          Open nex.ai/register
          <ArrowIcon />
        </a>
      ) : status === 'ok' ? (
        <p className="wiz-nex-ok" role="status">
          Check your inbox at {email} for the Nex API key, then paste it on the
          Setup step.
        </p>
      ) : (
        <div className="form-group" style={{ margin: 0 }}>
          <label className="label" htmlFor="wiz-nex-email">
            Email
          </label>
          <div style={{ display: 'flex', gap: 8 }}>
            <input
              className="input"
              id="wiz-nex-email"
              type="email"
              placeholder="you@example.com"
              value={email}
              onChange={(e) => onChangeEmail(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && status !== 'submitting' && email.trim().length > 0) {
                  e.preventDefault()
                  e.stopPropagation()
                  onSubmit()
                }
              }}
              disabled={status === 'submitting'}
              style={{ flex: 1 }}
            />
            <button
              className="btn btn-primary"
              type="button"
              onClick={onSubmit}
              disabled={status === 'submitting' || email.trim().length === 0}
            >
              {status === 'submitting' ? 'Registering...' : 'Register'}
            </button>
          </div>
          {error && (
            <p style={{ color: 'var(--red)', fontSize: 12, marginTop: 6 }} role="alert">
              {error}
            </p>
          )}
        </div>
      )}
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
          <EnterHint />
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
  nexApiKey: string
  onChangeNexApiKey: (v: string) => void
  gbrainOpenAIKey: string
  onChangeGBrainOpenAIKey: (v: string) => void
  gbrainAnthropicKey: string
  onChangeGBrainAnthropicKey: (v: string) => void
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
  nexApiKey,
  onChangeNexApiKey,
  gbrainOpenAIKey,
  onChangeGBrainOpenAIKey,
  gbrainAnthropicKey,
  onChangeGBrainAnthropicKey,
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
  // GBrain requires an OpenAI key to function — the TUI gates on this in
  // InitGBrainOpenAIKey (see internal/tui/init_flow.go:215). Mirror the
  // gate here so the wizard doesn't let users commit an unusable config.
  const gbrainSelected = memoryBackend === 'gbrain'
  const gbrainOpenAIMissing = gbrainSelected && gbrainOpenAIKey.trim().length === 0
  const canContinue = (hasInstalledSelection || hasAtLeastOneKey) && !gbrainOpenAIMissing

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
              color: 'var(--text)',
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

        {gbrainSelected && (
          <div className="wiz-backend-keys">
            <p className="wiz-backend-keys-title">GBrain keys</p>
            <p className="wiz-backend-keys-hint">
              GBrain uses OpenAI for embeddings (required) and optionally
              Anthropic for reasoning.
            </p>
            <div className="form-group">
              <label className="label" htmlFor="wiz-gbrain-openai">
                OpenAI API key{' '}
                <span style={{ color: 'var(--red)' }}>*</span>
              </label>
              <input
                className="input"
                id="wiz-gbrain-openai"
                type="password"
                placeholder="sk-..."
                value={gbrainOpenAIKey}
                onChange={(e) => onChangeGBrainOpenAIKey(e.target.value)}
                autoComplete="off"
              />
              {gbrainOpenAIMissing && (
                <p style={{ color: 'var(--red)', fontSize: 11, marginTop: 4 }}>
                  Required: GBrain can&apos;t create embeddings without an OpenAI
                  key.
                </p>
              )}
            </div>
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label className="label" htmlFor="wiz-gbrain-anthropic">
                Anthropic API key{' '}
                <span style={{ fontSize: 11, color: 'var(--text-tertiary)' }}>
                  (optional)
                </span>
              </label>
              <input
                className="input"
                id="wiz-gbrain-anthropic"
                type="password"
                placeholder="sk-ant-..."
                value={gbrainAnthropicKey}
                onChange={(e) => onChangeGBrainAnthropicKey(e.target.value)}
                autoComplete="off"
              />
            </div>
          </div>
        )}
      </div>

      <div className="wizard-panel">
        <p className="wizard-panel-title">Nex API key</p>
        <p style={{ fontSize: 12, color: 'var(--text-secondary)', margin: '-8px 0 12px 0' }}>
          Unlocks hosted memory, entity briefs, and managed integrations. You
          can skip this and paste later from Settings. Don&apos;t have one?
          Sign up on the Identity step above.
        </p>
        <div className="form-group" style={{ marginBottom: 0 }}>
          <label className="label" htmlFor="wiz-nex-api-key">
            Nex API key{' '}
            <span style={{ fontSize: 11, color: 'var(--text-tertiary)' }}>
              (optional, paste if you have one)
            </span>
          </label>
          <input
            className="input"
            id="wiz-nex-api-key"
            type="password"
            placeholder="nex-..."
            value={nexApiKey}
            onChange={(e) => onChangeNexApiKey(e.target.value)}
            autoComplete="off"
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
          {ONBOARDING_COPY.step2_cta}
          <ArrowIcon />
          <EnterHint />
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
  onNext: () => void
  onBack: () => void
}

function TaskStep({
  taskTemplates,
  selectedTaskTemplate,
  onSelectTaskTemplate,
  taskText,
  onChangeTaskText,
  onNext,
  onBack,
}: TaskStepProps) {
  return (
    <div className="wizard-step">
      <div className="wizard-hero">
        <h1 className="wizard-headline" style={{ fontSize: 28 }}>
          {ONBOARDING_COPY.step3_title}
        </h1>
        {taskTemplates.length > 0 && (
          <p className="wizard-subhead">
            Type your own first task, or pick from the blueprint&apos;s
            suggested sequence below.
          </p>
        )}
      </div>

      <div>
        <textarea
          className="task-textarea task-textarea-primary"
          id="wiz-task-input"
          placeholder={ONBOARDING_COPY.step3_placeholder}
          value={taskText}
          onChange={(e) => onChangeTaskText(e.target.value)}
          autoFocus
        />
        <p className="task-textarea-hint">
          <Kbd size="sm">↵</Kbd> new line ·{' '}
          <Kbd size="sm">{cmdEnterLabel().split(' ')[0]}</Kbd>
          <Kbd size="sm">↵</Kbd> review setup
        </p>
      </div>

      {taskTemplates.length > 0 && (
        <div className="task-suggestions">
          <p className="task-suggestions-label">
            Suggested sequence for this blueprint
          </p>
          <div className="task-suggestions-list">
            {taskTemplates.map((t, idx) => {
              const isSelected = selectedTaskTemplate === t.id
              return (
                <button
                  key={t.id}
                  className={`task-suggestion ${isSelected ? 'selected' : ''}`}
                  onClick={() => {
                    const nextId = isSelected ? null : t.id
                    onSelectTaskTemplate(nextId)
                    if (nextId) {
                      onChangeTaskText(t.prompt ?? t.name)
                    }
                  }}
                  type="button"
                >
                  <span className="task-suggestion-num">{idx + 1}</span>
                  <span className="task-suggestion-name">{t.name}</span>
                </button>
              )
            })}
          </div>
        </div>
      )}

      <div className="wizard-nav">
        <button className="btn btn-ghost" onClick={onBack} type="button">
          Back
        </button>
        <button className="btn btn-primary" onClick={onNext} type="button">
          Review setup
          <ArrowIcon />
          <EnterHint label={cmdEnterLabel()} />
        </button>
      </div>
    </div>
  )
}

/**
 * On the task step, Enter inserts a newline inside the textarea and
 * ⌘/Ctrl+Enter advances. Compute the label lazily so non-Mac browsers
 * see "Ctrl+Enter" instead of the glyph.
 */
function cmdEnterLabel(): string {
  const mac = typeof navigator !== 'undefined'
    && /Mac|iPod|iPhone|iPad/.test(navigator.platform)
  return mac ? '⌘ Enter' : 'Ctrl Enter'
}

/* ─── Step 7: Readiness Summary ─── */

// ReadinessStatus mirrors the TUI's three-state readiness color mapping
// (see internal/tui/init_flow.go readinessStatusColor): 'ready' = green
// check, 'next' = blue warning (follow-up needed), 'missing' = red.
type ReadinessStatus = 'ready' | 'next' | 'missing'

interface ReadinessCheck {
  label: string
  status: ReadinessStatus
  detail: string
}

interface ReadyStepProps {
  checks: ReadinessCheck[]
  taskText: string
  submitting: boolean
  onSkip: () => void
  onSubmit: () => void
  onBack: () => void
}

// ReadyStep is the six-item final review matching the TUI's InitDone
// readinessChecks() view. It's honest: a missing Nex key is not papered
// over, and GBrain+no-OpenAI-key would show a red "missing" row (though
// the Setup step blocks continuing in that case, so users shouldn't get
// here with it).
function ReadyStep({
  checks,
  taskText,
  submitting,
  onSkip,
  onSubmit,
  onBack,
}: ReadyStepProps) {
  return (
    <div className="wizard-step">
      <div className="wizard-hero">
        <h1 className="wizard-headline" style={{ fontSize: 28 }}>
          You&apos;re set
        </h1>
        <p className="wizard-subhead">
          Here&apos;s what&apos;s configured. Anything with a{' '}
          <span className="readiness-glyph-inline missing">!</span> or{' '}
          <span className="readiness-glyph-inline next">—</span> can be fixed
          later from Settings.
        </p>
      </div>

      <div className="wizard-panel readiness-panel">
        <ul className="readiness-list">
          {checks.map((check) => (
            <li key={check.label} className="readiness-item">
              <span
                className={`readiness-glyph ${check.status}`}
                aria-hidden="true"
              >
                {check.status === 'ready'
                  ? '✓'
                  : check.status === 'next'
                    ? '—'
                    : '!'}
              </span>
              <div className="readiness-body">
                <div className="readiness-label">{check.label}</div>
                <div className="readiness-detail">{check.detail}</div>
              </div>
            </li>
          ))}
        </ul>
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
            {!submitting && taskText.trim().length > 0 && <EnterHint />}
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
  // Optional in-wizard Nex registration. Mirrors the TUI's InitNexRegister
  // phase — we POST /nex/register which shells out to `nex-cli setup <email>`.
  // If nex-cli isn't installed we flip to `fallback` (external link to
  // nex.ai/register, key pasted on the Setup step).
  const [nexEmail, setNexEmail] = useState('')
  const [nexSignupStatus, setNexSignupStatus] = useState<NexSignupStatus>('hidden')
  const [nexSignupError, setNexSignupError] = useState('')

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
  // Matches MEMORY_BACKEND_OPTIONS[0] (the "Markdown (default)" tile) and the
  // server-side `config.ResolveMemoryBackend` default. Shipping 'nex' here
  // contradicted the label and meant a user who clicked through got a
  // different backend than the one marked default.
  const [memoryBackend, setMemoryBackend] = useState<MemoryBackend>('markdown')
  // Nex API key (maps to `api_key` on /config). Parity with TUI's InitAPIKey
  // phase. Kept separate from `apiKeys` because the latter is the per-runtime
  // fallback set (Anthropic/OpenAI/Google) while this one unlocks hosted
  // memory and managed integrations. Empty = skipped, not an error.
  const [nexApiKey, setNexApiKey] = useState('')
  // GBrain-specific key inputs. Only rendered when memoryBackend === 'gbrain'.
  // Mirrors the TUI's InitGBrainOpenAIKey (required) + InitGBrainAnthropKey
  // (optional) phases.
  const [gbrainOpenAIKey, setGbrainOpenAIKey] = useState('')
  const [gbrainAnthropicKey, setGbrainAnthropicKey] = useState('')

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

  // When a blueprint is selected, populate agents AND first tasks from that
  // blueprint only. Previously we flattened tasks across every blueprint, so
  // the task step showed ~26 tiles of unrelated work — including tasks from
  // blueprints the user never picked.
  useEffect(() => {
    if (selectedBlueprint === null) {
      // "Start from scratch" — preview the same 5-agent founding team the
      // broker seeds via scratchFoundingTeamBlueprint. Keep the slugs and
      // built_in flag in sync with internal/team/broker_onboarding.go.
      setAgents(SCRATCH_FOUNDING_TEAM.map((a) => ({ ...a })))
      setTaskTemplates([])
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
    const bpTasks = (bp as unknown as { tasks?: TaskTemplate[] } | undefined)?.tasks
    setTaskTemplates(Array.isArray(bpTasks) ? bpTasks : [])
    // Clear any task-template selection and suggestion-derived text when the
    // blueprint changes. Without this, switching from (say) Consulting to
    // YouTube Factory leaves "Turn the directive..." stuck in the textarea —
    // nonsensical in the new context. User-typed custom text is preserved,
    // since selectedTaskTemplate is null for that path.
    setSelectedTaskTemplate((prevSel) => {
      if (prevSel != null) setTaskText('')
      return null
    })
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

  // Open the in-wizard Nex signup affordance. A separate handler (not just
  // `setNexSignupStatus('open')` inline) keeps the error/email state reset in
  // one place — reopening after a failed attempt shouldn't leak the old error.
  const openNexSignup = useCallback(() => {
    setNexSignupError('')
    setNexSignupStatus('open')
  }, [])

  // Submit the email to /nex/register. On success: mark status 'ok' so the
  // UI tells the user to check their inbox. On ErrNotInstalled (502 from the
  // broker when nex-cli isn't on PATH): flip to 'fallback' — the user gets an
  // external link to nex.ai/register and pastes the key on the Setup step.
  // Any other error: surface the message and let the user retry.
  const submitNexSignup = useCallback(async () => {
    const email = nexEmail.trim()
    if (email.length === 0) return
    setNexSignupStatus('submitting')
    setNexSignupError('')
    try {
      await post<{ status: string; output?: string }>('/nex/register', { email })
      setNexSignupStatus('ok')
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Registration failed'
      // nex.Register returns ErrNotInstalled (broker wraps as 502) when
      // nex-cli isn't on PATH. Detect and flip to the external-link flow.
      if (msg.toLowerCase().includes('not installed') || msg.includes('502')) {
        setNexSignupStatus('fallback')
        return
      }
      setNexSignupStatus('open')
      setNexSignupError(msg)
    }
  }, [nexEmail])

  // Close the Nex signup panel via Escape — keeps the outer Escape handler
  // in `useKeyboardShortcuts` free to act on app-level panels without
  // fighting the wizard's internal affordance.
  const closeNexSignup = useCallback(() => {
    if (nexSignupStatus === 'open' || nexSignupStatus === 'ok' || nexSignupStatus === 'fallback') {
      setNexSignupStatus('hidden')
      setNexSignupError('')
    }
  }, [nexSignupStatus])

  // Compute readiness checks. Runs at render time for the 'ready' step — no
  // useMemo because the surface is small (6 checks) and recomputation only
  // happens when one of these inputs changes. Matches the TUI's six-item
  // list in init_flow.go readinessChecks().
  const readinessChecks: ReadinessCheck[] = (() => {
    const checks: ReadinessCheck[] = []

    // 1. Nex API key
    const hasNexKey = nexApiKey.trim().length > 0
    checks.push({
      label: 'Nex API key',
      status: hasNexKey ? 'ready' : 'next',
      detail: hasNexKey
        ? 'Configured. Hosted memory and integrations unlocked.'
        : 'Skipped. Paste a key later from Settings to enable hosted memory.',
    })

    // 2. Tmux / web session. The web app doesn't need tmux — that's the
    // TUI's office runtime. Surface it as a positive "web session" rather
    // than flagging a missing dependency.
    checks.push({
      label: 'Session runtime',
      status: 'ready',
      detail: 'Web session. No tmux required in the browser.',
    })

    // 3. LLM runtime — whatever CLI the user picked as primary, if installed.
    const primaryLabel = runtimePriority[0]
    const primarySpec = primaryLabel
      ? RUNTIMES.find((r) => r.label === primaryLabel)
      : undefined
    const primaryDetection = primarySpec
      ? detectedBinary(prereqs, primarySpec.binary)
      : undefined
    if (primarySpec && primaryDetection?.found) {
      checks.push({
        label: 'LLM runtime',
        status: 'ready',
        detail: primaryDetection.version
          ? `${primarySpec.label} — ${primaryDetection.version}`
          : `${primarySpec.label} installed`,
      })
    } else if (primarySpec) {
      checks.push({
        label: 'LLM runtime',
        status: 'next',
        detail: `${primarySpec.label} selected but not installed. Install before agents can reason.`,
      })
    } else {
      // No runtime picked — check if any API key is set so the user has a path.
      const hasAnyKey = Object.values(apiKeys).some((v) => v.trim().length > 0)
      checks.push({
        label: 'LLM runtime',
        status: hasAnyKey ? 'ready' : 'missing',
        detail: hasAnyKey
          ? 'Provider API key will drive agent runs.'
          : 'Pick a CLI or add a provider key on the Setup step.',
      })
    }

    // 4. Memory backend
    const memoryLabel = MEMORY_BACKEND_OPTIONS.find((o) => o.value === memoryBackend)?.label
      ?? memoryBackend
    let memoryStatus: ReadinessStatus = 'ready'
    let memoryDetail = memoryLabel
    if (memoryBackend === 'gbrain') {
      if (gbrainOpenAIKey.trim().length === 0) {
        memoryStatus = 'missing'
        memoryDetail = 'GBrain selected but OpenAI key is missing.'
      } else {
        memoryDetail = 'GBrain with OpenAI embeddings.'
      }
    } else if (memoryBackend === 'nex') {
      if (!hasNexKey) {
        memoryStatus = 'next'
        memoryDetail = 'Nex selected — add a Nex API key to enable hosted memory.'
      } else {
        memoryDetail = 'Hosted memory via Nex.'
      }
    } else if (memoryBackend === 'markdown') {
      memoryDetail = 'Git-native team wiki in ~/.wuphf/wiki.'
    } else {
      memoryStatus = 'next'
      memoryDetail = 'No shared memory — agents only see per-turn context.'
    }
    checks.push({
      label: 'Memory backend',
      status: memoryStatus,
      detail: memoryDetail,
    })

    // 5. Blueprint
    if (selectedBlueprint === null) {
      checks.push({
        label: 'Blueprint',
        status: 'ready',
        detail: 'Start from scratch (5-person founding team).',
      })
    } else {
      const bp = blueprints.find((b) => b.id === selectedBlueprint)
      checks.push({
        label: 'Blueprint',
        status: 'ready',
        detail: bp?.name ?? selectedBlueprint,
      })
    }

    // 6. Integrations count
    const keyCount = Object.values(apiKeys).filter((v) => v.trim().length > 0).length
      + (gbrainOpenAIKey.trim().length > 0 ? 1 : 0)
      + (gbrainAnthropicKey.trim().length > 0 ? 1 : 0)
    checks.push({
      label: 'Integrations',
      status: keyCount > 0 ? 'ready' : 'next',
      detail:
        keyCount > 0
          ? `${keyCount} provider key${keyCount === 1 ? '' : 's'} configured.`
          : 'None configured. Add providers later from Settings.',
    })

    return checks
  })()

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
        // list + API keys so the broker reads them on next launch. Send as a
        // single POST — the broker's handleConfig does a non-atomic read-
        // mutate-write, so two parallel calls race and corrupt config.json.
        // Keys go through this path (not /onboarding/complete) because the
        // broker's /config endpoint is the canonical persistence surface
        // for config.APIKey, OpenAIAPIKey, AnthropicAPIKey, etc.
        const configPayload: Record<string, unknown> = {
          memory_backend: memoryBackend,
        }
        if (providerPriority.length > 0) {
          configPayload.llm_provider = providerPriority[0]
          configPayload.llm_provider_priority = providerPriority
        }
        // Nex API key (optional — empty string not sent so we don't clobber
        // an existing value with a blank one).
        const trimmedNex = nexApiKey.trim()
        if (trimmedNex.length > 0) {
          configPayload.api_key = trimmedNex
        }
        // GBrain-conditional keys. Only forwarded when GBrain is the active
        // backend; other backends don't need these and sending would
        // overwrite any user-configured values on GET.
        if (memoryBackend === 'gbrain') {
          const trimmedOAI = gbrainOpenAIKey.trim()
          if (trimmedOAI.length > 0) {
            configPayload.openai_api_key = trimmedOAI
          }
          const trimmedAnthropic = gbrainAnthropicKey.trim()
          if (trimmedAnthropic.length > 0) {
            configPayload.anthropic_api_key = trimmedAnthropic
          }
        }
        // Generic per-provider API keys from the fallback grid. Legacy
        // env-var-style keys (ANTHROPIC_API_KEY, OPENAI_API_KEY, GOOGLE_API_KEY)
        // mapped to the broker's config field names. Google key has no
        // /config field yet — drop it silently rather than fail.
        const genericAnthropic = (apiKeys['ANTHROPIC_API_KEY'] ?? '').trim()
        if (genericAnthropic.length > 0 && memoryBackend !== 'gbrain') {
          configPayload.anthropic_api_key = genericAnthropic
        }
        const genericOpenAI = (apiKeys['OPENAI_API_KEY'] ?? '').trim()
        if (genericOpenAI.length > 0 && memoryBackend !== 'gbrain') {
          configPayload.openai_api_key = genericOpenAI
        }
        const genericGemini = (apiKeys['GOOGLE_API_KEY'] ?? '').trim()
        if (genericGemini.length > 0) {
          configPayload.gemini_api_key = genericGemini
        }
        post('/config', configPayload).catch(() => {})

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
      nexApiKey,
      gbrainOpenAIKey,
      gbrainAnthropicKey,
      taskText,
      setOnboardingComplete,
      onComplete,
    ],
  )

  // Keyboard: Enter advances each step when the step's own gate allows it,
  // so the whole wizard can be run without reaching for the mouse. Textarea
  // steps (TaskStep) keep Enter for newlines; ⌘/Ctrl+Enter advances there.
  // The NexSignupPanel handles its own Enter inside the email input via an
  // onKeyDown below, so we bail out when that's the focused target.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key !== 'Enter') return
      const target = e.target as HTMLElement | null
      if (target?.id === 'wiz-nex-email') return
      const inTextarea = target?.tagName === 'TEXTAREA'
      const isSubmitCombo = e.metaKey || e.ctrlKey
      if (inTextarea && !isSubmitCombo) return

      const canIdentityContinue = company.trim().length > 0 && description.trim().length > 0
      const hasInstalledSelection = runtimePriority.some((label) => {
        const spec = RUNTIMES.find((r) => r.label === label)
        if (!spec) return false
        return Boolean(detectedBinary(prereqs, spec.binary)?.found)
      })
      const hasAnyApiKey = Object.values(apiKeys).some((v) => v.trim().length > 0)
      const gbrainOpenAIMissing = memoryBackend === 'gbrain' && gbrainOpenAIKey.trim().length === 0
      const canSetupContinue = (hasInstalledSelection || hasAnyApiKey) && !gbrainOpenAIMissing

      switch (step) {
        case 'welcome':
          e.preventDefault()
          goTo('identity')
          return
        case 'templates':
          e.preventDefault()
          nextStep()
          return
        case 'identity':
          if (canIdentityContinue) {
            e.preventDefault()
            nextStep()
          }
          return
        case 'team':
          e.preventDefault()
          nextStep()
          return
        case 'setup':
          if (canSetupContinue) {
            e.preventDefault()
            nextStep()
          }
          return
        case 'task':
          if (isSubmitCombo) {
            e.preventDefault()
            nextStep()
          }
          return
        case 'ready':
          if (!submitting && taskText.trim().length > 0) {
            e.preventDefault()
            finishOnboarding(false)
          }
          return
      }
    }
    function onEscape(e: KeyboardEvent) {
      if (e.key !== 'Escape') return
      closeNexSignup()
    }
    window.addEventListener('keydown', onKey)
    window.addEventListener('keydown', onEscape)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('keydown', onEscape)
    }
  }, [
    step,
    company,
    description,
    runtimePriority,
    prereqs,
    apiKeys,
    memoryBackend,
    gbrainOpenAIKey,
    submitting,
    taskText,
    goTo,
    nextStep,
    finishOnboarding,
    closeNexSignup,
  ])

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
            nexEmail={nexEmail}
            nexSignupStatus={nexSignupStatus}
            nexSignupError={nexSignupError}
            onChangeCompany={setCompany}
            onChangeDescription={setDescription}
            onChangePriority={setPriority}
            onChangeNexEmail={setNexEmail}
            onSubmitNexSignup={submitNexSignup}
            onOpenNexSignup={openNexSignup}
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
            nexApiKey={nexApiKey}
            onChangeNexApiKey={setNexApiKey}
            gbrainOpenAIKey={gbrainOpenAIKey}
            onChangeGBrainOpenAIKey={setGbrainOpenAIKey}
            gbrainAnthropicKey={gbrainAnthropicKey}
            onChangeGBrainAnthropicKey={setGbrainAnthropicKey}
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
            onNext={nextStep}
            onBack={prevStep}
          />
        )}

        {step === 'ready' && (
          <ReadyStep
            checks={readinessChecks}
            taskText={taskText}
            submitting={submitting}
            onSkip={() => finishOnboarding(true)}
            onSubmit={() => finishOnboarding(false)}
            onBack={prevStep}
          />
        )}
      </div>
    </div>
  )
}
