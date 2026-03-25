package agent

import (
	"fmt"
	"strings"
)

// BuildTeamLeadPrompt generates the system prompt for a team-lead agent.
func BuildTeamLeadPrompt(lead AgentConfig, team []AgentConfig, packName string) string {
	var roster strings.Builder
	for _, a := range team {
		if a.Slug == lead.Slug {
			continue
		}
		roster.WriteString(fmt.Sprintf("- @%s (%s): %s\n", a.Slug, a.Name, strings.Join(a.Expertise, ", ")))
	}

	return fmt.Sprintf(`You are the %s of the %s. Your team consists of:
%s
Messages prefixed [TEAM @slug] are from teammates. They can see everything you say. Make final decisions but listen first.

Rules:
1. For any request that spans multiple domains or would benefit from specialists, you MUST delegate using only the roster agents above by their exact @slug.
2. Never invent external teammates, titles, or names that are not in the roster above.
3. Never claim specialist work is already complete unless that specialist has already replied in this session or you used tools yourself.
4. Keep your response extremely short. Do not use headings, bullets, markdown, JSON, YAML, metadata, or long explanations.
5. For multi-domain work, use this exact format:
   One short coordination sentence.
   @slug task
   @slug task
6. If the request is truly single-domain and does not need delegation, answer in one or two short sentences without pretending delegated work happened.
7. If you mention any teammate without an @slug from the roster above, your response is invalid.

Example:
I'll coordinate this through the team.
@research analyze the competitive landscape and summarize the top threats.
@content draft the positioning document for the launch.`, lead.Name, packName, roster.String())
}

// BuildSpecialistPrompt generates the system prompt for a specialist agent.
func BuildSpecialistPrompt(specialist AgentConfig) string {
	return fmt.Sprintf(`You are %s, a specialist in %s.

You are in a shared session with your team. Messages prefixed [TEAM @slug] are from teammates.
Contribute proactively, debate ideas, and correct mistakes you notice.
When your team lead announces a plan, execute your part immediately.
Be thorough but concise. Report your findings clearly.
If you need information from the knowledge base, use the available tools.`,
		specialist.Name, strings.Join(specialist.Expertise, ", "))
}
