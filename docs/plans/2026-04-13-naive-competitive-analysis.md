# Naive (usenaive.ai) Competitive Analysis

Date: 2026-04-13
Source: Live product walkthrough of app.usenaive.ai + public research

---

## What Naive is

Naive (usenaive.ai) is a YC S25 company (Relixir Inc.) positioning as "The company
runtime. Build and run your entire business through chat." $49-149/mo + $0.50/credit
pay-as-you-go. Raised ~$2.5M from YC, 468 Capital, DG Daiwa Ventures, Rocket Internet.

Tagline: "Quit your job. Build your Dream Company now."

## Naive IS Paperclip

Naive is a hosted fork of Paperclip with a billing layer on top. This is not speculation.
71+ "Paperclip" references remain in their production JS bundle (index-DsiFu2Ny.js, 4MB).
Config paths point to `~/.paperclip/instances/default/db`. localStorage keys use
`paperclip:*` prefix. CSS classes named `paperclip-mermaid`, `paperclip-mdxeditor`.
Issue prefixes still `PAP-*` in places. 43 openclaw protocol references.

Timeline: Paperclip launched publicly March 4, 2026. Naive's SSL cert was issued
March 8 (4 days later). App launched March 10 on Fly.io. Zero attribution anywhere,
MIT license violation (copyright notices stripped). Documented at not-so-naive.vercel.app.

## What Naive adds on top of Paperclip

1. **Stripe billing + credits** ($49/mo for 50 credits, $149/mo for 200, $0.50/credit PAYG)
2. **Virtual cards** per agent (Stripe Issuing) for autonomous payments
3. **Email provisioning** per agent (agent-name@company.nai... addresses)
4. **Phone/SMS** provisioning per agent
5. **Domain purchase** and hosting (agents deploy to Vercel/Supabase)
6. **Browser automation** with 2FA support per agent
7. **Template store** (pre-built businesses + employees, marketplace model)
8. **Composio integration** (25+ services: Stripe, GitHub, Shopify, HubSpot, etc.)
9. **Getting Started checklist** (7-step onboarding wizard)
10. **Strategy page** (auto-generated positioning, target audience, active plans, goals)
11. **Polished web UI** (Next.js marketing site, clean dashboard)

## What Naive inherits from Paperclip (the bad parts)

All of Paperclip's architectural problems carry over:
- **Session resume accumulation** (--resume, ~70% of token waste, issue #544)
- **Global MCP inheritance** (all agents load all tool definitions, ~24k tokens/turn)
- **Heartbeat polling** (83 heartbeat references in bundle, burns tokens on empty inbox)
- **Workspace corruption** (222 workspaces, 1 cwd, issue #3335)

Naive has NOT fixed the core architecture. They added a billing layer and real-world
identity primitives (email, phone, bank) on top of the same wasteful engine.

## Naive's UI (what we observed)

### Sidebar
- Company (Chat, Overview, Strategy, Payments, Settings)
- Tasks (Kanban board: Needs Review, In Progress, Queued, Completed)
- Drive (shared file system, files organized by department)
- Apps (deployed websites/web apps with preview)
- Store (template marketplace: businesses + employees)
- TEAM section: CEO + department groups (Engineering, Marketing, Operations, Sales)
- Getting Started checklist (0/7 items)
- Credits balance + trial countdown

### Company Chat
- Slack-like message feed
- Agent status messages: "Got it - working on X now", "Finished X"
- Composer: "Ask a follow-up or start a new plan... Type @ to mention a teammate"
- Active agent bar at bottom showing current task + green dot
- Integration bar: 30+ service icons for quick connection

### Company Overview
- Company name + description + status (Active/Online)
- Stats cards: Team count, Tasks, Credits spent, Approvals
- Financials: Revenue (Stripe), AI Usage spending chart (per-minute graph)
- Cost breakdown: Chat Tokens vs Images
- Team roster by department
- Apps list with deploy status

### Strategy Page
- Auto-generated Positioning + Target Audience
- Strategy description
- Active Plans with progress (3/4, 1 active)
- Goals section ("Define goals through chat, CEO breaks them into milestones")

### Tasks (Kanban)
- Columns: Needs Review, In Progress, Queued, Completed
- Task IDs: GERMA-1, GERMA-5 etc.
- Each task shows assigned agent

### Drive
- Shared file system
- Tabs: All Files, Executive, Marketing, Sales, Engineering, Recruiting, Pending
- Files show: Name, Source (Agent), Folder (department), Modified date
- Search + Upload

### Agent Profile (per agent)
- Tabs: Profile, Chat, Browser, Phone, Workspace, Virtual Cards, Inbox, Compute, Settings
- Identity: own email, phone, legal entity (LLC), department
- FLEX badge: "Specialization set at runtime via templates"
- Chat: 1:1 DM with agent
- Browser: live view of agent's browser session during runs
- Workspace: Files + Skills (per-agent file system)
- Compute: per-agent credit spend, resource breakdown (AI inference tokens,
  image generation, Vercel, Supabase), monthly budget controls, Pause button

### Store (Template Marketplace)
- Business categories: Agency, E-Commerce, Media, SaaS, Professional Services, etc.
- Employee departments: Marketing, Sales, Engineering, Operations, Data
- Featured: Faceless YouTube Empire (6 employees, 8.4K downloads),
  SMMA (4 employees, 21.5K downloads), YouTube Long-Form Producer
- Each template has "Get" button, download count, star rating

### Apps
- Deployed websites/web apps
- Tabs: All, Web Apps, Websites
- Shows deploy status (active), preview thumbnail
- + New App button

## Pricing comparison

| | WUPHF | Naive | Paperclip |
|---|---|---|---|
| Price | Free (self-hosted) | $49-149/mo + credits | Free (self-hosted) |
| AI cost | Your own API keys/subs | $0.50/credit (markup) | Your own API keys |
| Hosting | Local / your server | Fly.io (hosted) | Local / your server |
| License | MIT | Proprietary | MIT |

## What Naive does well (learn from)

1. **Onboarding**: Getting Started checklist (7 steps) makes first-run guided
2. **Strategy page**: Auto-generating positioning + target audience from chat is clever
3. **Template store**: Pre-built businesses dramatically lower time-to-value
4. **Per-agent compute dashboard**: Token breakdown with budget controls per agent
5. **Drive**: Shared artifact storage by department, agents produce visible files
6. **Real-world identity**: Email, phone, bank for each agent (agents act autonomously)
7. **Active agent status bar**: Shows what's running without navigating away
8. **Department grouping**: Teams organized by function (Engineering, Marketing, etc.)

## What Naive does poorly

1. **Inherits all Paperclip token waste**: Same heartbeat, resume, global MCP
2. **Credit markup**: Users pay $0.50/credit on top of already-expensive AI inference
3. **No visibility into agent work**: Browser tab says "inactive" during runs,
   no terminal/stdout streaming visible
4. **Chat is shallow**: Status messages only ("Got it, working on X"), not real
   conversation between agents
5. **Closed source**: Can't inspect, modify, or self-host
6. **Attribution scandal**: MIT license violation, controversy is public
7. **No live streaming of agent tool calls**: You can't watch the agent think/work
8. **No DM mid-task steering**: No way to redirect an agent while it's running

## Implications for WUPHF

### Our real differentiators vs Naive
1. **Architecture**: Fresh sessions, push wakes, per-agent MCP (Naive has none of this)
2. **Visibility**: Live agent stdout streaming (Naive shows "Browser inactive")
3. **Steering**: DM agents mid-task without restart (Naive can't)
4. **Self-hosted + free**: No credit markup, use your own subscriptions
5. **Open source**: MIT, inspect and modify anything
6. **Slack-native UX**: Agents in shared channels, real conversation (not just status)

### What we should consider adding (post-launch)
1. Pack templates (like Naive's store, but as CLI: `wuphf init --pack youtube-empire`)
2. Per-agent compute dashboard with budget controls
3. Getting Started checklist for first-run
4. Strategy/goals page auto-generated from initial chat
5. Drive/artifacts view for agent-produced files

### What we should NOT copy
1. Credit system / billing markup (our ICP uses their own subscriptions)
2. Real-world identity (email, phone, bank) - too much infrastructure for v1
3. Closed-source model (our open-source positioning is the pitch)
4. Hosted-only model (self-hosted is the differentiator)

## Updated competitive landscape

```
                    Open Source    Self-Hosted    Efficient Architecture    Live Visibility
Paperclip           Yes           Yes            No (3 root causes)        No
Naive               No            No             No (inherits Paperclip)   No
WUPHF               Yes           Yes            Yes                       Yes
```

## Content implications

The Reddit post should acknowledge Naive exists. Two competitors, not one.
But the positioning is even stronger now:

- Paperclip: open source, self-hosted, but wastes tokens
- Naive: hosted Paperclip fork with billing markup, same waste, $49-149/mo
- WUPHF: open source, self-hosted, architecturally efficient, live visibility

The pitch becomes: "Why pay $149/mo for a Paperclip fork that inherits all the
token waste, when you can self-host something architecturally better for free?"
