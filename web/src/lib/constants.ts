export const SIDEBAR_APPS = [
  { id: 'tasks', icon: '\u2705', name: 'Tasks' },
  { id: 'requests', icon: '\uD83D\uDCCB', name: 'Requests' },
  { id: 'policies', icon: '\uD83D\uDEE1', name: 'Policies' },
  { id: 'calendar', icon: '\uD83D\uDCC5', name: 'Calendar' },
  { id: 'skills', icon: '\u26A1', name: 'Skills' },
  { id: 'activity', icon: '\uD83D\uDCE6', name: 'Activity' },
  { id: 'receipts', icon: '\uD83E\uDDFE', name: 'Receipts' },
  { id: 'health-check', icon: '\uD83D\uDD0D', name: 'Health Check' },
  { id: 'settings', icon: '\u2699', name: 'Settings' },
] as const

export const ONBOARDING_COPY = {
  step1_headline: 'Open source Slack for self-evolving AI agents.',
  step1_subhead:
    'A collaborative office where AI agents like Claude Code, Codex, and OpenClaw learn your work playbooks, build personalized skills, and execute, 24x7. Each agent backed by its own knowledge graph.',
  step1_cta: 'Open the office',
  step2_prereqs_title: 'First, make sure you have the tools',
  step2_keys_title: 'Connect your AI providers',
  step2_cta: 'Ready',
  step3_title: 'What should this office do first?',
  step3_placeholder: 'What should the office invent and start operating?',
  step3_skip: 'Open the office without a first task',
  step3_cta: 'Start the first loop',
} as const

export const DISCONNECT_THRESHOLD = 3
export const MESSAGE_POLL_INTERVAL = 2000
export const MEMBER_POLL_INTERVAL = 5000
export const REQUEST_POLL_INTERVAL = 3000
