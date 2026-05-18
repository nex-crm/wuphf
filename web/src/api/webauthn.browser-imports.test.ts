import { describe, expect, it, vi } from "vitest";

vi.mock("node:crypto", () => {
  throw new Error("renderer WebAuthn modules must not import node:crypto");
});

describe("webauthn browser imports", () => {
  it("loads renderer WebAuthn modules without pulling node crypto", async () => {
    const [api, cosignPrompt, registrationPanel] = await Promise.all([
      import("./webauthn"),
      import("../components/cosign/CosignPrompt"),
      import("../components/cosign/CredentialRegistrationPanel"),
    ]);

    expect(api.requestWebAuthnCosignChallenge).toBeTypeOf("function");
    expect(cosignPrompt.CosignPrompt).toBeTypeOf("function");
    expect(registrationPanel.CredentialRegistrationPanel).toBeTypeOf(
      "function",
    );
  });
});
