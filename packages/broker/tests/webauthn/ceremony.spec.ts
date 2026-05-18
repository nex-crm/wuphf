import type {
  AuthenticationResponseJSON,
  PublicKeyCredentialCreationOptionsJSON,
  PublicKeyCredentialRequestOptionsJSON,
  RegistrationResponseJSON,
  WebAuthnCredential,
} from "@simplewebauthn/server";
import { asAgentId } from "@wuphf/protocol";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { createSimpleWebAuthnCeremony } from "../../src/webauthn/ceremony.ts";

const simpleWebAuthnMocks = vi.hoisted(() => ({
  generateRegistrationOptions: vi.fn(),
  verifyRegistrationResponse: vi.fn(),
  generateAuthenticationOptions: vi.fn(),
  verifyAuthenticationResponse: vi.fn(),
}));

vi.mock("@simplewebauthn/server", () => simpleWebAuthnMocks);

describe("createSimpleWebAuthnCeremony", () => {
  beforeEach(() => {
    simpleWebAuthnMocks.generateRegistrationOptions.mockReset();
    simpleWebAuthnMocks.verifyRegistrationResponse.mockReset();
    simpleWebAuthnMocks.generateAuthenticationOptions.mockReset();
    simpleWebAuthnMocks.verifyAuthenticationResponse.mockReset();
  });

  it("passes strict registration options to SimpleWebAuthn", async () => {
    const options: PublicKeyCredentialCreationOptionsJSON = {
      rp: { id: "localhost", name: "WUPHF" },
      user: { id: "agent_alpha", name: "agent_alpha:approver", displayName: "Approver" },
      challenge: "challenge",
      pubKeyCredParams: [{ type: "public-key", alg: -7 }],
    };
    simpleWebAuthnMocks.generateRegistrationOptions.mockResolvedValue(options);

    const result = await createSimpleWebAuthnCeremony().generateRegistrationOptions({
      rpName: "WUPHF",
      rpId: "localhost",
      agentId: asAgentId("agent_alpha"),
      role: "approver",
      challenge: "challenge",
      excludeCredentialIds: ["cred_1", "cred_2"],
    });

    expect(result).toBe(options);
    expect(simpleWebAuthnMocks.generateRegistrationOptions).toHaveBeenCalledWith({
      rpName: "WUPHF",
      rpID: "localhost",
      userName: "agent_alpha:approver",
      userDisplayName: "approver for agent_alpha",
      challenge: "challenge",
      attestationType: "none",
      excludeCredentials: [{ id: "cred_1" }, { id: "cred_2" }],
      authenticatorSelection: {
        residentKey: "preferred",
        userVerification: "required",
      },
    });
  });

  it("passes strict registration verification options and maps verified results", async () => {
    const publicKey = new Uint8Array([1, 2, 3]);
    simpleWebAuthnMocks.verifyRegistrationResponse.mockResolvedValue({
      verified: true,
      registrationInfo: {
        credential: {
          id: "cred_approver",
          publicKey,
          counter: 7,
        },
      },
    });
    const response = registrationResponse();

    const result = await createSimpleWebAuthnCeremony().verifyRegistration({
      response,
      expectedChallenge: "challenge",
      expectedOrigins: ["http://localhost:5173", "http://localhost:3000"],
      expectedRpId: "localhost",
    });

    expect(simpleWebAuthnMocks.verifyRegistrationResponse).toHaveBeenCalledWith({
      response,
      expectedChallenge: "challenge",
      expectedOrigin: ["http://localhost:5173", "http://localhost:3000"],
      expectedRPID: "localhost",
      requireUserPresence: true,
      requireUserVerification: true,
    });
    expect(result).toEqual({
      credentialId: "cred_approver",
      publicKey,
      signCount: 7,
    });
  });

  it("maps unverified registration results to null", async () => {
    simpleWebAuthnMocks.verifyRegistrationResponse.mockResolvedValue({ verified: false });

    await expect(
      createSimpleWebAuthnCeremony().verifyRegistration({
        response: registrationResponse(),
        expectedChallenge: "challenge",
        expectedOrigins: ["http://localhost:5173"],
        expectedRpId: "localhost",
      }),
    ).resolves.toBeNull();
  });

  it("passes strict authentication options to SimpleWebAuthn", async () => {
    const options: PublicKeyCredentialRequestOptionsJSON = {
      challenge: "challenge",
      rpId: "localhost",
      allowCredentials: [{ id: "cred_approver", type: "public-key" }],
    };
    simpleWebAuthnMocks.generateAuthenticationOptions.mockResolvedValue(options);

    const result = await createSimpleWebAuthnCeremony().generateAuthenticationOptions({
      rpId: "localhost",
      challenge: "challenge",
      allowCredentialIds: ["cred_approver"],
    });

    expect(result).toBe(options);
    expect(simpleWebAuthnMocks.generateAuthenticationOptions).toHaveBeenCalledWith({
      rpID: "localhost",
      challenge: "challenge",
      allowCredentials: [{ id: "cred_approver" }],
      userVerification: "required",
    });
  });

  it("passes strict authentication verification options and maps verified results", async () => {
    const credential: WebAuthnCredential = {
      id: "cred_approver",
      publicKey: new Uint8Array([1, 2, 3]),
      counter: 7,
    };
    simpleWebAuthnMocks.verifyAuthenticationResponse.mockResolvedValue({
      verified: true,
      authenticationInfo: {
        credentialID: "cred_approver",
        newCounter: 8,
        userVerified: true,
      },
    });
    const response = authenticationResponse();

    const result = await createSimpleWebAuthnCeremony().verifyAuthentication({
      response,
      expectedChallenge: "challenge",
      expectedOrigins: ["http://localhost:5173"],
      expectedRpId: "localhost",
      credential,
    });

    expect(simpleWebAuthnMocks.verifyAuthenticationResponse).toHaveBeenCalledWith({
      response,
      expectedChallenge: "challenge",
      expectedOrigin: ["http://localhost:5173"],
      expectedRPID: "localhost",
      credential,
      requireUserVerification: true,
    });
    expect(result).toEqual({
      credentialId: "cred_approver",
      newSignCount: 8,
      userVerified: true,
    });
  });

  it("maps unverified authentication results to null", async () => {
    simpleWebAuthnMocks.verifyAuthenticationResponse.mockResolvedValue({ verified: false });

    await expect(
      createSimpleWebAuthnCeremony().verifyAuthentication({
        response: authenticationResponse(),
        expectedChallenge: "challenge",
        expectedOrigins: ["http://localhost:5173"],
        expectedRpId: "localhost",
        credential: {
          id: "cred_approver",
          publicKey: new Uint8Array([1, 2, 3]),
          counter: 7,
        },
      }),
    ).resolves.toBeNull();
  });
});

function registrationResponse(): RegistrationResponseJSON {
  return {
    id: "cred_approver",
    rawId: "cred_approver",
    response: {
      clientDataJSON: "Y2xpZW50",
      attestationObject: "YXR0ZXN0YXRpb24",
    },
    clientExtensionResults: {},
    type: "public-key",
  };
}

function authenticationResponse(): AuthenticationResponseJSON {
  return {
    id: "cred_approver",
    rawId: "cred_approver",
    response: {
      clientDataJSON: "Y2xpZW50",
      authenticatorData: "YXV0aA",
      signature: "c2ln",
    },
    clientExtensionResults: {},
    type: "public-key",
  };
}
