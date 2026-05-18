import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type {
  ApprovalClaim,
  ApprovalClaimJsonValue,
  ApprovalScope,
  ApprovalScopeJsonValue,
  SignedApprovalTokenJsonValue,
} from "@wuphf/protocol";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as webauthn from "../../api/webauthn";
import { CosignPrompt, describeCosignFailure } from "./CosignPrompt";

vi.mock("../../api/webauthn", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../api/webauthn")>();
  return {
    ...actual,
    requestWebAuthnCosignChallenge: vi.fn(),
    runWebAuthnAuthenticationCeremony: vi.fn(),
    verifyWebAuthnCosign: vi.fn(),
  };
});

const requestChallengeMock = vi.mocked(webauthn.requestWebAuthnCosignChallenge);
const runCeremonyMock = vi.mocked(webauthn.runWebAuthnAuthenticationCeremony);
const verifyMock = vi.mocked(webauthn.verifyWebAuthnCosign);

describe("<CosignPrompt>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    requestChallengeMock.mockReset();
    runCeremonyMock.mockReset();
    verifyMock.mockReset();
  });

  it("renders the human-readable claim and scope being approved", () => {
    const { claim, scope } = approvalPair();

    render(<CosignPrompt claim={claim} scope={scope} />);

    expect(screen.getByText("Receipt co-sign")).toBeInTheDocument();
    expect(screen.getAllByText("01ARZ3NDEKTSV4RRFFQ69G5FAV")).toHaveLength(2);
    expect(
      screen.getAllByText(
        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      ),
    ).toHaveLength(2);
    expect(screen.getAllByText("approver")).not.toHaveLength(0);
  });

  it("runs the cosign ceremony and shows the issued token id", async () => {
    const { claim, scope } = approvalPair();
    const onAccepted = vi.fn();
    requestChallengeMock.mockResolvedValue(
      challengeResponse("challenge-1", claim, scope),
    );
    runCeremonyMock.mockResolvedValue(assertionResponse());
    verifyMock.mockResolvedValue(signedToken(claim, scope));

    render(
      <CosignPrompt claim={claim} scope={scope} onAccepted={onAccepted} />,
    );

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Sign approval" }));

    await waitFor(() =>
      expect(screen.getByText("Approval token issued")).toBeInTheDocument(),
    );
    expect(screen.getByText("01ARZ3NDEKTSV4RRFFQ69G5FAW")).toBeInTheDocument();
    expect(onAccepted).toHaveBeenCalledWith(signedToken(claim, scope));
  });

  it("shows threshold progress when the broker returns approval_pending", async () => {
    const { claim, scope } = approvalPair();
    requestChallengeMock.mockResolvedValue(
      challengeResponse("challenge-2", claim, scope),
    );
    runCeremonyMock.mockResolvedValue(assertionResponse());
    verifyMock.mockResolvedValue({
      status: "approval_pending",
      satisfiedRoles: ["approver"],
      requiredThreshold: 2,
    });

    render(<CosignPrompt claim={claim} scope={scope} />);

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Sign approval" }));

    await waitFor(() =>
      expect(screen.getByText("More roles required")).toBeInTheDocument(),
    );
    expect(screen.getByText("1 of 2 roles satisfied")).toBeInTheDocument();
    expect(screen.getByText("Satisfied roles: approver")).toBeInTheDocument();
  });

  it("shows a non-leaky expired challenge error", async () => {
    const { claim, scope } = approvalPair();
    requestChallengeMock.mockRejectedValue(new Error("challenge expired"));

    render(<CosignPrompt claim={claim} scope={scope} />);

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Sign approval" }));

    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());
    expect(screen.getByRole("alert")).toHaveTextContent(
      "The approval challenge expired or is no longer valid",
    );
  });

  it("shows a non-leaky wrong-agent error", async () => {
    const { claim, scope } = approvalPair();
    requestChallengeMock.mockResolvedValue(
      challengeResponse("challenge-3", claim, scope),
    );
    runCeremonyMock.mockResolvedValue(assertionResponse());
    verifyMock.mockRejectedValue(new Error("wrong agent"));

    render(<CosignPrompt claim={claim} scope={scope} />);

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Sign approval" }));

    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());
    expect(screen.getByRole("alert")).toHaveTextContent(
      "rejected this approval for the current agent",
    );
  });

  it("renders the broker-canonical challenge payload before the security key ceremony", async () => {
    const raw = costSpikePair("budget\u180b-prod-01");
    const canonical = costSpikePair("budget-prod-01");
    requestChallengeMock.mockResolvedValue(
      challengeResponse("challenge-4", canonical.claim, canonical.scope),
    );
    runCeremonyMock.mockImplementation(async () => {
      expect(screen.getAllByText("budget-prod-01")).toHaveLength(2);
      expect(
        screen.queryByText("budget\u180b-prod-01"),
      ).not.toBeInTheDocument();
      return assertionResponse();
    });
    verifyMock.mockResolvedValue(signedToken(canonical.claim, canonical.scope));

    render(<CosignPrompt claim={raw.claim} scope={raw.scope} />);

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Sign approval" }));

    await waitFor(() => expect(runCeremonyMock).toHaveBeenCalledTimes(1));
  });

  it("maps authenticator cancellation to a clear local error", () => {
    expect(describeCosignFailure(new Error("NotAllowedError"))).toBe(
      "The security key ceremony was cancelled before a token was issued.",
    );
  });
});

function approvalPair(): { claim: ApprovalClaim; scope: ApprovalScope } {
  const claimId = "claim1";
  const receiptId = "01ARZ3NDEKTSV4RRFFQ69G5FAV";
  const frozenArgsHash =
    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
  return {
    claim: {
      schemaVersion: 1,
      claimId,
      kind: "receipt_co_sign",
      receiptId,
      frozenArgsHash,
      riskClass: "high",
    } as ApprovalClaim,
    scope: {
      mode: "single_use",
      claimId,
      claimKind: "receipt_co_sign",
      role: "approver",
      maxUses: 1,
      receiptId,
      frozenArgsHash,
    } as ApprovalScope,
  };
}

function costSpikePair(costCeilingId: string): {
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
} {
  const claimId = "claim-cost-spike-1";
  return {
    claim: {
      schemaVersion: 1,
      claimId,
      kind: "cost_spike_acknowledgement",
      agentId: "agent_alpha",
      costCeilingId,
      thresholdBps: 9000,
      currentMicroUsd: 120,
      ceilingMicroUsd: 100,
    } as ApprovalClaim,
    scope: {
      mode: "single_use",
      claimId,
      claimKind: "cost_spike_acknowledgement",
      role: "approver",
      maxUses: 1,
      agentId: "agent_alpha",
      costCeilingId,
    } as ApprovalScope,
  };
}

function challengeResponse(
  challengeId: string,
  claim: ApprovalClaim,
  scope: ApprovalScope,
): webauthn.WebAuthnCosignChallengeResponse {
  return {
    challengeId,
    requestOptions: requestOptions(),
    claim,
    scope,
  };
}

function requestOptions(): webauthn.WebAuthnRequestOptionsJson {
  return {
    challenge: "BAU",
    rpId: "localhost",
  };
}

function assertionResponse(): webauthn.WebAuthnAssertionResponseJson {
  return {
    id: "authentication-credential",
    rawId: "AQID",
    response: {
      clientDataJSON: "CA",
      authenticatorData: "Bw",
      signature: "CQ",
    },
    clientExtensionResults: {},
    type: "public-key",
  };
}

function signedToken(
  claim: ApprovalClaim,
  scope: ApprovalScope,
): SignedApprovalTokenJsonValue {
  return {
    schemaVersion: 1,
    tokenId: "01ARZ3NDEKTSV4RRFFQ69G5FAW",
    claim: claimJson(claim),
    scope: scopeJson(scope),
    notBefore: 1,
    expiresAt: 2,
    issuedTo: "agent1",
    signature: {
      credentialId: "AQID",
      authenticatorData: "Bw",
      clientDataJson: "CA",
      signature: "CQ",
    },
  };
}

function claimJson(claim: ApprovalClaim): ApprovalClaimJsonValue {
  return {
    schemaVersion: 1,
    claimId: "claim1",
    kind: claim.kind,
    receiptId: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
    frozenArgsHash:
      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    riskClass: "high",
  } as ApprovalClaimJsonValue;
}

function scopeJson(scope: ApprovalScope): ApprovalScopeJsonValue {
  return {
    mode: "single_use",
    claimId: "claim1",
    claimKind: scope.claimKind,
    role: "approver",
    maxUses: 1,
    receiptId: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
    frozenArgsHash:
      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  } as ApprovalScopeJsonValue;
}
