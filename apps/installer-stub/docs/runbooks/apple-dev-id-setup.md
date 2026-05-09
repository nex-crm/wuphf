# Apple Developer ID Setup

This release path signs the macOS app with Developer ID Application, notarizes
with notarytool, and staples the resulting artifacts before upload.

## Provisioning

1. Enroll the release owner in the Apple Developer Program.
2. In Certificates, Identifiers & Profiles, create a Developer ID Application
   certificate for the WUPHF release team.
3. Install the certificate in Keychain Access and export it as a password
   protected `.p12`.
4. Base64-encode the export as one line:

   ```bash
   base64 -i "$CERT_PATH" | tr -d '\n'
   ```

5. Add these GitHub Secrets in the release environment:

   | Secret | Value |
   |---|---|
   | `APPLE_CERT_P12_BASE64` | One-line base64 value from the exported certificate |
   | `APPLE_CERT_PASSWORD` | Password used for the certificate export |
   | `APPLE_ID` | Apple ID email used for notarization |
   | `APPLE_TEAM_ID` | Team ID from the Apple developer account |
   | `APPLE_APP_SPECIFIC_PASSWORD` | App-specific password for notarytool |

The workflow maps `APPLE_CERT_P12_BASE64` to electron-builder's `CSC_LINK` and
`APPLE_CERT_PASSWORD` to `CSC_KEY_PASSWORD`. No certificate file is committed or
checked into the workspace.

## Smoke Test

1. Push a rewrite release tag, for example `v0.0.1-rewrite`.
2. Open the `Release Rewrite` workflow run.
3. Confirm `detect-secrets` reports all Apple values set.
4. Confirm the macOS job signs, notarizes, staples, and validates the `.dmg` and
   `.zip` artifacts.
5. Download the draft release artifact and run:

   ```bash
   spctl --assess --type open --verbose "$ARTIFACT_PATH"
   ```

If any Apple secret is missing on a tag push, the workflow must fail before
artifacts can publish.
