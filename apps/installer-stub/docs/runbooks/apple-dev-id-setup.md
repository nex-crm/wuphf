# Apple Developer ID Setup

Last updated: 2026-05-09 / Owner: @FranDias

This release path signs the macOS app with a Developer ID Application
certificate, notarizes with notarytool through electron-builder, staples the
`.app` inside the updater `.zip` plus the `.dmg`, and publishes
electron-updater's `latest-mac.yml`.

## Before You Start

- Apple Developer Program enrollment is required and costs USD 99/year.
- Existing paid memberships can usually provision a Developer ID certificate in
  one session. New organization enrollments can take a week or more depending
  on D-U-N-S, legal authority, and document verification.
- Creating Developer ID certificates requires the Account Holder role, or a
  delegated cloud-managed Developer ID certificate admin.
- Use a Developer ID Application certificate. Do not use Developer ID Installer
  or Apple Development for this app bundle.
- `APPLE_APP_SPECIFIC_PASSWORD` is created at account.apple.com under Sign-In
  and Security -> App-Specific Passwords. It is not the iCloud account password.
- Look up `APPLE_TEAM_ID` at developer.apple.com -> Membership -> Team ID.

## Certificate Export

1. In Certificates, Identifiers & Profiles, create a certificate signing request
   from Keychain Access on the trusted Mac.
2. Create a Developer ID Application certificate for the WUPHF release team and
   upload the CSR when Apple asks for it.
3. Install the certificate in Keychain Access on the trusted Mac.
4. In Keychain Access, expand the certificate and confirm the private key is
   present under it.
5. Right-click the certificate/private-key pair, choose Export, and save a
   password-protected `.p12`. The export must include the private key.
6. Base64-encode the export as one line:

   ```bash
   base64 -i "$CERT_PATH" | tr -d '\n'
   ```

## GitHub Secrets

Create these as environment secrets in the `production-release` GitHub
environment, not as repository-wide secrets:

| Secret | Value |
|---|---|
| `APPLE_CERT_P12_BASE64` | One-line base64 value from the exported `.p12` |
| `APPLE_CERT_PASSWORD` | Password used for the `.p12` export |
| `APPLE_ID` | Apple ID email used for notarization |
| `APPLE_TEAM_ID` | 10-character Apple Developer Team ID |
| `APPLE_APP_SPECIFIC_PASSWORD` | App-specific password from account.apple.com |

The workflow imports the `.p12` into a temporary keychain under
`${{ runner.temp }}`, makes that keychain the default for the build, and deletes
it in an `if: always()` cleanup step. No certificate file is committed.

## Smoke Test

1. Push a rewrite release tag, for example `v0.0.1-rewrite`.
2. Approve the `production-release` environment if GitHub asks for it.
3. Open the `Release Rewrite` workflow run.
4. Confirm the macOS job creates a temporary keychain, signs, notarizes, staples
   the `.app` and `.dmg`, validates both staples, refreshes `latest-mac.yml`,
   and verifies the manifest sha512.
5. Download the draft release `.dmg` and run:

   ```bash
   spctl --assess --type open --verbose "$ARTIFACT_PATH"
   ```

Successful output includes `accepted` and a Notarized Developer ID source.

## Cert Renewal

Apple documents Developer ID Application certificates as valid for five years.
Existing apps signed while the cert was valid can continue to install and run,
but new WUPHF updates need a new certificate after expiry.

Check the current certificate expiry on a trusted Mac:

```bash
security find-certificate -c "Developer ID Application: <name>" -p \
  | openssl x509 -noout -dates
```

Track the certificate subject, serial, and `notAfter` date in
`../CALENDAR.md`. The Apple Developer ID owner must start renewal at least 60
days before `notAfter`.

Renewal procedure:

1. Create or select the renewed Developer ID Application certificate in the
   Apple Developer portal.
2. Install it in Keychain Access and confirm the private key is present.
3. Re-export the certificate/private-key pair as a password-protected `.p12`.
4. Re-base64 it as one line:

   ```bash
   base64 -i "$CERT_PATH" | tr -d '\n'
   ```

5. Update GitHub environment secrets `APPLE_CERT_P12_BASE64` and
   `APPLE_CERT_PASSWORD` in `production-release`.
6. Run a signed rewrite smoke tag and verify `spctl` accepts the `.dmg`.
7. Update `../CALENDAR.md` with the new subject, serial, and `notAfter`.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `errSecInternalComponent` during codesign | Keychain locked or missing key partition list | Confirm the workflow ran `security unlock-keychain` and `security set-key-partition-list` before `bun run build:mac` |
| `The specified item could not be found in the keychain` | `.p12` export did not include the private key | Re-export the certificate/private-key pair from Keychain Access |
| `Asset validation failed (-18000)` or notarytool auth failure | Wrong Apple ID password or Team ID | Use an app-specific password and verify the Team ID under developer.apple.com -> Membership |
| `MAC verification failed` while importing `.p12` | Bad `.p12` password or base64 value has whitespace | Re-run the one-line <code>base64 | tr -d '\n'</code> command and update the secret |
| `errSecAuthFailed` or `errSecInvalidPasswordRef` while importing `.p12` | Bad `.p12` password or keychain import auth failure | Re-export with a known password and update `APPLE_CERT_PASSWORD` |
| `notarytool` status `Invalid` | Apple rejected the signed payload | Capture the submission ID from the build log and run `xcrun notarytool log <submission-id> --apple-id "$APPLE_ID" --team-id "$APPLE_TEAM_ID" --password "$APPLE_APP_SPECIFIC_PASSWORD" developer_log.json` |
| `spctl` reports `rejected` | Notarization or stapling did not complete | Check the macOS job's notarization output and `xcrun stapler validate` step |

If any Apple secret is missing on a tag push, the workflow must fail before
artifacts can publish.
