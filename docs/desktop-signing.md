# Desktop release signing runbook

The desktop app (`desktop/oswails`) is a fat, self-contained binary — it embeds
the UI (`web/dist`) and runs the broker in-process. So **every product release
needs a new build**: a change to `web/`, `internal/`, `desktop/oswails/`, or the
Go deps ships only when a fresh signed artifact is cut. Docs/website/test/CI
changes do not.

Automated by `.github/workflows/desktop-release.yml`. It fires on every product
release tag `v*` (the desktop app rides the main release cadence) and on a
standalone `desktop-v*` tag or manual dispatch → build → sign → notarize. On a
`v*` tag the signed dmg + Windows installer are APPENDED to the published GitHub
Release goreleaser creates for that tag; on a `desktop-v*` tag / dispatch they go
to a DRAFT to review first. The `desktop-build-check.yml` PR gate builds it
unsigned on any PR touching those paths, so a desktop-breaking change fails the
PR, not the release.

## One-time setup

### macOS (Developer ID + notarization) — required; Gatekeeper blocks unsigned downloads
- **Certificate:** Apple Developer portal → Certificates → **Developer ID
  Application** → install (`security find-identity -v -p codesigning` must show
  it). Export from Keychain Access as `.p12` (with the private key).
- **Notary key:** App Store Connect → Users and Access → Integrations → App Store
  Connect API → **Team Key, Developer role** → download the `.p8`, note Key ID +
  Issuer ID. (Use the **Developer account** Apple ID, not your Mac login.)

Repo secrets (Settings → Secrets and variables → Actions):

| Secret | Value |
|---|---|
| `APPLE_CERT_P12_BASE64` | `base64 -i DeveloperID.p12` |
| `APPLE_CERT_PASSWORD` | the `.p12` export password |
| `APPLE_SIGN_IDENTITY` | `Developer ID Application: GarageSpace, Inc. (GXAA6X232R)` |
| `APPLE_TEAM_ID` | `GXAA6X232R` |
| `APPLE_NOTARY_KEY_P8_BASE64` | `base64 -i AuthKey_XXX.p8 \| gh secret set APPLE_NOTARY_KEY_P8_BASE64` |
| `APPLE_NOTARY_KEY_ID` | the Key ID |
| `APPLE_NOTARY_ISSUER_ID` | the Issuer ID |
| `KEYCHAIN_PASSWORD` | any random string (ephemeral CI keychain) |

### Windows (Azure Trusted Signing) — optional; without it the installer ships unsigned
GarageSpace is a company, so **Azure Trusted Signing** (~$10/mo, cloud, instant
SmartScreen trust) is the recommended path over an EV/OV cert. Set up a Trusted
Signing account + certificate profile, then add: `AZURE_TENANT_ID`,
`AZURE_CLIENT_ID`, `AZURE_CLIENT_SECRET`, `AZURE_TS_ENDPOINT`, `AZURE_TS_ACCOUNT`,
`AZURE_TS_PROFILE`. The release job signs only when these exist.

## Cut a release

The desktop app rides the main `v*` cadence, so every product release already
cuts a signed dmg + Windows installer and appends them to that tag's GitHub
Release — no separate step. For an out-of-band desktop-only build:

```bash
git tag desktop-v0.1.1 && git push origin desktop-v0.1.1
# → desktop-release.yml builds, signs, notarizes, and drafts a GitHub Release.
# Review the draft, then publish. Link the .dmg / .exe on the website.
```

## Manual macOS build (local, validated 2026-06-14)

```bash
cd web && bun run build && cd ..
cd desktop/oswails && wails build -s -skipbindings -tags desktop && cd ../..
APP=desktop/oswails/build/bin/WUPHF.app
codesign --deep --force --options runtime --timestamp \
  --entitlements desktop/oswails/build/darwin/entitlements.plist \
  --sign "Developer ID Application: GarageSpace, Inc. (GXAA6X232R)" "$APP"
mkdir -p dist
stage=$(mktemp -d); cp -R "$APP" "$stage/"; ln -s /Applications "$stage/Applications"
hdiutil create -volname WUPHF -srcfolder "$stage" -ov -format UDZO dist/WUPHF.dmg
codesign --sign "Developer ID Application: GarageSpace, Inc. (GXAA6X232R)" --timestamp dist/WUPHF.dmg
xcrun notarytool submit dist/WUPHF.dmg --keychain-profile wuphf-notary --wait
xcrun stapler staple dist/WUPHF.dmg
spctl -a -t open --context context:primary-signature -v dist/WUPHF.dmg   # → accepted: Notarized Developer ID
```

## Gotchas
- WKWebView JITs JS → the hardened runtime needs `com.apple.security.cs.allow-jit`
  + `allow-unsigned-executable-memory` (`build/darwin/entitlements.plist`), else
  notarization or launch fails.
- `CFBundleShortVersionString` is stamped from the tag in CI; locally it's
  whatever `wails.json` `info.productVersion` says.
- Notarization needs the **Developer account** Apple ID / team, never the Mac
  login Apple ID.
