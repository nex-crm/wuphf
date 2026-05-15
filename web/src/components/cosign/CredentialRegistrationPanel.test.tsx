import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as webauthn from "../../api/webauthn";
import { showNotice } from "../ui/Toast";
import {
  CredentialRegistrationPanel,
  describeRegistrationFailure,
} from "./CredentialRegistrationPanel";

vi.mock("../../api/webauthn", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../api/webauthn")>();
  return {
    ...actual,
    requestWebAuthnRegistrationChallenge: vi.fn(),
    runWebAuthnRegistrationCeremony: vi.fn(),
    verifyWebAuthnRegistration: vi.fn(),
  };
});

vi.mock("../ui/Toast", () => ({
  showNotice: vi.fn(),
}));

const requestChallengeMock = vi.mocked(
  webauthn.requestWebAuthnRegistrationChallenge,
);
const runCeremonyMock = vi.mocked(webauthn.runWebAuthnRegistrationCeremony);
const verifyMock = vi.mocked(webauthn.verifyWebAuthnRegistration);
const showNoticeMock = vi.mocked(showNotice);

describe("<CredentialRegistrationPanel>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    requestChallengeMock.mockReset();
    runCeremonyMock.mockReset();
    verifyMock.mockReset();
    showNoticeMock.mockReset();
  });

  it("registers the selected role and shows the broker-confirmed credential", async () => {
    requestChallengeMock.mockResolvedValue({
      challengeId: "challenge-1",
      creationOptions: creationOptions(),
    });
    runCeremonyMock.mockResolvedValue(attestationResponse());
    verifyMock.mockResolvedValue({
      credentialId: "credential-123",
      role: "approver",
    });

    render(<CredentialRegistrationPanel />);

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Register security key" }));

    await waitFor(() =>
      expect(screen.getByText("Credential registered")).toBeInTheDocument(),
    );
    expect(screen.getByText("credential-123")).toBeInTheDocument();
    expect(requestChallengeMock).toHaveBeenCalledWith({ role: "approver" });
    expect(verifyMock).toHaveBeenCalledWith({
      challengeId: "challenge-1",
      attestationResponse: attestationResponse(),
    });
    expect(showNoticeMock).toHaveBeenCalledWith(
      "Security key registered for approver",
      "success",
    );
  });

  it("can register a custom broker role", async () => {
    requestChallengeMock.mockResolvedValue({
      challengeId: "challenge-2",
      creationOptions: creationOptions(),
    });
    runCeremonyMock.mockResolvedValue(attestationResponse());
    verifyMock.mockResolvedValue({
      credentialId: "credential-456",
      role: "security",
    });

    render(<CredentialRegistrationPanel />);

    const user = userEvent.setup();
    await user.selectOptions(screen.getByLabelText("Approval role"), "custom");
    await user.type(screen.getByLabelText("Custom role"), "security");
    await user.click(
      screen.getByRole("button", { name: "Register security key" }),
    );

    await waitFor(() =>
      expect(screen.getByText("credential-456")).toBeInTheDocument(),
    );
    expect(requestChallengeMock).toHaveBeenCalledWith({ role: "security" });
  });

  it("shows a clear expired registration challenge error", async () => {
    requestChallengeMock.mockRejectedValue(new Error("invalid challenge"));

    render(<CredentialRegistrationPanel />);

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Register security key" }));

    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());
    expect(screen.getByRole("alert")).toHaveTextContent(
      "The registration challenge expired or is no longer valid",
    );
  });

  it("requires a non-empty custom role before contacting the broker", async () => {
    render(<CredentialRegistrationPanel />);

    const user = userEvent.setup();
    await user.selectOptions(screen.getByLabelText("Approval role"), "custom");
    await user.click(
      screen.getByRole("button", { name: "Register security key" }),
    );

    expect(requestChallengeMock).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Choose a role before registering",
    );
  });

  it("maps authenticator cancellation to a clear local error", () => {
    expect(describeRegistrationFailure(new Error("AbortError"))).toBe(
      "The security key ceremony was cancelled before a credential was registered.",
    );
  });
});

function creationOptions(): webauthn.WebAuthnCreationOptionsJson {
  return {
    rp: { id: "localhost", name: "WUPHF" },
    user: { id: "AQID", name: "approver", displayName: "Approver" },
    challenge: "BAU",
    pubKeyCredParams: [{ type: "public-key", alg: -7 }],
  };
}

function attestationResponse(): webauthn.WebAuthnAttestationResponseJson {
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
