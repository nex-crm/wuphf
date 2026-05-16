# 04b — Morgan ships a custom agency pack (Scenario 4: configure)

## Who and why

**Persona:** Morgan Lee, agency founder, six people. They want one
office their whole team can spin up with the same roster. The pack
must be opinionated — Producer, Account, Strategy, Creative, Dev — and
must work for every project, not need a tweak per client.

**Outcome they came for:** define a pack directory once, then have any
teammate run `npx wuphf --pack agency-six` and land in the exact same
office.

## Steps

### 1. Create a pack directory

```bash
mkdir -p ~/.wuphf/packs/agency-six/agents
mkdir -p ~/.wuphf/packs/agency-six/channels
```

### 2. Drop agent JSONs into the pack

Create `~/.wuphf/packs/agency-six/agents/producer.json`,
`account.json`, `strategy.json`, `creative.json`, `dev.json`. Each
file mirrors the default agent JSON shape with a custom
`system_prompt` per role.

### 3. Define the channel list

Create `~/.wuphf/packs/agency-six/channels/general.json` with
appropriate channel metadata for each desired channel
(`#general`, `#clients`, `#delivery`).

### 4. Launch with the pack

```bash
npx wuphf --pack agency-six
```

#### Verify

- The CLI prints the pack name on startup.
- The office name reads "agency-six" in the sidebar.
- All five named agents are in the participants column.
- Three channels appear in the sidebar.

### 5. Run a real agency workflow

In `#general`:

```
Client kickoff for ACME on Thursday. Producer owns the agenda;
Strategy briefs creative; Dev confirms tech stack.
```

#### Verify

- All three named agents reply in-thread within 90s.
- Producer's reply contains a numbered agenda.
- The Inbox sidebar entry gets a non-zero badge.

### 6. Confirm a teammate can reproduce

On a second machine (or the share/team-member flow):

```bash
npx wuphf --pack agency-six
```

#### Verify

- The teammate's office has the same channels and the same agent
  roster.
- The agent JSONs match (compare `~/.wuphf/agents/*.json`).

## What success looks like

The agency-six pack works on any machine after one `npx wuphf --pack`
invocation. No code change. No private dependency. The pack is fully
described by JSON files in `~/.wuphf/packs/agency-six/`. Morgan can
hand the directory to a new hire on day one.
