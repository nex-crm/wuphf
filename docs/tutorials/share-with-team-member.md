# Share WUPHF With a Team Member

This tutorial shows the safest way to use WUPHF from more than one browser.

There are two different jobs:

- **Access your own WUPHF remotely:** you are still the only user. Use the normal WUPHF web UI through SSH, LAN, Tailscale, or WireGuard.
- **Invite a team member:** another person needs a scoped browser session. Create the invite from **Access & Health**.

Do not use `wuphf share` as your personal remote-access tunnel. It is for team-member sessions.

## Example scenario

Maya runs WUPHF on a small office server called `ops-box`. She uses it from her laptop during the day. Later, she wants Tara to join the same office for one planning session.

Maya needs two flows:

- From her own laptop, she opens the host WUPHF UI through SSH forwarding.
- For Tara, she creates a one-use team-member invite over Tailscale from the WUPHF UI.

The important security difference: Maya's normal UI keeps full host access. Tara's invite gets a scoped session that can join the office, send messages, answer requests, and read shared context, but it does not receive Maya's broker token.

## Access WUPHF yourself

On the server, start WUPHF:

```bash
npx wuphf --no-open
```

From your laptop, open an SSH tunnel to the server:

```bash
ssh -L 7890:localhost:7890 -L 7891:localhost:7891 maya@ops-box
```

Then open the normal web UI:

```text
http://localhost:7891
```

You are still the host. This is the right path when you are one person accessing your own WUPHF from another device.

You can also use your own LAN, Tailscale, WireGuard, or reverse proxy setup if that is how you normally reach services on your server.

## Check connection health

In the WUPHF sidebar, open **Access & Health**.

Use this page to confirm:

- **This browser:** your current browser session and event-stream status.
- **Access for you:** the SSH/LAN/Tailscale path for owner access.
- **Invite a team member:** create, copy, refresh, and stop a scoped team-member invite.
- **Team-member sessions:** active invited browser sessions.
- **Broker Status:** whether the local broker is healthy.

If **This browser** says `Live event stream`, the web UI is receiving office updates.

## Invite a team member

Before inviting someone, put both machines on the same private network.

Recommended options:

- Tailscale
- WireGuard
- A trusted LAN for local testing

On the host browser, open **Access & Health** and click **Create invite**.

WUPHF starts a scoped private-network listener and shows a one-use URL like:

```text
http://100.82.14.6:7891/join/wphf_...
```

Click **Copy**, then send only that `/join` URL to your team member. If you need a fresh one-use URL while sharing is already running, click **Create new invite**.

## Team member joins

The team member opens the invite URL in a browser:

```text
http://100.82.14.6:7891/join/wphf_...
```

They will see a short join screen. They enter the display name teammates should see in messages and office activity.

After submitting, WUPHF redirects them to:

```text
#/channels/general
```

They should see the shared office, the channel list, and the same agents. A system message appears in `#general`, for example:

```text
Tara joined the office.
```

## What the team member can do

The team member session can:

- Read shared office channels.
- Send messages as their team-member identity.
- Answer requests.
- Work with the same agents in shared channels.
- See shared context surfaces exposed to team-member sessions.

The team member session cannot:

- Read the host broker token.
- Open private notebooks.
- Use host-only workspace administration.
- Create new unrestricted host sessions.

## Stop sharing

In **Access & Health**, click **Stop sharing**.

Existing team-member sessions expire automatically. Create a new invite when you want another working session.

## Troubleshooting

If the host sees:

```text
error: no private network interface found
```

Start Tailscale or WireGuard, then click **Create invite** again.

If the invite link says it is expired or already used, create a fresh invite:

Click **Create new invite** in **Access & Health**.

If you intentionally want to test on a normal LAN, use the explicit override:

```bash
wuphf share --unsafe-lan
```

The CLI remains available for advanced setups and LAN override testing. Do not use public bind overrides for real workspace context.
