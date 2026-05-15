import {
  type AuthenticationResponseJSON,
  generateAuthenticationOptions,
  generateRegistrationOptions,
  type RegistrationResponseJSON,
  verifyAuthenticationResponse,
  verifyRegistrationResponse,
  type WebAuthnCredential,
} from "@simplewebauthn/server";

import type {
  RegisteredWebAuthnCredentialVerification,
  WebAuthnAuthenticationVerification,
  WebAuthnCeremony,
} from "./types.ts";

export function createSimpleWebAuthnCeremony(): WebAuthnCeremony {
  return {
    async generateRegistrationOptions(args) {
      return await generateRegistrationOptions({
        rpName: args.rpName,
        rpID: args.rpId,
        userName: `${args.agentId}:${args.role}`,
        userDisplayName: `${args.role} for ${args.agentId}`,
        challenge: args.challenge,
        attestationType: "none",
        excludeCredentials: args.excludeCredentialIds.map((id) => ({ id })),
        authenticatorSelection: {
          residentKey: "preferred",
          userVerification: "required",
        },
      });
    },

    async verifyRegistration(args) {
      return await verifyRegistration({
        response: args.response,
        expectedChallenge: args.expectedChallenge,
        expectedOrigins: args.expectedOrigins,
        expectedRpId: args.expectedRpId,
      });
    },

    async generateAuthenticationOptions(args) {
      return await generateAuthenticationOptions({
        rpID: args.rpId,
        challenge: args.challenge,
        allowCredentials: args.allowCredentialIds.map((id) => ({ id })),
        userVerification: "required",
      });
    },

    async verifyAuthentication(args) {
      return await verifyAuthentication({
        response: args.response,
        expectedChallenge: args.expectedChallenge,
        expectedOrigins: args.expectedOrigins,
        expectedRpId: args.expectedRpId,
        credential: args.credential,
      });
    },
  };
}

async function verifyRegistration(args: {
  readonly response: RegistrationResponseJSON;
  readonly expectedChallenge: string;
  readonly expectedOrigins: readonly string[];
  readonly expectedRpId: string;
}): Promise<RegisteredWebAuthnCredentialVerification | null> {
  const result = await verifyRegistrationResponse({
    response: args.response,
    expectedChallenge: args.expectedChallenge,
    expectedOrigin: [...args.expectedOrigins],
    expectedRPID: args.expectedRpId,
    requireUserPresence: true,
    requireUserVerification: true,
  });
  if (!result.verified) {
    return null;
  }
  return {
    credentialId: result.registrationInfo.credential.id,
    publicKey: result.registrationInfo.credential.publicKey,
    signCount: result.registrationInfo.credential.counter,
  };
}

async function verifyAuthentication(args: {
  readonly response: AuthenticationResponseJSON;
  readonly expectedChallenge: string;
  readonly expectedOrigins: readonly string[];
  readonly expectedRpId: string;
  readonly credential: WebAuthnCredential;
}): Promise<WebAuthnAuthenticationVerification | null> {
  const result = await verifyAuthenticationResponse({
    response: args.response,
    expectedChallenge: args.expectedChallenge,
    expectedOrigin: [...args.expectedOrigins],
    expectedRPID: args.expectedRpId,
    credential: args.credential,
    requireUserVerification: true,
  });
  if (!result.verified) {
    return null;
  }
  return {
    credentialId: result.authenticationInfo.credentialID,
    newSignCount: result.authenticationInfo.newCounter,
    userVerified: result.authenticationInfo.userVerified,
  };
}
