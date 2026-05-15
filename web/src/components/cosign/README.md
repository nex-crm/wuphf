# WebAuthn Cosign Components

This module owns the renderer-only WebAuthn approval surfaces for Branch 12.

- `CosignPrompt` renders the exact `ApprovalClaim` and `ApprovalScope`, starts
  the assertion ceremony, and shows the accepted token or threshold progress.
- `CredentialRegistrationPanel` is the standalone settings surface for binding a
  browser WebAuthn credential to an approval role.
- `web/src/api/webauthn.ts` is the only place that knows the four broker route
  shapes and the `@simplewebauthn/browser` ceremony wrapper types.

The renderer never proxies WebAuthn through Electron IPC. The browser ceremony
runs through `navigator.credentials`, and the resulting JSON response is sent
to the broker over the loopback HTTP client.
