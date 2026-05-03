# Share WUPHF With a Co-founder

This tutorial is for the first real customer profile for co-founder sharing:

- **Who:** two technical founders already using WUPHF locally.
- **Situation:** the host founder has WUPHF running and wants their co-founder in the same office for one working session.
- **Network:** both laptops are already on the same Tailscale or WireGuard network.
- **Goal:** the co-founder opens one invite link, lands in `#general`, and can work with the same agents.

This is not a public tunnel flow. WUPHF keeps the broker token on the host machine and only serves the shared office through a private-network listener.

## Single-user remote access

If WUPHF is running on your own server and you are still the only user, do not use `wuphf share` as your primary access path. Keep the normal host web UI and reach it through infrastructure you control, such as SSH port forwarding, Tailscale, WireGuard, or your own LAN tunnel.

Use `wuphf share` when you want to mint a separate co-founder browser session. That session is scoped: it can join the office, send messages, answer requests, and read shared context, but it does not receive the host broker token or private notebook access.

## Host founder

Start WUPHF as usual:

```bash
npx wuphf
```

In a second terminal, start sharing:

```bash
wuphf share
```

Expected output:

```text
WUPHF share
Private network: tailscale0 100.82.14.6
Public bind: blocked
Invite: http://100.82.14.6:7891/join/wphf_...
Expires: 24h, one use
Waiting for co-founder to join...
```

Send the printed `/join` URL to your co-founder.

## Co-founder

Open the invite URL in a browser:

```text
http://100.82.14.6:7891/join/wphf_...
```

The invite is accepted once, the browser receives a WUPHF session cookie, and the page redirects to `#/channels/general`.

## Done

The host terminal prints:

```text
Co-founder joined. Open #general and work together.
```

Both founders should now see the same office. A system message appears in `#general` noting that the co-founder joined.

## Troubleshooting

If the host sees:

```text
error: no private network interface found
```

Start Tailscale or WireGuard and rerun:

```bash
wuphf share
```

If the invitee sees an expired-link message, create a new invite:

```bash
wuphf share
```

If you intentionally want to test on a normal LAN, use the explicit override:

```bash
wuphf share --unsafe-lan
```

Do not use public bind overrides for real workspace context.
