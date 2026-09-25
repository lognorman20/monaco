package app

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed agent_skill.md.tmpl
var agentSkillTemplate string

// AgentDocs is what an outside agent reads to trade for a cabal: skill.md and the connect
// text a member pastes into it. Both carry the API's public base URL, so they always point
// at the environment that served them.
type AgentDocs struct {
	baseURL string
	skill   string
}

// NewAgentDocs renders skill.md for baseURL, which has no trailing slash.
func NewAgentDocs(baseURL string) (*AgentDocs, error) {
	tmpl, err := template.New("agent_skill.md").Parse(agentSkillTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse agent skill template: %w", err)
	}
	var out bytes.Buffer
	err = tmpl.Execute(&out, struct {
		BaseURL           string
		IntentsPerHour    int
		ReasonMaxChars    int
		IdempotencyKeyMax int
	}{baseURL, AgentIntentsPerHour, MaxAgentIntentReasonRunes, MaxAgentIdempotencyKeyLength})
	if err != nil {
		return nil, fmt.Errorf("render agent skill template: %w", err)
	}
	return &AgentDocs{baseURL: baseURL, skill: out.String()}, nil
}

// AgentIntentsPerHour is the sustained per-agent intent limit the API enforces.
const AgentIntentsPerHour = 30

// SkillMarkdown is the rendered skill.md.
func (d *AgentDocs) SkillMarkdown() string { return d.skill }

// SkillURL is where skill.md is served.
func (d *AgentDocs) SkillURL() string { return d.baseURL + "/v1/agent/skill.md" }

// ConnectText is the block a member pastes into an agent to let it trade for the cabal.
func (d *AgentDocs) ConnectText(cabalName, agentName, apiKey string) string {
	lines := []string{
		fmt.Sprintf("You can trade stocks for the Monaco cabal %q as agent %q, within its budget. You never hold the money; Monaco executes your trades.", cabalName, agentName),
		"API base: " + d.baseURL,
		"Send this header on every request: X-Monaco-Agent-Key: " + apiKey,
		fmt.Sprintf("Before trading, read %s and follow it.", d.SkillURL()),
		fmt.Sprintf("Start with GET %s/v1/agent to see your budget, cash and holdings.", d.baseURL),
		"Always send an idempotencyKey with each intent. If a request times out, resend the same body with the same idempotencyKey.",
	}
	return strings.Join(lines, "\n")
}
