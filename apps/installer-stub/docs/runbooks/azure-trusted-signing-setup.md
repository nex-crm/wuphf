# Azure Trusted Signing Setup

Last updated: 2026-05-09 / Owner: @FranDias

Windows releases use Azure Trusted Signing through the official pinned GitHub
Action. No certificate file is exported, downloaded, or committed.

Microsoft now documents this service as Artifact Signing. The workflow still
uses the pinned `Azure/trusted-signing-action` slug until a separately reviewed
action migration lands.

Official references:

- Microsoft quickstart:
  <https://learn.microsoft.com/en-us/azure/artifact-signing/quickstart>
- Signing integrations and endpoint table:
  <https://learn.microsoft.com/en-us/azure/artifact-signing/how-to-signing-integrations>
- FAQ:
  <https://learn.microsoft.com/en-us/azure/artifact-signing/faq>

## Prerequisites

1. Use a paid Azure subscription with billing enabled. The
   `Microsoft.CodeSigning` resource provider is not supported on Free or Trial
   subscriptions.
2. Confirm the tenant and subscription:

   ```bash
   az account show --query '{subscription:id, tenant:tenantId, name:name}' -o table
   ```

3. Register the resource provider before creating accounts:

   ```bash
   az provider register --namespace Microsoft.CodeSigning
   az provider show --namespace Microsoft.CodeSigning --query registrationState -o tsv
   ```

   Continue only after the state is `Registered`.

4. Confirm the human creating the signing account has Contributor or Owner on
   the target resource group/subscription.
5. Confirm the identity-validation operator has the
   `Artifact Signing Identity Verifier` role. Microsoft disables the Azure
   portal's `New identity` action when this role is missing.
6. Public Trust identity validation is region and country limited. For public
   certificates, start the request at least 20 business days before the first
   release window; Microsoft may request more documentation.

First-time account readiness checklist:

| Check | Expected result |
|---|---|
| Subscription billing | Paid subscription, not Free/Trial |
| Resource provider | `az provider show --namespace Microsoft.CodeSigning --query registrationState -o tsv` returns `Registered` |
| Account creator role | Contributor or Owner on the target scope |
| Identity validation role | `Artifact Signing Identity Verifier` assigned before opening the identity blade |
| CI signer role | `Trusted Signing Certificate Profile Signer` assigned on the certificate profile after profile creation |

## Supported Regions And Endpoints

Store `AZURE_ENDPOINT` exactly as Microsoft lists the endpoint URI value for the
region that contains the Artifact Signing account and certificate profile. Do
not use Azure location names such as `https://eastus.codesigning.azure.net/`;
the endpoint uses Microsoft signing service short codes and normally has no
trailing slash.

| Azure region | Region class field | `AZURE_ENDPOINT` |
|---|---|---|
| Brazil South | `BrazilSouth` | `https://brs.codesigning.azure.net` |
| Central US | `CentralUS` | `https://cus.codesigning.azure.net` |
| East US | `EastUS` | `https://eus.codesigning.azure.net` |
| Japan East | `JapanEast` | `https://jpe.codesigning.azure.net` |
| Korea Central | `KoreaCentral` | `https://krc.codesigning.azure.net` |
| North Central US | `NorthCentralUS` | `https://ncus.codesigning.azure.net` |
| North Europe | `NorthEurope` | `https://neu.codesigning.azure.net` |
| Poland Central | `PolandCentral` | `https://plc.codesigning.azure.net` |
| South Central US | `SouthCentralUS` | `https://scus.codesigning.azure.net` |
| Switzerland North | `SwitzerlandNorth` | `https://swn.codesigning.azure.net` |
| West Central US | `WestCentralUS` | `https://wcus.codesigning.azure.net` |
| West Europe | `WestEurope` | `https://weu.codesigning.azure.net` |
| West US | `WestUS` | `https://wus.codesigning.azure.net` |
| West US 2 | `WestUS2` | `https://wus2.codesigning.azure.net` |
| West US 3 | `WestUS3` | `https://wus3.codesigning.azure.net` |

A region/endpoint mismatch commonly appears as `403 Forbidden` or an internal
`SignerSign()` failure during signing.

When Azure adds or removes regions, copy the exact `Endpoint URI value` from the
Microsoft signing integrations table. Do not derive the hostname from the Azure
location name and do not add a trailing slash unless Microsoft's table starts
including one.

## Provisioning

1. Create or select the resource group for release signing.
2. Create an Artifact Signing account in a supported region.
3. Complete identity validation for the subscription/account.
4. Create a Public Trust certificate profile for WUPHF installer releases.
5. Record the certificate subject common name shown by Azure. For this PR the
   placeholder expected publisher is `WUPHF (installer stub)`; replace it before
   merge if Azure issues a different CN.
6. Create an app registration for CI signing.
7. Create a client secret for the app registration and record its expiry in the
   release calendar.
8. Grant the service principal `Trusted Signing Certificate Profile Signer` on
   the certificate profile scope. Do not rely on generic Contributor access.
   Account-scope signer access works, but profile-scope is least privilege.
9. Add these GitHub Secrets in the `production-release` environment:

   | Secret | Value |
   |---|---|
   | `AZURE_TENANT_ID` | Entra tenant ID |
   | `AZURE_CLIENT_ID` | App registration client ID |
   | `AZURE_CLIENT_SECRET` | App registration client secret |
   | `AZURE_SIGNING_ACCOUNT_NAME` | Artifact Signing account name |
   | `AZURE_CERT_PROFILE_NAME` | Certificate profile name |
   | `AZURE_ENDPOINT` | Region endpoint, for example `https://eus.codesigning.azure.net` |
   | `AZURE_EXPECTED_PUBLISHER_NAME` | Expected signer certificate common name, for example `WUPHF (installer stub)` |

`AZURE_SIGNING_ACCOUNT_NAME` and `AZURE_CERT_PROFILE_NAME` are both required.
The account identifies the signing service container; the profile identifies the
certificate identity within that account.

## Workflow Behavior

The Windows job:

1. Builds the NSIS installer unsigned, **passing
   `--config.win.publisherName="$AZURE_EXPECTED_PUBLISHER_NAME"`** so the value
   baked into `app-update.yml` matches the certificate identity that will sign
   it. (electron-updater compares this baked-in publisher name against the
   downloaded installer's Authenticode CN at update time; a mismatch breaks
   auto-update silently for end users.)
2. Signs the final `.exe` AND every packaged `.dll` recursively with
   `Azure/trusted-signing-action`, retrying up to three attempts with
   30-second waits.
3. Asserts the Authenticode signature is `Valid` for every signed payload
   (`*.exe` AND `*.dll`) and that the signer CN equals
   `AZURE_EXPECTED_PUBLISHER_NAME` for each.
4. Refreshes `latest.yml` from the signed artifact bytes and uploads the
   artifact.

The `win.publisherName` placeholder in `electron-builder.yml`
(`WUPHF (installer stub)`) is intentionally kept for local PR builds, which
do not auto-update. Production releases override it via the workflow secret.

## Smoke Test

1. Push a rewrite release tag, for example `v0.0.1-rewrite`.
2. Approve the `production-release` environment if GitHub asks for it.
3. Open the `Release Rewrite` workflow run.
4. Confirm `Detect Azure signing secrets` reports all Azure values set.
5. Confirm the Windows job signs with `Azure/trusted-signing-action`, verifies
   the signer CN, refreshes `latest.yml`, and verifies the manifest.
6. Download the `.exe` from the draft release and inspect its signature:

   ```powershell
   Get-AuthenticodeSignature .\wuphf-installer-stub-0.0.1-rewrite-win-x64.exe
   ```

Success means `Status` is `Valid` and
`SignerCertificate.GetNameInfo(SimpleName, $false)` equals
`AZURE_EXPECTED_PUBLISHER_NAME`.

## Regional Fallback

The endpoint must match the account/profile region. A fallback therefore needs
an already-provisioned Artifact Signing account and certificate profile in the
fallback region, not just a different URL.

Emergency failover procedure:

1. Confirm the primary region is the failing dependency by checking the Windows
   job logs for service errors, 5xx responses, throttling, or repeated
   `SignerSign()` failures.
2. Pick the pre-provisioned fallback region. Prefer `West US 2`
   (`https://wus2.codesigning.azure.net`) or `West US 3`
   (`https://wus3.codesigning.azure.net`) for a US fallback when available.
3. Add fallback environment secrets under new names, for example
   `AZURE_ENDPOINT_WESTUS2`, `AZURE_SIGNING_ACCOUNT_NAME_WESTUS2`, and
   `AZURE_CERT_PROFILE_NAME_WESTUS2`. Add a matching
   `AZURE_EXPECTED_PUBLISHER_NAME_WESTUS2` if the fallback profile's CN differs.
4. Commit a reviewed workflow change that points the Windows signing action and
   signer assertion at the fallback secret names.
5. Move the rewrite release tag to the workflow-fix commit only if the release
   is still draft-only. If any release was published or users may have seen
   `latest*.yml`, bump to a new higher version tag instead.
6. Rerun the release workflow and verify the Authenticode CN before publishing.
7. After the incident, restore the normal secret names or promote the fallback
   region intentionally in a separate PR.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `Microsoft.CodeSigning` cannot be registered | Free/Trial subscription or missing subscription permission | Move to a paid subscription and have an Owner register the provider |
| `New identity` button is disabled | Human operator lacks `Artifact Signing Identity Verifier` | Assign the role and reopen the Azure portal blade |
| `403`, `SignerSign()`, or endpoint authorization failure | Endpoint does not match the account region, or service principal lacks signer role | Use the endpoint table above and assign `Trusted Signing Certificate Profile Signer` on the profile scope |
| `401` or client credential failure | Wrong tenant/client/secret, expired `AZURE_CLIENT_SECRET`, or wrong environment | Rotate the client secret, update the `production-release` secret, and rerun |
| Repeated 5xx, throttling, or timestamp service failure | Azure regional or service-side incident | Let the workflow retry once. If all attempts fail, use the regional fallback procedure. |
| Signature exists but signer CN assertion fails | Wrong certificate profile or expected publisher secret | Stop. Do not upload. Set `AZURE_EXPECTED_PUBLISHER_NAME` to the Azure-issued CN or switch to the intended profile. |
| Signature exists but SmartScreen warns | New publisher reputation | Keep the signature, publish checksums, and expect reputation to improve after installs |
| `latest.yml` sha512 mismatch | Signing changed the `.exe` after manifest generation | Confirm `Refresh Windows update manifest after signing` ran after signer verification |

If any Azure secret is missing on a tag push, the workflow must fail before
artifacts can publish.
