/**
 * Type definitions for the Slack-style chat system.
 */

export interface ChatMessage {
  id: string;
  channelId: string;
  sender: string;
  senderType: 'agent' | 'human';
  content: string;
  timestamp: number;
  mentions: string[];
  replyTo?: string;
  metadata?: Record<string, unknown>;
}

export interface Channel {
  id: string;
  name: string;
  type: 'public' | 'private' | 'dm';
  members: string[];
  createdAt: number;
  topic?: string;
}

export interface SuggestedResponse {
  id: string;
  content: string;
  confidence: number;
  source: string;
}
