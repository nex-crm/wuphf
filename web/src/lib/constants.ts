export const SIDEBAR_APPS = [
  { id: 'studio', icon: '\u25B6', name: 'Studio' },
  { id: 'tasks', icon: '\u2705', name: 'Tasks' },
  { id: 'requests', icon: '\uD83D\uDCCB', name: 'Requests' },
  { id: 'policies', icon: '\uD83D\uDEE1', name: 'Policies' },
  { id: 'calendar', icon: '\uD83D\uDCC5', name: 'Calendar' },
  { id: 'skills', icon: '\u26A1', name: 'Skills' },
  { id: 'activity', icon: '\uD83D\uDCE6', name: 'Activity' },
  { id: 'receipts', icon: '\uD83E\uDDFE', name: 'Receipts' },
  { id: 'health-check', icon: '\uD83D\uDD0D', name: 'Health Check' },
] as const

export const ONBOARDING_COPY = {
  step1_headline: 'Your office, visible and doing real work.',
  step1_subhead:
    'WUPHF gives operators one room where AI specialists can claim real business work, show progress in public, and pull in a human only when judgment is required. Live runs are judged on real outcomes, not proof artifacts or demo theater.',
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
