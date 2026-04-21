import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import PlaybookSkillBadge from './PlaybookSkillBadge'
import * as api from '../../api/playbook'

describe('<PlaybookSkillBadge>', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('renders the compiled-skill path once the backend reports it exists', async () => {
    vi.spyOn(api, 'fetchPlaybooks').mockResolvedValue([
      {
        slug: 'churn-prevention',
        title: 'Churn prevention',
        source_path: 'team/playbooks/churn-prevention.md',
        skill_path: 'team/playbooks/.compiled/churn-prevention/SKILL.md',
        skill_exists: true,
        execution_log_path: 'team/playbooks/churn-prevention.executions.jsonl',
        execution_count: 3,
        runnable_by_agents: ['*'],
      },
    ])
    render(<PlaybookSkillBadge slug="churn-prevention" />)
    await waitFor(() =>
      expect(
        screen.getByText('team/playbooks/.compiled/churn-prevention/SKILL.md'),
      ).toBeInTheDocument(),
    )
    expect(screen.getByText('3 executions logged')).toBeInTheDocument()
  })

  it('falls back to a pending label when the backend has no row yet', async () => {
    vi.spyOn(api, 'fetchPlaybooks').mockResolvedValue([])
    render(<PlaybookSkillBadge slug="mid-market-onboarding" />)
    await waitFor(() =>
      expect(
        screen.getByText(/Compiled skill pending — recompiling/),
      ).toBeInTheDocument(),
    )
  })
})
