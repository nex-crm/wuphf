import { describe, it, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { OrchestrationService } from '../../../src/tui/services/orchestration-service.js';

describe('OrchestrationService', () => {
  let service: OrchestrationService;

  beforeEach(() => {
    service = new OrchestrationService({
      globalBudget: { maxTokens: 10_000, maxCostUsd: 1.0 },
    });
  });

  // ── Goal management ──

  describe('createGoal / getGoals', () => {
    it('round-trips a created goal', () => {
      const goal = service.createGoal('Test Goal', 'A description');
      const goals = service.getGoals();
      assert.equal(goals.length, 1);
      assert.equal(goals[0].id, goal.id);
      assert.equal(goals[0].title, 'Test Goal');
      assert.equal(goals[0].description, 'A description');
      assert.equal(goals[0].status, 'active');
    });

    it('creates multiple goals', () => {
      service.createGoal('Goal 1', 'First');
      service.createGoal('Goal 2', 'Second');
      assert.equal(service.getGoals().length, 2);
    });

    it('assigns unique IDs to each goal', () => {
      const g1 = service.createGoal('A', 'a');
      const g2 = service.createGoal('B', 'b');
      assert.notEqual(g1.id, g2.id);
    });
  });

  // ── Task management ──

  describe('createTask / getTasks', () => {
    it('round-trips a created task', () => {
      const task = service.createTask({
        title: 'Do thing',
        description: 'A task',
        requiredSkills: ['writing'],
        priority: 'high',
      });
      const tasks = service.getTasks();
      assert.equal(tasks.length, 1);
      assert.equal(tasks[0].id, task.id);
      assert.equal(tasks[0].title, 'Do thing');
      assert.equal(tasks[0].status, 'pending');
    });

    it('links task to parent goal', () => {
      const goal = service.createGoal('Parent', 'desc');
      const task = service.createTask({
        title: 'Child',
        description: 'linked',
        requiredSkills: [],
        priority: 'medium',
        parentGoalId: goal.id,
      });

      const goalTasks = service.getTasks(goal.id);
      assert.equal(goalTasks.length, 1);
      assert.equal(goalTasks[0].id, task.id);
    });

    it('returns empty array for unknown goal id', () => {
      service.createTask({
        title: 'Orphan',
        description: 'no parent',
        requiredSkills: [],
        priority: 'low',
      });
      const tasks = service.getTasks('nonexistent-id');
      assert.equal(tasks.length, 0);
    });

    it('getActiveTasks returns only in-progress/locked tasks', () => {
      service.createTask({
        title: 'Pending',
        description: 'not active',
        requiredSkills: [],
        priority: 'low',
      });
      // All tasks start as pending, so active should be empty
      assert.equal(service.getActiveTasks().length, 0);
    });
  });

  // ── Workflow templates ──

  describe('instantiateWorkflow', () => {
    it('creates goal and tasks from seo-audit template', () => {
      const { goal, tasks } = service.instantiateWorkflow('seo-audit');
      assert.equal(goal.title, 'SEO Audit');
      assert.equal(goal.status, 'active');
      assert.equal(tasks.length, 3);
      assert.ok(tasks.every((t) => t.parentGoalId === goal.id));
      assert.ok(tasks.every((t) => t.status === 'pending'));
    });

    it('creates goal and tasks from lead-gen-pipeline template', () => {
      const { goal, tasks } = service.instantiateWorkflow('lead-gen-pipeline');
      assert.equal(goal.title, 'Lead Generation');
      assert.equal(tasks.length, 3);
    });

    it('throws on unknown template name', () => {
      assert.throws(
        () => service.instantiateWorkflow('nonexistent'),
        /Unknown workflow template/,
      );
    });

    it('getTemplateNames returns available templates', () => {
      const names = service.getTemplateNames();
      assert.ok(names.includes('seo-audit'));
      assert.ok(names.includes('lead-gen-pipeline'));
      assert.ok(names.includes('enrichment-batch'));
    });
  });

  // ── Budget ──

  describe('getBudgetSnapshots / getGlobalBudget', () => {
    it('returns empty snapshots when no agents tracked', () => {
      const snapshots = service.getBudgetSnapshots();
      assert.equal(snapshots.length, 0);
    });

    it('returns zero global budget when nothing consumed', () => {
      const global = service.getGlobalBudget();
      assert.equal(global.tokens, 0);
      assert.equal(global.cost, 0);
      assert.equal(global.percentTokens, 0);
      assert.equal(global.percentCost, 0);
    });
  });

  // ── Subscribe ──

  describe('subscribe', () => {
    it('fires listener on createGoal', () => {
      let callCount = 0;
      service.subscribe(() => {
        callCount++;
      });
      service.createGoal('Test', 'desc');
      assert.equal(callCount, 1);
    });

    it('fires listener on createTask', () => {
      let callCount = 0;
      service.subscribe(() => {
        callCount++;
      });
      service.createTask({
        title: 'Task',
        description: 'desc',
        requiredSkills: [],
        priority: 'low',
      });
      assert.equal(callCount, 1);
    });

    it('fires multiple times for instantiateWorkflow', () => {
      let callCount = 0;
      service.subscribe(() => {
        callCount++;
      });
      service.instantiateWorkflow('seo-audit');
      // 1 createGoal + 3 createTask = 4 notifications
      assert.equal(callCount, 4);
    });

    it('unsubscribe stops notifications', () => {
      let callCount = 0;
      const unsub = service.subscribe(() => {
        callCount++;
      });
      service.createGoal('First', 'desc');
      assert.equal(callCount, 1);

      unsub();
      service.createGoal('Second', 'desc');
      assert.equal(callCount, 1); // no increase
    });
  });

  // ── Agent routing ──

  describe('registerAgentSkills / findBestAgentForTask', () => {
    it('returns null when no agents registered', () => {
      const task = service.createTask({
        title: 'Task',
        description: 'desc',
        requiredSkills: ['seo'],
        priority: 'high',
      });
      assert.equal(service.findBestAgentForTask(task.id), null);
    });

    it('finds matching agent for task', () => {
      service.registerAgentSkills('seo-agent', [
        { name: 'seo', description: 'SEO expertise', proficiency: 0.9 },
      ]);
      const task = service.createTask({
        title: 'SEO Task',
        description: 'needs seo',
        requiredSkills: ['seo'],
        priority: 'high',
      });
      const result = service.findBestAgentForTask(task.id);
      assert.ok(result);
      assert.equal(result.agentSlug, 'seo-agent');
      assert.ok(result.score > 0);
    });

    it('returns null for unknown task id', () => {
      assert.equal(service.findBestAgentForTask('nonexistent'), null);
    });
  });
});
