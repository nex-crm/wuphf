import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { APPROVAL_ROLE_VALUES } from "@wuphf/protocol";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "../../api/client";
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
const [defaultRole] = APPROVAL_ROLE_VALUES;

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
      role: defaultRole,
    });

    render(<CredentialRegistrationPanel />);

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Register security key" }));

    await waitFor(() =>
      expect(screen.getByText("Credential registered")).toBeInTheDocument(),
    );
    expect(screen.getByText("credential-123")).toBeInTheDocument();
    expect(requestChallengeMock).toHaveBeenCalledWith({ role: defaultRole });
    expect(verifyMock).toHaveBeenCalledWith({
      challengeId: "challenge-1",
      attestationResponse: attestationResponse(),
    });
    expect(showNoticeMock).toHaveBeenCalledWith(
      `Security key registered for ${defaultRole}`,
      "success",
    );
  });

  it("registers another protocol role without offering custom roles", async () => {
    requestChallengeMock.mockResolvedValue({
      challengeId: "challenge-2",
      creationOptions: creationOptions(),
    });
    runCeremonyMock.mockResolvedValue(attestationResponse());
    verifyMock.mockResolvedValue({
      credentialId: "credential-456",
      role: "host",
    });

    render(<CredentialRegistrationPanel />);

    const roleSelect = screen.getByLabelText("Approval role");
    expect(
      screen.queryByRole("option", { name: "Custom" }),
    ).not.toBeInTheDocument();
    expect(
      Array.from(roleSelect.querySelectorAll("option")).map(
        (option) => option.value,
      ),
    ).toEqual(APPROVAL_ROLE_VALUES);

    const user = userEvent.setup();
    await user.selectOptions(roleSelect, "host");
    await user.click(
      screen.getByRole("button", { name: "Register security key" }),
    );

    await waitFor(() =>
      expect(screen.getByText("credential-456")).toBeInTheDocument(),
    );
    expect(requestChallengeMock).toHaveBeenCalledWith({ role: "host" });
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

  it("keeps registration enabled for the default protocol role", async () => {
    render(<CredentialRegistrationPanel />);

    expect(
      screen.getByRole("button", { name: "Register security key" }),
    ).toBeEnabled();
    expect(requestChallengeMock).not.toHaveBeenCalled();
  });

  it("maps authenticator cancellation to a clear local error", () => {
    expect(describeRegistrationFailure(new Error("AbortError"))).toBe(
      "The security key ceremony was cancelled before a credential was registered.",
    );
  });

  it("maps broker storage failures to clear operator guidance", () => {
    expect(
      describeRegistrationFailure(
        new ApiError({
          status: 503,
          statusText: "Service Unavailable",
          bodyText: '{"error":"store_busy"}',
          errorCode: "store_busy",
          retryAfter: "2",
        }),
      ),
    ).toContain("Try again in 2 seconds");
    expect(
      describeRegistrationFailure(
        new ApiError({
          status: 507,
          statusText: "Insufficient Storage",
          bodyText: '{"error":"store_full"}',
          errorCode: "store_full",
        }),
      ),
    ).toContain("Free disk space and restart the broker");
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
