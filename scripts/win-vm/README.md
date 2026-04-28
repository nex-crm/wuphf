# Windows VM harness

Local cross-platform test harness for verifying `wuphf` on Windows ARM64
without leaving the Mac. Built for the Product Hunt launch — Mac (this
machine) and Linux (CI) we can verify directly; Windows needs a VM.

> **CI does not use this harness.** Automated Windows coverage runs on
> GitHub-hosted `windows-latest` runners (see `.github/workflows/ci.yml` —
> the `windows-smoke`, `go-cross-build`, and `release-build` jobs). This
> directory is for hands-on local triage on Apple Silicon when CI flags
> something you want to reproduce interactively. Don't try to wire it into
> Actions; the VM bring-up steps require macOS host tooling (UTM,
> AppleScript, hdiutil) that no hosted runner provides.

## What lives here

| File | Purpose |
|---|---|
| `autounattend.xml` | Win11 ARM64 setup answer file. Zero-prompt install of a `wuphf` local user with OpenSSH listening on :22, hostname `WUPHF-DEV`. |
| `autounattend.img` | ISO 9660/Joliet image wrapping `autounattend.xml` (plus an optional drivers payload), with the `.img` extension UTM expects for USB media. Attach as a USB drive before booting Windows setup so it auto-detects the answer file. Regenerate with `make-answer-disk.sh`. |
| `make-answer-disk.sh` | Rebuild `autounattend.img` from `autounattend.xml`. Re-run after editing the XML. |
| `build.sh` | Cross-compile `wuphf.exe` for windows/{amd64,arm64} on this Mac with the goreleaser ldflags. |
| `push-and-test.sh` | After `build.sh`, push the binary into the running UTM VM via SSH and run smoke tests. |

## VM identifiers

- UTM VM name: `Wuphf-Win11-ARM`
- UTM UUID: `BD57713D-0B79-4B87-AD08-88A9BF7922CC`
- Hostname inside the VM: `WUPHF-DEV`
- Local account: `wuphf` / `office`
- SSH listens on `:22` over the QEMU shared NAT — reachable from the host
  via the VM's allocated IP (use `utmctl ip-address <UUID>`).

## First-boot recovery

If Windows setup ever asks anything, the answer file failed. Common causes:

1. **Answer disk not attached as USB.** Confirm `autounattend.img` is in
   the VM's drive list as a USB *disk* (not CD). Win setup ignores
   `autounattend.xml` on optical media unless it's at the root of the
   install ISO itself.
2. **Answer-disk image stale.** This harness's `make-answer-disk.sh` builds
   ISO 9660/Joliet media (FAT broke driver-signature validation in 24H2,
   producing "Error scanning for drivers" on otherwise-valid INFs). Re-run
   the script after any edit to `autounattend.xml` so the attached `.img`
   matches what's on disk.
3. **Architecture string wrong.** All `processorArchitecture="arm64"` for
   Win11-on-Apple-Silicon. amd64/x86 there will silently fail.

## Working with the VM headlessly from the Mac

```sh
# Boot
/Applications/UTM.app/Contents/MacOS/utmctl start BD57713D-0B79-4B87-AD08-88A9BF7922CC

# Get the IP once Windows is up
/Applications/UTM.app/Contents/MacOS/utmctl ip-address BD57713D-0B79-4B87-AD08-88A9BF7922CC

# Push a file (uses the UTM guest agent — guest tools must be installed).
# Quote the destination — POSIX shell would otherwise eat the backslashes.
/Applications/UTM.app/Contents/MacOS/utmctl file push <UUID> ./wuphf.exe 'C:\wuphf\wuphf.exe'

# Or via SSH (preferred — works without UTM guest agent)
ssh wuphf@<vm-ip> 'C:\wuphf\wuphf.exe --version'

# Stop
/Applications/UTM.app/Contents/MacOS/utmctl stop BD57713D-0B79-4B87-AD08-88A9BF7922CC
```

## Iteration loop

1. Edit Go code on the Mac.
2. `./scripts/win-vm/build.sh` — produces `dist/windows/{amd64,arm64}/wuphf.exe`.
3. `./scripts/win-vm/push-and-test.sh` — copies the arm64 binary into the
   running VM and runs smoke tests over SSH. Reports failures with full
   stderr.
4. Fix Go code; repeat from step 1.
