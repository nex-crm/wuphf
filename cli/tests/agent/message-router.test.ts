import { describe, it, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { MessageRouter } from '../../src/agent/message-router.js';

describe('MessageRouter', () => {
  let router: MessageRouter;

  const agents = [
    { slug: 'seo-agent', expertise: ['seo', 'content-analysis', 'keyword-research'] },
    { slug: 'sales-agent', expertise: ['prospecting', 'outreach', 'pipeline'] },
    { slug: 'enrichment-agent', expertise: ['data-enrichment', 'validation'] },
    { slug: 'cs-agent', expertise: ['relationship-management', 'health-scoring', 'renewal-tracking'] },
  ];

  beforeEach(() => {
    router = new MessageRouter();
  });

  // ── Routing to best agent ───────────────────────────────────────

  it('routes SEO message to seo-agent', () => {
    const result = router.route('Analyze our SEO keyword rankings', agents);
    assert.equal(result.primary, 'seo-agent');
    assert.equal(result.isFollowUp, false);
  });

  it('routes prospecting message to sales-agent', () => {
    const result = router.route('Find new leads for our pipeline', agents);
    assert.equal(result.primary, 'sales-agent');
    assert.equal(result.isFollowUp, false);
  });

  it('routes enrichment message to enrichment-agent', () => {
    const result = router.route('Enrich and validate this contact data', agents);
    assert.equal(result.primary, 'enrichment-agent');
  });

  it('routes customer success message to cs-agent', () => {
    const result = router.route('Check customer health scores and churn risk', agents);
    assert.equal(result.primary, 'cs-agent');
  });

  // ── No match → team-lead ────────────────────────────────────────

  it('returns team-lead when no agents match', () => {
    const result = router.route('What is the meaning of life?', agents);
    assert.equal(result.primary, 'team-lead');
    assert.equal(result.isFollowUp, false);
    assert.equal(result.teamLeadAware, false);
  });

  it('returns team-lead when no agents are available', () => {
    const result = router.route('Analyze SEO', []);
    assert.equal(result.primary, 'team-lead');
  });

  // ── Thread follow-up detection ──────────────────────────────────

  it('detects follow-up and routes to same agent', () => {
    // Simulate recent activity
    router.recordAgentActivity('seo-agent');
    const result = router.route('also check the meta descriptions', agents);
    assert.equal(result.primary, 'seo-agent');
    assert.equal(result.isFollowUp, true);
  });

  it('detects follow-up with "and" prefix', () => {
    router.recordAgentActivity('sales-agent');
    const result = router.route('and send them a follow-up email', agents);
    assert.equal(result.primary, 'sales-agent');
    assert.equal(result.isFollowUp, true);
  });

  it('detects follow-up with "what about" prefix', () => {
    router.recordAgentActivity('enrichment-agent');
    const result = router.route('what about the phone numbers?', agents);
    assert.equal(result.primary, 'enrichment-agent');
    assert.equal(result.isFollowUp, true);
  });

  it('does not detect follow-up when no recent activity', () => {
    // No recordAgentActivity called
    const result = router.route('also do this', agents);
    // Should fall through to skill-based routing, not follow-up
    assert.equal(result.isFollowUp, false);
  });

  // ── Multi-agent collaboration ───────────────────────────────────

  it('returns collaborators when multiple agents match', () => {
    const result = router.route('Research and analyze competitor SEO content strategy', agents);
    // Should have a primary and potentially collaborators
    assert.ok(result.primary);
    assert.ok(Array.isArray(result.collaborators));
  });

  it('teamLeadAware is true when primary is not team-lead', () => {
    const result = router.route('Analyze SEO rankings', agents);
    assert.equal(result.teamLeadAware, true);
  });

  // ── extractSkills ───────────────────────────────────────────────

  it('extracts research skills', () => {
    const skills = router.extractSkills('research competitor analysis');
    assert.ok(skills.includes('market-research'));
    assert.ok(skills.includes('competitive-analysis'));
  });

  it('extracts prospecting skills', () => {
    const skills = router.extractSkills('find new leads for outreach');
    assert.ok(skills.includes('prospecting'));
    assert.ok(skills.includes('outreach'));
  });

  it('extracts enrichment skills', () => {
    const skills = router.extractSkills('enrich and validate the data');
    assert.ok(skills.includes('data-enrichment'));
    assert.ok(skills.includes('validation'));
  });

  it('extracts SEO skills', () => {
    const skills = router.extractSkills('improve our seo keyword rankings');
    assert.ok(skills.includes('seo'));
    assert.ok(skills.includes('content-analysis'));
  });

  it('extracts customer success skills', () => {
    const skills = router.extractSkills('check customer health and churn risk');
    assert.ok(skills.includes('relationship-management'));
    assert.ok(skills.includes('health-scoring'));
  });

  it('extracts code/general skills', () => {
    const skills = router.extractSkills('fix the bug in deploy script');
    assert.ok(skills.includes('general'));
    assert.ok(skills.includes('planning'));
  });

  it('returns empty array for unmatched message', () => {
    const skills = router.extractSkills('what is the meaning of life?');
    assert.equal(skills.length, 0);
  });

  it('deduplicates skills', () => {
    const skills = router.extractSkills('research and investigate the analysis');
    const uniqueCount = new Set(skills).size;
    assert.equal(skills.length, uniqueCount);
  });

  // ── registerAgent / unregisterAgent ─────────────────────────────

  it('unregisterAgent removes agent from routing', () => {
    router.registerAgent('temp-agent', ['seo']);
    router.unregisterAgent('temp-agent');
    const result = router.route('Analyze SEO', []);
    assert.equal(result.primary, 'team-lead');
  });
});
