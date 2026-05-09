# Azure Trusted Signing Setup

Last updated: 2026-05-09 / Owner: @FranDias

Windows releases use Azure Trusted Signing through the official pinned GitHub
Action. No certificate file is exported, downloaded, or committed.

## Before You Start

- Identity validation usually takes 1-7 business days and can take longer for
  some organizations. Start before the release window.
- Azure Trusted Signing is GA, but availability is region-limited. Pick a
  supported region such as `eastus`, `eastus2`, `westus2`, or `westus3` before
  creating the account.
- Budget for per-signature cost. As of this runbook date, the public price is
  about USD 0.0023 per signature, small but non-zero.
- First releases may still trigger SmartScreen reputation warnings. A valid
  signature proves publisher identity; SmartScreen reputation builds over time.

## Provisioning

1. Create or select an Azure subscription for release signing.
2. Create a Trusted Signing account in the target Azure region. This is the
   organization-wide signing account.
3. Complete identity validation for the account.
4. Create a certificate profile for WUPHF installer releases. The profile is the
   per-product certificate identity used by the action.
5. Create an app registration for CI signing.
6. Create a client secret for the app registration.
7. Grant the service principal `Trusted Signing Certificate Profile Signer` on
   the certificate profile scope. Do not rely on generic Contributor access.
   Account-scope signer access works, but profile-scope is least privilege.
8. Add these GitHub Secrets in the `production-release` environment:

   | Secret | Value |
   |---|---|
   | `AZURE_TENANT_ID` | Entra tenant ID |
   | `AZURE_CLIENT_ID` | App registration client ID |
   | `AZURE_CLIENT_SECRET` | App registration client secret |
   | `AZURE_SIGNING_ACCOUNT_NAME` | Trusted Signing account name |
   | `AZURE_CERT_PROFILE_NAME` | Certificate profile name |
   | `AZURE_ENDPOINT` | Region endpoint, for example `https://eastus.codesigning.azure.net/` |

`AZURE_SIGNING_ACCOUNT_NAME` and `AZURE_CERT_PROFILE_NAME` are both required.
The account identifies the signing service container; the profile identifies the
certificate identity within that account.

## Workflow Behavior

The Windows job builds the NSIS installer unsigned, signs the final `.exe` and
packaged `.dll` files with `Azure/trusted-signing-action`, refreshes
`latest.yml` from the signed artifact bytes, and verifies the manifest sha512
before upload.

## Smoke Test

1. Push a rewrite release tag, for example `v0.0.1-rewrite`.
2. Approve the `production-release` environment if GitHub asks for it.
3. Open the `Release Rewrite` workflow run.
4. Confirm `Detect Azure signing secrets` reports all Azure values set.
5. Confirm the Windows job signs with `Azure/trusted-signing-action`, refreshes
   `latest.yml`, and verifies the manifest.
6. Download the `.exe` from the draft release and inspect its signature:

   ```powershell
   Get-AuthenticodeSignature .\wuphf-installer-stub-0.0.1-rewrite-win-x64.exe
   ```

Success means `Status` is `Valid` and `SignerCertificate.Subject` contains the
expected WUPHF organization identity.

## Plan B

If Trusted Signing is unavailable in the required region or identity validation
misses the release window, use an EV code-signing certificate from Sectigo or
DigiCert with signtool. That swap must be a separate reviewed workflow change:
store no certificate files in the repo, scope any token or HSM credential to
`production-release`, refresh `latest.yml` after signing, and keep the same
manifest verifier.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `403` or authorization failure | Service principal lacks `Trusted Signing Certificate Profile Signer` | Assign the role on the certificate profile scope |
| Endpoint not found | Region endpoint is wrong | Use `https://<region>.codesigning.azure.net/` for the account region |
| Signature exists but SmartScreen warns | New publisher reputation | Keep the signature, publish checksums, and expect reputation to improve after installs |
| `latest.yml` sha512 mismatch | Signing changed the `.exe` after manifest generation | Confirm `Refresh Windows update manifest after signing` ran before verification |

If any Azure secret is missing on a tag push, the workflow must fail before
artifacts can publish.
