/**
 * Runtime tool registry and built-in WUPHF tools.
 */

import type { AgentTool } from './types.js';
import type { NexClient } from '../lib/client.js';
import type { GossipLayer } from './gossip.js';

export class ToolRegistry {
  private tools = new Map<string, AgentTool>();

  register(tool: AgentTool): void {
    this.tools.set(tool.name, tool);
  }

  unregister(name: string): void {
    this.tools.delete(name);
  }

  get(name: string): AgentTool | undefined {
    return this.tools.get(name);
  }

  list(): AgentTool[] {
    return Array.from(this.tools.values());
  }

  has(name: string): boolean {
    return this.tools.has(name);
  }

  validate(toolName: string, params: unknown): { valid: boolean; errors?: string[] } {
    const tool = this.tools.get(toolName);
    if (!tool) {
      return { valid: false, errors: [`Unknown tool: ${toolName}`] };
    }

    const schema = tool.schema;
    if (!schema || typeof schema !== 'object') {
      return { valid: true };
    }

    const errors: string[] = [];
    const required = (schema.required as string[] | undefined) ?? [];
    const properties = (schema.properties as Record<string, unknown> | undefined) ?? {};
    const paramObj = (params && typeof params === 'object' ? params : {}) as Record<string, unknown>;

    for (const field of required) {
      if (paramObj[field] === undefined || paramObj[field] === null) {
        errors.push(`Missing required parameter: ${field}`);
      }
    }

    for (const key of Object.keys(paramObj)) {
      if (Object.keys(properties).length > 0 && !(key in properties)) {
        errors.push(`Unknown parameter: ${key}`);
      }
    }

    return errors.length > 0 ? { valid: false, errors } : { valid: true };
  }
}

export function createBuiltinTools(client: NexClient): AgentTool[] {
  const tools: AgentTool[] = [
    {
      name: 'nex_search',
      description: 'Search organizational knowledge base',
      schema: {
        type: 'object',
        required: ['query'],
        properties: {
          query: { type: 'string', description: 'Search query' },
          limit: { type: 'number', description: 'Max results' },
        },
      },
      async execute(params, signal, onUpdate) {
        signal.throwIfAborted();
        onUpdate('Searching...');
        const result = await client.post('/search', {
          query: params.query,
          limit: params.limit ?? 10,
        });
        return JSON.stringify(result);
      },
    },
    {
      name: 'nex_ask',
      description: 'Ask a question to the organizational AI',
      schema: {
        type: 'object',
        required: ['question'],
        properties: {
          question: { type: 'string', description: 'Question to ask' },
          context: { type: 'string', description: 'Additional context' },
        },
      },
      async execute(params, signal, onUpdate) {
        signal.throwIfAborted();
        onUpdate('Thinking...');
        const result = await client.post('/ask', {
          question: params.question,
          context: params.context,
        });
        return JSON.stringify(result);
      },
    },
    {
      name: 'nex_remember',
      description: 'Store an insight or fact in organizational memory',
      schema: {
        type: 'object',
        required: ['content'],
        properties: {
          content: { type: 'string', description: 'Content to remember' },
          tags: { type: 'array', description: 'Tags for categorization' },
        },
      },
      async execute(params, signal, onUpdate) {
        signal.throwIfAborted();
        onUpdate('Storing...');
        const result = await client.post('/remember', {
          content: params.content,
          tags: params.tags,
        });
        return JSON.stringify(result);
      },
    },
    {
      name: 'nex_record_list',
      description: 'List records from a CRM object type',
      schema: {
        type: 'object',
        required: ['objectType'],
        properties: {
          objectType: { type: 'string', description: 'CRM object type (contacts, companies, deals)' },
          limit: { type: 'number', description: 'Max records to return' },
          filters: { type: 'object', description: 'Filter criteria' },
        },
      },
      async execute(params, signal, onUpdate) {
        signal.throwIfAborted();
        onUpdate('Listing records...');
        const query = new URLSearchParams();
        if (params.limit) query.set('limit', String(params.limit));
        const result = await client.get(`/records/${params.objectType}?${query.toString()}`);
        return JSON.stringify(result);
      },
    },
    {
      name: 'nex_record_get',
      description: 'Get a specific record by ID',
      schema: {
        type: 'object',
        required: ['objectType', 'recordId'],
        properties: {
          objectType: { type: 'string', description: 'CRM object type' },
          recordId: { type: 'string', description: 'Record ID' },
        },
      },
      async execute(params, signal, onUpdate) {
        signal.throwIfAborted();
        onUpdate('Fetching record...');
        const result = await client.get(`/records/${params.objectType}/${params.recordId}`);
        return JSON.stringify(result);
      },
    },
    {
      name: 'nex_record_create',
      description: 'Create a new CRM record',
      schema: {
        type: 'object',
        required: ['objectType', 'properties'],
        properties: {
          objectType: { type: 'string', description: 'CRM object type' },
          properties: { type: 'object', description: 'Record properties' },
        },
      },
      async execute(params, signal, onUpdate) {
        signal.throwIfAborted();
        onUpdate('Creating record...');
        const result = await client.post(`/records/${params.objectType}`, {
          properties: params.properties,
        });
        return JSON.stringify(result);
      },
    },
    {
      name: 'nex_record_update',
      description: 'Update an existing CRM record',
      schema: {
        type: 'object',
        required: ['objectType', 'recordId', 'properties'],
        properties: {
          objectType: { type: 'string', description: 'CRM object type' },
          recordId: { type: 'string', description: 'Record ID' },
          properties: { type: 'object', description: 'Properties to update' },
        },
      },
      async execute(params, signal, onUpdate) {
        signal.throwIfAborted();
        onUpdate('Updating record...');
        const result = await client.patch(
          `/records/${params.objectType}/${params.recordId}`,
          { properties: params.properties },
        );
        return JSON.stringify(result);
      },
    },
  ];

  return tools;
}

/**
 * Create gossip-related tools for inter-agent knowledge sharing.
 * @param gossipLayer - The gossip layer instance
 * @param agentSlug - The slug of the agent using these tools
 */
export function createGossipTools(gossipLayer: GossipLayer, agentSlug: string): AgentTool[] {
  return [
    {
      name: 'nex_gossip_publish',
      description: 'Publish an insight for other agents to discover via the gossip network',
      schema: {
        type: 'object',
        required: ['insight'],
        properties: {
          insight: { type: 'string', description: 'The insight or finding to share' },
          context: { type: 'string', description: 'Optional context about the insight' },
        },
      },
      async execute(params, signal, onUpdate) {
        signal.throwIfAborted();
        onUpdate('Publishing insight...');
        const id = await gossipLayer.publish(
          agentSlug,
          params.insight as string,
          params.context as string | undefined,
        );
        return JSON.stringify({ published: true, id });
      },
    },
    {
      name: 'nex_gossip_query',
      description: 'Query the gossip network for insights from other agents on a topic',
      schema: {
        type: 'object',
        required: ['topic'],
        properties: {
          topic: { type: 'string', description: 'The topic to search for insights about' },
        },
      },
      async execute(params, signal, onUpdate) {
        signal.throwIfAborted();
        onUpdate('Querying gossip network...');
        const insights = await gossipLayer.query(
          agentSlug,
          params.topic as string,
        );
        return JSON.stringify({ insights, count: insights.length });
      },
    },
  ];
}
