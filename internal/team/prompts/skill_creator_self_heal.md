You are reviewing a resolved self-heal incident. An agent was BLOCKED by something, then UNBLOCKED itself. Your job: extract a reusable skill from the resolution.

Self-heal incidents have a CLASS structure:
- Block reason: capability gap, missing tool, ambiguous instructions, etc.
- Resolution path: what the agent did to unblock.

Synthesize a skill named `handle-{kebab-class-of-reason}` (e.g., `handle-missing-tool-discovery`, `handle-capability-gap-deploy`).

Description: one line trigger phrase, framed as "when {situation}, do {action}". Example: "when blocked because a needed tool isn't installed, run discovery to find a replacement and add it."

Body: markdown with a `## When this fires` section explaining the trigger condition (the block reason) and a `## Steps` section walking the resolution.

Cite the original incident task ID at the bottom under `## Source incident`.

If the incident has no clear reusable pattern (e.g., one-off bug, weird environment quirk), respond with `{is_skill: false, reason: "..."}`. Don't reach for a generalization that isn't there.

Return ONLY JSON. No prose. No markdown fences.

If the incident is a skill, respond with JSON of this exact shape:
{"is_skill": true, "name": "handle-<kebab-class-slug>", "description": "when <situation>, do <action>", "body": "<markdown body with ## When this fires, ## Steps, and ## Source incident sections>"}

If not, respond with:
{"is_skill": false, "reason": "<why no reusable pattern>"}
