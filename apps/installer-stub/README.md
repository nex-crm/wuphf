# @wuphf/installer-stub

WUPHF v1 installer pipeline. Tiny Electron hello-world packaged by electron-builder, signed (Apple Dev ID + Azure Trusted Signing), notarized (Mac), and shipped via Sparkle (Mac) / electron-updater (Win) auto-updaters.

This stub exists *only* so we can prove the signing + auto-update story end-to-end **today**, before the real desktop shell ([`feat/desktop-shell-skeleton`](https://github.com/nex-crm/wuphf/pulls?q=is%3Apr+head%3Afeat%2Fdesktop-shell-skeleton)) lands. When that branch merges and stabilizes, the `electron-builder.yml` `files:` glob retargets to `apps/desktop/` and this stub gets deleted in the same PR.

## Run it

```bash
# Local dry-run (no signing, no installer — just a packed dir)
cd apps/installer-stub && bun run build:dry-run
# → apps/installer-stub/dist/<platform>-unpacked/

# Local real installer (signed only if local env vars present)
bun run installer:build:local
# → apps/installer-stub/dist/wuphf-installer-stub-0.0.0.dmg  (mac)
# → apps/installer-stub/dist/wuphf-installer-stub-0.0.0.exe  (win)
# → apps/installer-stub/dist/wuphf-installer-stub-0.0.0.AppImage  (linux)
```

Install the artifact, double-click it, see one window:

```
WUPHF installer-stub v0.0.0
Channel: dev
Auto-update: idle
[ Check for updates ]
```

## Test the release pipeline

```bash
# In CI, on every PR push (no secrets needed)
cd apps/installer-stub && WUPHF_RELEASE_MODE=pr bun run check:secrets
cd apps/installer-stub && WUPHF_RELEASE_MODE=pr bun run build:dry-run

# In CI, on a tag push (matrix: macos-14 / windows-2022 / ubuntu-24.04)
# Triggered by tags matching `v[0-9]+.*-rewrite`
# See: .github/workflows/release-rewrite.yml
```

## Secrets you'll need (production releases only)

| Secret | Used for | Setup |
|---|---|---|
| `APPLE_ID` | Apple notarytool auth | [docs/runbooks/apple-dev-id-setup.md](./docs/runbooks/apple-dev-id-setup.md) |
| `APPLE_TEAM_ID` | Apple notarytool team | same |
| `APPLE_APP_SPECIFIC_PASSWORD` | Apple notarytool password | same |
| `APPLE_CERT_P12_BASE64` | Developer ID cert (base64-encoded .p12) | same |
| `APPLE_CERT_PASSWORD` | .p12 unlock password | same |
| `AZURE_TENANT_ID` | Azure Trusted Signing tenant | [docs/runbooks/azure-trusted-signing-setup.md](./docs/runbooks/azure-trusted-signing-setup.md) |
| `AZURE_CLIENT_ID` | Azure Trusted Signing app | same |
| `AZURE_CLIENT_SECRET` | Azure Trusted Signing secret | same |
| `AZURE_SIGNING_ACCOUNT_NAME` | Trusted Signing account name | same |
| `AZURE_CERT_PROFILE_NAME` | Cert profile (e.g. `wuphf-prod-2026`) | same |
| `AZURE_ENDPOINT` | Trusted Signing endpoint | same |

When all are set, tag pushes produce signed + notarized + auto-updateable artifacts. When some are missing, PR pushes still produce unsigned-with-warning artifacts (CI green, useful for local testing).

macOS signing maps `APPLE_CERT_P12_BASE64` → `CSC_LINK` and `APPLE_CERT_PASSWORD` → `CSC_KEY_PASSWORD` for electron-builder. Windows signing builds first, signs the final installer output with the pinned `Azure/trusted-signing-action`, then refreshes `latest.yml` so electron-updater hashes match the signed artifact.

## Architecture

```
GitHub tag push
  └── .github/workflows/release-rewrite.yml
        ├── job: build-mac (macos-14)
        │     ├── electron-builder --mac
        │     ├── codesign + notarize + staple
        │     └── upload .dmg + .zip + latest-mac.yml
        ├── job: build-win (windows-2022)
        │     ├── electron-builder --win
        │     ├── Azure Trusted Signing
        │     ├── refresh latest.yml after signing
        │     └── upload .exe + latest.yml
        ├── job: build-linux (ubuntu-24.04)
        │     ├── electron-builder --linux
        │     └── upload .AppImage + .deb (unsigned, SHA-256 published)
        └── job: publish (after all 3)
              ├── verify-appcast.sh
              ├── verify-latest-yml.sh
              └── gh release create
```

## Read more

- [`AGENTS.md`](./AGENTS.md) — 13 hard rules (signing, secrets, reproducibility).
- [`docs/runbooks/apple-dev-id-setup.md`](./docs/runbooks/apple-dev-id-setup.md) — provisioning Apple Dev ID + notarytool.
- [`docs/runbooks/azure-trusted-signing-setup.md`](./docs/runbooks/azure-trusted-signing-setup.md) — provisioning Azure Trusted Signing.
- [`docs/runbooks/linux-distribution.md`](./docs/runbooks/linux-distribution.md) — what we sign vs. what we don't on Linux.

## RFC anchors

Distribution: §11. Branch: §15 row 3 (`feat/installer-pipeline`, week 1–3). Signed artifact end-to-end: §15 row 3 last clause.
