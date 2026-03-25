/**
 * Pre-built workflow templates for common multi-agent workflows.
 */

import type { GoalDefinition, TaskDefinition } from './types.js';

export interface WorkflowTemplate {
  name: string;
  description: string;
  goals: Omit<GoalDefinition, 'id' | 'createdAt' | 'tasks'>[];
  tasks: Omit<
    TaskDefinition,
    'id' | 'createdAt' | 'status' | 'completedAt' | 'result'
  >[];
}

export const workflows: Record<string, WorkflowTemplate> = {
  'seo-audit': {
    name: 'SEO Audit',
    description: 'Comprehensive SEO analysis of content and keywords',
    goals: [
      {
        title: 'SEO Audit',
        description: 'Analyze and improve search visibility',
        status: 'active',
      },
    ],
    tasks: [
      {
        title: 'Keyword analysis',
        description: 'Research current keyword rankings',
        requiredSkills: ['seo', 'keyword-research'],
        priority: 'high',
      },
      {
        title: 'Content gap analysis',
        description: 'Identify missing content opportunities',
        requiredSkills: ['seo', 'content-analysis'],
        priority: 'medium',
      },
      {
        title: 'Competitor analysis',
        description: 'Analyze competitor SEO strategies',
        requiredSkills: ['seo', 'competitive-analysis'],
        priority: 'medium',
      },
    ],
  },

  'lead-gen-pipeline': {
    name: 'Lead Generation Pipeline',
    description: 'Automated lead discovery and qualification',
    goals: [
      {
        title: 'Lead Generation',
        description: 'Find and qualify new prospects',
        status: 'active',
      },
    ],
    tasks: [
      {
        title: 'Prospect discovery',
        description: 'Search for potential leads matching ICP',
        requiredSkills: ['prospecting', 'research'],
        priority: 'high',
      },
      {
        title: 'Lead enrichment',
        description: 'Enrich discovered leads with additional data',
        requiredSkills: ['data-enrichment', 'research'],
        priority: 'medium',
      },
      {
        title: 'Lead scoring',
        description: 'Score and qualify enriched leads',
        requiredSkills: ['prospecting', 'data-enrichment'],
        priority: 'medium',
      },
    ],
  },

  'enrichment-batch': {
    name: 'Data Enrichment Batch',
    description: 'Bulk enrichment of existing records',
    goals: [
      {
        title: 'Data Enrichment',
        description: 'Fill gaps in existing records',
        status: 'active',
      },
    ],
    tasks: [
      {
        title: 'Identify gaps',
        description: 'Find records with missing key fields',
        requiredSkills: ['data-enrichment'],
        priority: 'high',
      },
      {
        title: 'Research and fill',
        description: 'Research and fill missing data',
        requiredSkills: ['data-enrichment', 'research'],
        priority: 'high',
      },
      {
        title: 'Validate updates',
        description: 'Verify enriched data quality',
        requiredSkills: ['data-enrichment', 'validation'],
        priority: 'medium',
      },
    ],
  },
};
