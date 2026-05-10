# Release Day Troubleshooting

Last updated: 2026-05-09 / Owner: @FranDias

Use this runbook for failures in `.github/workflows/release-rewrite.yml`.

**Never delete or replace a published release. Delete drafts only.** If a
published release or any `latest*.yml` manifest may have been seen by users,
ship a new higher version tag and fix forward.

## First Checks

1. Open GitHub Actions -> `Release Rewrite` -> the failed run.
2. Identify the failed job: `detect-secrets`, `build-mac`, `build-win`,
   `build-linux`, or `publish`.
3. Open the failed step logs and copy the first real error, not the final
   process-exit line.
4. Check whether a GitHub draft release exists for the tag:

   ```bash
   gh release view "$TAG" --json isDraft,assets
   ```

5. If the release is draft, reruns and `gh release upload --clobber` can repair
   missing assets. If the release is published, do not clobber it.

## Common Failures

| Failed job or step | Likely cause | Safe recovery |
|---|---|---|
| `Detect Apple signing secrets` | Missing Apple environment secret | Add the missing `production-release` secret, rerun the failed job |
| `Prepare macOS signing keychain` | Bad `.p12` base64 or password | Re-export the certificate/private-key pair, update `APPLE_CERT_P12_BASE64` / `APPLE_CERT_PASSWORD`, rerun |
| `Build signed macOS artifacts` waits or fails in notarytool | Apple notary queue, network failure, auth failure, or invalid binary | Use the notary section below |
| `Staple and validate macOS DMG` | Notarization did not complete for the DMG bytes | Rerun the macOS job if the release is still draft; if repeated, inspect notary logs and rebuild |
| `Detect Azure signing secrets` | Missing Azure environment secret | Add the missing `production-release` secret, rerun |
| `Sign Windows artifacts` returns `401` | Wrong tenant/client/secret or expired client secret | Rotate `AZURE_CLIENT_SECRET`, update the environment secret, rerun |
| `Sign Windows artifacts` returns `403` or `SignerSign()` | Endpoint/account region mismatch or missing profile signer role | Use the endpoint table in the Azure runbook and confirm `Trusted Signing Certificate Profile Signer` on the certificate profile |
| `Assert Windows signer identity` fails | Wrong Azure cert profile or stale expected publisher secret | Stop before publish. Set `AZURE_EXPECTED_PUBLISHER_NAME` to the Azure-issued signer CN or switch to the intended profile |
| `Sign Windows artifacts` returns 5xx, throttling, or timestamp failures | Azure service or regional incident | Let the three attempts finish. If they all fail, use regional fallback in the Azure runbook |
| `Assert release asset set` | One platform job did not upload all expected artifacts | Rerun or fix that platform job before rerunning publish |
| `Verify release update manifests` | Manifest sha512/size does not match final signed bytes | Confirm the post-sign/post-staple refresh step ran after signing, then rebuild that platform |
| `gh release create` fails with missing tag | The remote tag was deleted or the workflow was run on the wrong ref | Recreate the intended tag at the reviewed commit or bump to a new version tag |
| `gh release upload` fails, returns 5xx, or leaves partial assets | GitHub API outage/rate limit or network failure | Keep the release draft and rerun publish. The workflow uses `--clobber`, asserts all expected assets are present, then publishes automatically |

## Apple Notarytool Timeout

The macOS job has a 60-minute timeout and the notary preload wrapper retries
transient notarytool failures with 1, 5, and 15 minute backoffs. If the job still
times out:

1. Confirm whether Apple accepted a submission ID in the build log.
2. If there is a submission ID, fetch diagnostics:

   ```bash
   xcrun notarytool log "$SUBMISSION_ID" \
     --apple-id "$APPLE_ID" \
     --team-id "$APPLE_TEAM_ID" \
     --password "$APPLE_APP_SPECIFIC_PASSWORD" \
     developer_log.json
   ```

3. Check Apple Developer System Status for notarization incidents.
4. If the release is still a draft, rerun `build-mac`. Reruns submit a new
   notarization request and may consume more Apple queue time.
5. If the same tag needs a workflow fix, move the tag only while the release is
   still draft-only. If the release was published, ship a new higher version.

## Partial Draft Release Upload

The publish job creates or updates a draft release, uploads all expected assets,
checks GitHub's release asset inventory, and then flips the verified draft to a
published release automatically. Expected assets are:

- `.dmg`
- `.zip`
- `.exe`
- `.AppImage`
- `.deb`
- `latest-mac.yml`
- `latest.yml`
- `latest-linux.yml`
- `release-checksums.txt`

If upload fails midway:

1. Confirm the release is still draft:

   ```bash
   gh release view "$TAG" --json isDraft -q '.isDraft'
   ```

2. Rerun the failed publish job or the full workflow. The publish job uses
   `gh release upload --clobber`, so existing draft assets are overwritten with
   the freshly downloaded build artifacts.
3. Do not manually upload a subset unless GitHub Actions is unavailable and the
   release manager can verify all asset hashes against `release-checksums.txt`.
4. Do not manually publish the draft. The workflow publishes it automatically
   after the post-upload asset assertion passes.

## Published Release Already Exists

The publish job refuses to update a published release. Do not delete the
published release to make a rerun pass. A published release may already be
referenced by updater manifests, user downloads, or support docs.

Safe paths:

- If the release is correct, leave it alone and close the failed rerun as a
  duplicate.
- If the release is bad but already published, bump to a new higher version tag
  with fixed artifacts.
- If the release is draft-only and never exposed to users, delete the draft or
  rerun publish to clobber draft assets.

## Tag Re-Issuance

Use this only for draft-only rewrite releases where no user has seen the
manifest or artifacts.

If you need to re-issue a tag, first cancel any in-progress workflow runs for
the old tag in the Actions UI and wait until no old `publish` job can run. The
publish job also verifies that the tag still points at the workflow commit, but
human cancellation is the first containment step.

1. Cancel all in-progress `Release Rewrite` runs for the old tag in GitHub
   Actions.
2. Delete the draft release in GitHub.
3. Move the local tag to the reviewed fix commit:

   ```bash
   git fetch origin
   git tag -f "$TAG" "$FIX_COMMIT"
   git push origin ":refs/tags/$TAG"
   git push origin "$TAG"
   ```

4. Watch the new `Release Rewrite` workflow run from the start.
5. Re-verify all platform artifacts and let the workflow publish the draft only
   after the asset assertion passes.

If the release was published, do not move the tag. Create a new higher tag.

## Escalation

- Release manager: @FranDias
- Apple Developer ID owner: @FranDias
- Azure signing/billing owner: @FranDias until a named backup is assigned
- GitHub admin for protected environments/releases: @FranDias

Escalate immediately when the failure blocks a time-sensitive security update,
requires changing signing identity, or involves a published release that clients
may already have consumed.
