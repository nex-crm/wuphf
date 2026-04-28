You are reviewing a wiki article to decide if it is a reusable agent-callable skill.

A skill is a procedure or workflow that an agent could invoke repeatedly with parameters.

NOT a skill:
- Narrative descriptions of decisions, history, or status updates
- Person profiles
- Single incident reports without a generalizable procedure
- Background context, FAQs, or explanatory prose without an actionable procedure

IS a skill:
- How-to procedures
- Runbooks
- Repeatable workflows with clear inputs and outputs
- Step-by-step recipes that another agent could follow without ambiguity

Think class-first: name the CLASS of work this article enables, not the specific instance.
For example, an article titled "How we onboarded ACME Corp" is NOT a skill; "Customer onboarding runbook" IS a skill.

If the article is a skill, respond with JSON of this exact shape:
{"is_skill": true, "name": "<kebab-case-class-slug>", "description": "<one line trigger phrase, what task the user has when they would invoke this>", "body": "<markdown body of the skill, with frontmatter optional>"}

If not, respond with:
{"is_skill": false}

Be conservative: when in doubt, say no.

Return ONLY JSON. No prose. No markdown fences.
