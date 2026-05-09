# Azure Trusted Signing Setup

Windows releases use Azure Trusted Signing through the official pinned GitHub
Action. No certificate file is exported, downloaded, or committed.

## Provisioning

1. Create or select an Azure subscription for release signing.
2. Create a Trusted Signing account in the target Azure region.
3. Complete identity validation for the account.
4. Create a certificate profile for WUPHF installer releases.
5. Create an app registration for CI signing.
6. Create a client secret for the app registration.
7. Grant the service principal the Trusted Signing Certificate Profile Signer
   role on the signing account or certificate profile scope.
8. Add these GitHub Secrets in the release environment:

   | Secret | Value |
   |---|---|
   | `AZURE_TENANT_ID` | Entra tenant ID |
   | `AZURE_CLIENT_ID` | App registration client ID |
   | `AZURE_CLIENT_SECRET` | App registration client secret |
   | `AZURE_SIGNING_ACCOUNT_NAME` | Trusted Signing account name |
   | `AZURE_CERT_PROFILE_NAME` | Certificate profile name |
   | `AZURE_ENDPOINT` | Trusted Signing endpoint for the region |

`AZURE_SIGNING_ACCOUNT_NAME` is required by `Azure/trusted-signing-action`; the
certificate profile alone does not identify the account.

## Smoke Test

1. Push a rewrite release tag, for example `v0.0.1-rewrite`.
2. Open the `Release Rewrite` workflow run.
3. Confirm `detect-secrets` reports all Azure values set.
4. Confirm the Windows job builds the NSIS installer, signs with
   `Azure/trusted-signing-action`, refreshes `latest.yml`, and verifies the
   manifest.
5. Download the `.exe` from the draft release and inspect its signature:

   ```powershell
   Get-AuthenticodeSignature .\wuphf-installer-stub.exe
   ```

If any Azure secret is missing on a tag push, the workflow must fail before
artifacts can publish.
