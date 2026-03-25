/**
 * Suggested response generation for human @mentions.
 * Simple heuristic-based suggestions: accept, customize, defer.
 */

import { randomUUID } from 'node:crypto';
import type { ChatMessage, SuggestedResponse } from './types.js';

export function generateSuggestions(
  message: ChatMessage,
  context?: string,
): SuggestedResponse[] {
  const suggestions: SuggestedResponse[] = [];
  const content = message.content.toLowerCase();

  // Detect intent from message content
  const isQuestion = content.includes('?') || content.startsWith('what') ||
    content.startsWith('how') || content.startsWith('why') || content.startsWith('can');
  const isRequest = content.includes('please') || content.includes('could you') ||
    content.includes('can you') || content.includes('would you');
  const isApproval = content.includes('approve') || content.includes('confirm') ||
    content.includes('go ahead') || content.includes('proceed');

  if (isApproval) {
    suggestions.push({
      id: randomUUID(),
      content: 'Approved. Go ahead.',
      confidence: 0.9,
      source: message.sender,
    });
    suggestions.push({
      id: randomUUID(),
      content: 'Approved with changes: [specify adjustments]',
      confidence: 0.6,
      source: message.sender,
    });
    suggestions.push({
      id: randomUUID(),
      content: 'Hold off on this for now.',
      confidence: 0.3,
      source: message.sender,
    });
  } else if (isQuestion) {
    suggestions.push({
      id: randomUUID(),
      content: context ? `Based on context: ${context.substring(0, 100)}...` : 'Let me look into this.',
      confidence: 0.7,
      source: message.sender,
    });
    suggestions.push({
      id: randomUUID(),
      content: 'I need more details to answer this.',
      confidence: 0.5,
      source: message.sender,
    });
    suggestions.push({
      id: randomUUID(),
      content: 'Let me get back to you on this.',
      confidence: 0.4,
      source: message.sender,
    });
  } else if (isRequest) {
    suggestions.push({
      id: randomUUID(),
      content: 'On it.',
      confidence: 0.8,
      source: message.sender,
    });
    suggestions.push({
      id: randomUUID(),
      content: 'I can handle this, but with a slight modification: [specify]',
      confidence: 0.5,
      source: message.sender,
    });
    suggestions.push({
      id: randomUUID(),
      content: 'This is outside my current scope. Deferring.',
      confidence: 0.3,
      source: message.sender,
    });
  } else {
    // Generic fallback
    suggestions.push({
      id: randomUUID(),
      content: 'Acknowledged.',
      confidence: 0.7,
      source: message.sender,
    });
    suggestions.push({
      id: randomUUID(),
      content: 'Can you elaborate on this?',
      confidence: 0.5,
      source: message.sender,
    });
    suggestions.push({
      id: randomUUID(),
      content: 'Noted. Will follow up later.',
      confidence: 0.4,
      source: message.sender,
    });
  }

  return suggestions;
}
