import {
  apiBootstrapFromJson,
  apiBootstrapToJson,
  approvalSubmitRequestFromJson,
  asIdempotencyKey,
  isAllowedLoopbackHost,
  isLoopbackRemoteAddress,
  isStreamEventKind,
  isWsFrameType,
  MAX_WEBAUTHN_ASSERTION_FIELD_BYTES,
  STREAM_EVENT_KIND_VALUES,
  signedApprovalTokenToJsonValue,
  validateApprovalSubmitRequest,
  WS_FRAME_TYPE_VALUES,
} from "../../src/index.ts";
import { buildValidReceipt } from "./fixtures.ts";
import { expectEqual, expectThrows, header, nonNull } from "./harness.ts";

export function runIpcScenarios(): void {
  // ────────────────────────────────────────────────────────────────────────
  header(15, "ApiBootstrap codec: snake_case wire ↔ camelCase TS");
  // ────────────────────────────────────────────────────────────────────────
  // Wire JSON is snake_case (v0 contract: `{ token, broker_url }`); the TS
  // runtime surface is camelCase, enforced by `style/useNamingConvention`.
  // The codec functions are the only place those two shapes meet.
  const bootstrapWire = { token: "tok-bootstrap-demo-abc", broker_url: "http://127.0.0.1:54321" };
  const bootstrapTs = apiBootstrapFromJson(bootstrapWire);
  expectEqual("decoded brokerUrl (camelCase)", bootstrapTs.brokerUrl, "http://127.0.0.1:54321");
  expectEqual("round-trip back to wire shape", apiBootstrapToJson(bootstrapTs), {
    token: "tok-bootstrap-demo-abc",
    broker_url: "http://127.0.0.1:54321",
  });
  expectThrows(
    () =>
      apiBootstrapFromJson({
        token: "tok-bootstrap-demo-abc",
        brokerUrl: "http://127.0.0.1:54321",
      }),
    /broker_url|brokerUrl/,
  );

  // ────────────────────────────────────────────────────────────────────────
  header(16, "Loopback gate rejects DNS-rebinding probes");
  // ────────────────────────────────────────────────────────────────────────
  expectEqual("Host Localhost:3000 allowed", isAllowedLoopbackHost("Localhost:3000"), true);
  expectEqual(
    "Host localhost.evil.com rejected",
    isAllowedLoopbackHost("localhost.evil.com"),
    false,
  );
  expectEqual(
    "Remote IPv4-mapped loopback allowed",
    isLoopbackRemoteAddress("::ffff:127.0.0.1"),
    true,
  );
  expectEqual("Remote with port rejected", isLoopbackRemoteAddress("127.0.0.1:1234"), false);

  // ────────────────────────────────────────────────────────────────────────
  header(17, "IPC wire codecs pin WebAuthn approval tokens and runtime guards");
  // ────────────────────────────────────────────────────────────────────────
  const validReceipt = buildValidReceipt();
  const validToken = nonNull(validReceipt.approvals[0], "validReceipt.approvals[0]").signedToken;
  const goodReq = {
    receiptId: validReceipt.id,
    approvalToken: validToken,
    idempotencyKey: asIdempotencyKey("submit-01"),
  };
  const approvalSubmitWire = {
    receiptId: validReceipt.id,
    idempotencyKey: "submit-01",
    approvalToken: signedApprovalTokenToJsonValue(validToken),
  };
  const decodedSubmit = approvalSubmitRequestFromJson(approvalSubmitWire);
  expectEqual(
    "approvalSubmitRequestFromJson preserves caller-supplied notBefore",
    decodedSubmit.approvalToken.notBefore,
    validToken.notBefore,
  );
  expectEqual("decoded submit request validates", validateApprovalSubmitRequest(decodedSubmit), {
    ok: true,
  });
  expectEqual(
    "oversized assertion signature rejected before regex",
    validateApprovalSubmitRequest({
      ...goodReq,
      approvalToken: {
        ...validToken,
        signature: {
          ...validToken.signature,
          signature: "A".repeat(MAX_WEBAUTHN_ASSERTION_FIELD_BYTES + 1),
        },
      },
    }),
    {
      ok: false,
      reason:
        "approvalToken/signature/signature: approvalToken/signature/signature bytes exceeds budget: 16385 > 16384",
    },
  );
  expectEqual(
    "malformed WebAuthn assertion rejected",
    validateApprovalSubmitRequest({
      ...goodReq,
      approvalToken: {
        ...validToken,
        signature: {
          ...validToken.signature,
          signature: "not base64!",
        },
      },
    }),
    {
      ok: false,
      reason:
        "approvalToken/signature/signature: must be a canonical non-empty unpadded base64url string",
    },
  );
  expectEqual(
    "stream event guard accepts tuple values",
    STREAM_EVENT_KIND_VALUES.every(isStreamEventKind),
    true,
  );
  expectEqual(
    "stream event guard rejects unknown value",
    isStreamEventKind("receipt.deleted"),
    false,
  );
  expectEqual(
    "WS frame guard accepts tuple values",
    WS_FRAME_TYPE_VALUES.every(isWsFrameType),
    true,
  );
  expectEqual("WS frame guard rejects unknown value", isWsFrameType("close"), false);
}
