import type { OfficeMember } from '../api/client'

export type HarnessKind = 'claude-code' | 'codex' | 'openclaw'

const VALID_KINDS: Record<string, HarnessKind> = {
  'claude-code': 'claude-code',
  claude: 'claude-code',
  codex: 'codex',
  openclaw: 'openclaw',
}

function normalize(raw: string | undefined | null): HarnessKind | null {
  if (!raw) return null
  return VALID_KINDS[raw.toLowerCase()] ?? null
}

export function resolveHarness(
  provider: OfficeMember['provider'],
  fallback: HarnessKind = 'claude-code',
): HarnessKind {
  if (typeof provider === 'string') {
    return normalize(provider) ?? fallback
  }
  if (provider && typeof provider === 'object') {
    return normalize(provider.kind) ?? fallback
  }
  return fallback
}

export function harnessLabel(kind: HarnessKind): string {
  switch (kind) {
    case 'claude-code':
      return 'Claude Code'
    case 'codex':
      return 'Codex'
    case 'openclaw':
      return 'OpenClaw'
  }
}
