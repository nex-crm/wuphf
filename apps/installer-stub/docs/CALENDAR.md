# Installer Release Calendar

Last updated: 2026-05-09 / Owner: @FranDias

This file records release-pipeline dates that must survive staff handoffs.

## Apple Developer ID Application Certificate

- Owner: @FranDias
- Backup owner: unassigned
- Current subject: `Developer ID Application: <record exact subject>`
- Current serial: `<record serial>`
- Current `notAfter`: `<record yyyy-mm-dd>`
- Renewal lead time: start at least 60 days before `notAfter`
- Renewal runbook: `runbooks/apple-dev-id-setup.md#cert-renewal`

The owner is responsible for keeping this section current whenever
`APPLE_CERT_P12_BASE64` is rotated in the `production-release` GitHub
environment.

## Scheduled Maintenance

| Date | Owner | Item | Runbook |
|---|---|---|---|
| 2026-06-01 | @FranDias | Review `macos-14` release runner deprecation and test successor image | `runbooks/runner-image-maintenance.md` |
| 2026-06-01 | @FranDias | Plan Electron/electron-builder/electron-updater stack bump | `runbooks/electron-stack-maintenance.md` |
| Quarterly | @FranDias | Review Apple cert expiry, Azure identity validation, Azure client secret expiry, and signing owner backups | Apple/Azure runbooks |
