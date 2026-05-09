# Apple Developer ID Setup

Last updated: 2026-05-09 / Owner: @FranDias

This release path signs the macOS app with a Developer ID Application
certificate, notarizes with notarytool through electron-builder, staples the
`.dmg`, and publishes electron-updater's `latest-mac.yml`.

## Before You Start

- Apple Developer Program enrollment is required and costs USD 99/year.
- New organization enrollments can take 1-3 days before certificates are
  available.
- Use a Developer ID Application certificate. Do not use Developer ID Installer
  or Apple Development for this app bundle.
- `APPLE_APP_SPECIFIC_PASSWORD` is created at appleid.apple.com under Sign-In
  and Security -> App-Specific Passwords. It is not the iCloud account password.
- Look up `APPLE_TEAM_ID` at developer.apple.com -> Membership -> Team ID.

## Certificate Export

1. In Certificates, Identifiers & Profiles, create a Developer ID Application
   certificate for the WUPHF release team.
2. Install the certificate in Keychain Access on a trusted Mac.
3. In Keychain Access, expand the certificate and confirm the private key is
   present under it.
4. Right-click the certificate/private-key pair, choose Export, and save a
   password-protected `.p12`. The export must include the private key.
5. Base64-encode the export as one line:

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
| `APPLE_APP_SPECIFIC_PASSWORD` | App-specific password from appleid.apple.com |

The workflow imports the `.p12` into a temporary keychain under
`${{ runner.temp }}`, makes that keychain the default for the build, and deletes
it in an `if: always()` cleanup step. No certificate file is committed.

## Smoke Test

1. Push a rewrite release tag, for example `v0.0.1-rewrite`.
2. Approve the `production-release` environment if GitHub asks for it.
3. Open the `Release Rewrite` workflow run.
4. Confirm the macOS job creates a temporary keychain, signs, notarizes, staples
   the `.dmg`, validates the staple, refreshes `latest-mac.yml`, and verifies
   the manifest sha512.
5. Download the draft release `.dmg` and run:

   ```bash
   spctl --assess --type open --verbose "$ARTIFACT_PATH"
   ```

Successful output includes `accepted` and a Notarized Developer ID source.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `errSecInternalComponent` during codesign | Keychain locked or missing key partition list | Confirm the workflow ran `security unlock-keychain` and `security set-key-partition-list` before `bun run build:mac` |
| `The specified item could not be found in the keychain` | `.p12` export did not include the private key | Re-export the certificate/private-key pair from Keychain Access |
| `Asset validation failed (-18000)` or notarytool auth failure | Wrong Apple ID password or Team ID | Use an app-specific password and verify the Team ID under developer.apple.com -> Membership |
| `MAC verification failed` while importing `.p12` | Bad `.p12` password or base64 value has whitespace | Re-run the one-line `base64 | tr -d '\n'` command and update the secret |
| `spctl` reports `rejected` | Notarization or stapling did not complete | Check the macOS job's notarization output and `xcrun stapler validate` step |

If any Apple secret is missing on a tag push, the workflow must fail before
artifacts can publish.
