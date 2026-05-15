import type { ApprovalClaim, ApprovalScope } from "@wuphf/protocol";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { post } from "./client";
import {
  isWebAuthnApprovalPendingResponse,
  requestWebAuthnCosignChallenge,
  requestWebAuthnRegistrationChallenge,
  runWebAuthnAuthenticationCeremony,
  runWebAuthnRegistrationCeremony,
  verifyWebAuthnCosign,
  verifyWebAuthnRegistration,
  type WebAuthnAssertionResponseJson,
  type WebAuthnAttestationResponseJson,
  type WebAuthnCreationOptionsJson,
  type WebAuthnRequestOptionsJson,
} from "./webauthn";

vi.mock("./client", () => ({
  post: vi.fn(),
}));

const postMock = vi.mocked(post);

describe("webauthn api client", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    postMock.mockReset();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("posts the registration challenge request to the frozen broker route", async () => {
    const response = {
      challengeId: "challenge-1",
      creationOptions: registrationOptions(),
    };
    postMock.mockResolvedValue(response);

    await expect(
      requestWebAuthnRegistrationChallenge({ role: "approver" }),
    ).resolves.toEqual(response);

    expect(postMock).toHaveBeenCalledWith("/webauthn/registration/challenge", {
      role: "approver",
    });
  });

  it("posts the registration attestation to the frozen broker route", async () => {
    const attestationResponse = registrationResponse();
    const response = { credentialId: "cred_123", role: "approver" };
    postMock.mockResolvedValue(response);

    await expect(
      verifyWebAuthnRegistration({
        challengeId: "challenge-1",
        attestationResponse,
      }),
    ).resolves.toEqual(response);

    expect(postMock).toHaveBeenCalledWith("/webauthn/registration/verify", {
      challengeId: "challenge-1",
      attestationResponse,
    });
  });

  it("serializes protocol claim and scope JSON for cosign challenge requests", async () => {
    const response = {
      challengeId: "challenge-2",
      requestOptions: authenticationOptions(),
    };
    const { claim, scope } = approvalPair();
    postMock.mockResolvedValue(response);

    await expect(
      requestWebAuthnCosignChallenge({ claim, scope }),
    ).resolves.toEqual(response);

    expect(postMock).toHaveBeenCalledWith("/webauthn/cosign/challenge", {
      claim: {
        schemaVersion: 1,
        claimId: "claim1",
        kind: "receipt_co_sign",
        receiptId: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
        frozenArgsHash:
          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        riskClass: "high",
      },
      scope: {
        mode: "single_use",
        claimId: "claim1",
        claimKind: "receipt_co_sign",
        role: "approver",
        maxUses: 1,
        receiptId: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
        frozenArgsHash:
          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      },
    });
  });

  it("posts cosign assertions and narrows pending threshold responses", async () => {
    const pending = {
      status: "approval_pending" as const,
      satisfiedRoles: ["approver"],
      requiredThreshold: 2,
    };
    postMock.mockResolvedValue(pending);
    const assertionResponse = authenticationResponse();

    const response = await verifyWebAuthnCosign({
      challengeId: "challenge-3",
      assertionResponse,
    });

    expect(isWebAuthnApprovalPendingResponse(response)).toBe(true);
    expect(postMock).toHaveBeenCalledWith("/webauthn/cosign/verify", {
      challengeId: "challenge-3",
      assertionResponse,
    });
  });

  it("runs the SimpleWebAuthn registration wrapper against navigator.credentials.create", async () => {
    const create = vi.fn<
      (options: CredentialCreationOptions) => Promise<Credential | null>
    >(() => Promise.resolve(mockRegistrationCredential()));
    installWebAuthnMocks({ create });

    await expect(
      runWebAuthnRegistrationCeremony(registrationOptions()),
    ).resolves.toMatchObject({
      id: "registration-credential",
      rawId: "AQID",
      response: {
        attestationObject: "BAU",
        clientDataJSON: "Bg",
        transports: ["usb"],
      },
      type: "public-key",
    });

    expect(create).toHaveBeenCalledTimes(1);
    const options = create.mock.calls[0]?.[0];
    expect(options?.publicKey?.challenge).toBeInstanceOf(ArrayBuffer);
    expect(options?.publicKey?.user.id).toBeInstanceOf(ArrayBuffer);
  });

  it("runs the SimpleWebAuthn authentication wrapper against navigator.credentials.get", async () => {
    const get = vi.fn<
      (options?: CredentialRequestOptions) => Promise<Credential | null>
    >(() => Promise.resolve(mockAuthenticationCredential()));
    installWebAuthnMocks({ get });

    await expect(
      runWebAuthnAuthenticationCeremony(authenticationOptions()),
    ).resolves.toMatchObject({
      id: "authentication-credential",
      rawId: "AQID",
      response: {
        authenticatorData: "Bw",
        clientDataJSON: "CA",
        signature: "CQ",
        userHandle: "Cg",
      },
      type: "public-key",
    });

    expect(get).toHaveBeenCalledTimes(1);
    const options = get.mock.calls[0]?.[0];
    expect(options?.publicKey?.challenge).toBeInstanceOf(ArrayBuffer);
    expect(options?.publicKey?.allowCredentials?.[0]?.id).toBeInstanceOf(
      ArrayBuffer,
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

function registrationOptions(): WebAuthnCreationOptionsJson {
  return {
    rp: { id: "localhost", name: "WUPHF" },
    user: {
      id: "AQID",
      name: "approver",
      displayName: "Approver",
    },
    challenge: "BAU",
    pubKeyCredParams: [{ type: "public-key", alg: -7 }],
  };
}

function authenticationOptions(): WebAuthnRequestOptionsJson {
  return {
    challenge: "BAU",
    rpId: "localhost",
    allowCredentials: [{ id: "AQID", type: "public-key", transports: ["usb"] }],
    userVerification: "preferred",
  };
}

function registrationResponse(): WebAuthnAttestationResponseJson {
  return {
    id: "registration-credential",
    rawId: "AQID",
    response: {
      clientDataJSON: "Bg",
      attestationObject: "BAU",
      transports: ["usb"],
    },
    clientExtensionResults: {},
    type: "public-key",
  };
}

function authenticationResponse(): WebAuthnAssertionResponseJson {
  return {
    id: "authentication-credential",
    rawId: "AQID",
    response: {
      clientDataJSON: "CA",
      authenticatorData: "Bw",
      signature: "CQ",
      userHandle: "Cg",
    },
    clientExtensionResults: {},
    type: "public-key",
  };
}

function installWebAuthnMocks(
  credentials: Partial<CredentialsContainer>,
): void {
  vi.stubGlobal("PublicKeyCredential", function PublicKeyCredential() {
    return undefined;
  });
  Object.defineProperty(navigator, "credentials", {
    configurable: true,
    value: credentials,
  });
}

function mockRegistrationCredential(): Credential {
  return {
    id: "registration-credential",
    rawId: bytes(1, 2, 3),
    type: "public-key",
    response: {
      attestationObject: bytes(4, 5),
      clientDataJSON: bytes(6),
      getTransports: () => ["usb"],
    },
    getClientExtensionResults: () => ({}),
  } as unknown as Credential;
}

function mockAuthenticationCredential(): Credential {
  return {
    id: "authentication-credential",
    rawId: bytes(1, 2, 3),
    type: "public-key",
    response: {
      authenticatorData: bytes(7),
      clientDataJSON: bytes(8),
      signature: bytes(9),
      userHandle: bytes(10),
    },
    getClientExtensionResults: () => ({}),
  } as unknown as Credential;
}

function bytes(...values: readonly number[]): ArrayBuffer {
  const buffer = new ArrayBuffer(values.length);
  new Uint8Array(buffer).set(values);
  return buffer;
}
