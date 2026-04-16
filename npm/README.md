# wuphf

The pixel office CRM that reaches everyone, everywhere, all at once.

## Install

```bash
npx wuphf
```

Or install globally:

```bash
npm install -g wuphf
wuphf
```

Supported platforms: macOS and Linux on x64 or arm64.

## How it works

This package is a thin Node wrapper around the native `wuphf` Go binary.
On install (or on first run, if postinstall was skipped), it downloads
the matching release archive from
[github.com/nex-crm/wuphf/releases](https://github.com/nex-crm/wuphf/releases)
and places the binary in `node_modules/wuphf/bin/wuphf`.

To point the wrapper at a local build, set `WUPHF_BINARY`:

```bash
WUPHF_BINARY=./wuphf npx wuphf --version
```

## Links

- Source: https://github.com/nex-crm/wuphf
- Issues: https://github.com/nex-crm/wuphf/issues
