/**
 * Steer + FollowUp message queues for agent control.
 * - steer: high-priority interrupts that preempt current execution
 * - followUp: normal-priority messages queued for the next turn
 */

export class MessageQueues {
  private steerQueues = new Map<string, string[]>();
  private followUpQueues = new Map<string, string[]>();

  private getQueue(map: Map<string, string[]>, agentSlug: string): string[] {
    let q = map.get(agentSlug);
    if (!q) {
      q = [];
      map.set(agentSlug, q);
    }
    return q;
  }

  steer(agentSlug: string, message: string): void {
    this.getQueue(this.steerQueues, agentSlug).push(message);
  }

  followUp(agentSlug: string, message: string): void {
    this.getQueue(this.followUpQueues, agentSlug).push(message);
  }

  drainSteer(agentSlug: string): string | undefined {
    const q = this.steerQueues.get(agentSlug);
    if (!q || q.length === 0) return undefined;
    return q.shift();
  }

  drainFollowUp(agentSlug: string): string | undefined {
    const q = this.followUpQueues.get(agentSlug);
    if (!q || q.length === 0) return undefined;
    return q.shift();
  }

  hasSteer(agentSlug: string): boolean {
    const q = this.steerQueues.get(agentSlug);
    return q !== undefined && q.length > 0;
  }

  hasFollowUp(agentSlug: string): boolean {
    const q = this.followUpQueues.get(agentSlug);
    return q !== undefined && q.length > 0;
  }

  hasMessages(agentSlug: string): boolean {
    return this.hasSteer(agentSlug) || this.hasFollowUp(agentSlug);
  }
}
