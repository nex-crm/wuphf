import type { BrokerLogger } from "../types.ts";
import type {
  Clock,
  ConsumeCosignChallengeArgs,
  PruneExpiredWebAuthnStateArgs,
  PruneExpiredWebAuthnStateResult,
  SaveCosignChallengeArgs,
  SaveCredentialArgs,
  SaveRegistrationChallengeArgs,
  WebAuthnStore,
} from "./types.ts";

export const WEBAUTHN_PRUNE_WRITE_INTERVAL = 16;
export const WEBAUTHN_PRUNE_BATCH_ROWS = 64;

export interface OpportunisticWebAuthnPruneOptions {
  readonly clock: Clock;
  readonly logger: BrokerLogger;
  readonly writeInterval?: number;
  readonly maxRows?: number;
}

export function createOpportunisticPruningWebAuthnStore(
  inner: WebAuthnStore,
  options: OpportunisticWebAuthnPruneOptions,
): WebAuthnStore {
  return new OpportunisticPruningWebAuthnStore(inner, options);
}

class OpportunisticPruningWebAuthnStore implements WebAuthnStore {
  private readonly writeInterval: number;
  private readonly maxRows: number;
  private writesSincePrune = 0;

  constructor(
    private readonly inner: WebAuthnStore,
    private readonly options: OpportunisticWebAuthnPruneOptions,
  ) {
    this.writeInterval = normalizePositiveInteger(
      options.writeInterval ?? WEBAUTHN_PRUNE_WRITE_INTERVAL,
      "webauthn prune write interval",
    );
    this.maxRows = normalizePositiveInteger(
      options.maxRows ?? WEBAUTHN_PRUNE_BATCH_ROWS,
      "webauthn prune batch rows",
    );
  }

  async saveRegistrationChallenge(args: SaveRegistrationChallengeArgs): Promise<void> {
    await this.inner.saveRegistrationChallenge(args);
    await this.afterWrite();
  }

  async saveCosignChallenge(args: SaveCosignChallengeArgs): Promise<void> {
    await this.inner.saveCosignChallenge(args);
    await this.afterWrite();
  }

  pruneExpired(args: PruneExpiredWebAuthnStateArgs): Promise<PruneExpiredWebAuthnStateResult> {
    return this.inner.pruneExpired(args);
  }

  getChallenge(...args: Parameters<WebAuthnStore["getChallenge"]>) {
    return this.inner.getChallenge(...args);
  }

  listCredentialsForAgent(...args: Parameters<WebAuthnStore["listCredentialsForAgent"]>) {
    return this.inner.listCredentialsForAgent(...args);
  }

  listCredentialsForAgentRole(...args: Parameters<WebAuthnStore["listCredentialsForAgentRole"]>) {
    return this.inner.listCredentialsForAgentRole(...args);
  }

  getCredential(...args: Parameters<WebAuthnStore["getCredential"]>) {
    return this.inner.getCredential(...args);
  }

  async saveCredential(args: SaveCredentialArgs): Promise<void> {
    await this.inner.saveCredential(args);
    await this.afterWrite();
  }

  getConsumedToken(...args: Parameters<WebAuthnStore["getConsumedToken"]>) {
    return this.inner.getConsumedToken(...args);
  }

  listSatisfiedRoles(...args: Parameters<WebAuthnStore["listSatisfiedRoles"]>) {
    return this.inner.listSatisfiedRoles(...args);
  }

  async consumeCosignChallenge(
    args: ConsumeCosignChallengeArgs,
  ): ReturnType<WebAuthnStore["consumeCosignChallenge"]> {
    const result = await this.inner.consumeCosignChallenge(args);
    await this.afterWrite();
    return result;
  }

  private async afterWrite(): Promise<void> {
    this.writesSincePrune += 1;
    if (this.writesSincePrune < this.writeInterval) return;
    this.writesSincePrune = 0;
    const nowMs = readClock(this.options.clock);
    try {
      const result = await this.inner.pruneExpired({ nowMs, maxRows: this.maxRows });
      this.options.logger.info("webauthn_expired_state_pruned", {
        trigger: "write_interval",
        nowMs,
        maxRows: this.maxRows,
        consumedTokens: result.consumedTokens,
        orphanChallenges: result.orphanChallenges,
      });
    } catch (err) {
      this.options.logger.warn("webauthn_expired_state_prune_failed", {
        trigger: "write_interval",
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }
}

function readClock(clock: Clock): number {
  const nowMs = clock.now();
  if (!Number.isSafeInteger(nowMs) || nowMs < 0) {
    throw new Error(`webauthn prune clock returned invalid timestamp: ${nowMs}`);
  }
  return nowMs;
}

function normalizePositiveInteger(value: number, label: string): number {
  if (!Number.isSafeInteger(value) || value < 1) {
    throw new Error(`${label} must be a positive safe integer`);
  }
  return value;
}
