package core

import "os"

var agentContextEnvVars = []string{
	"PM_SESSION_ID",
	"COPILOT_SESSION_ID",
	"COPILOT_CLI_SESSION_ID",
	"CLAUDE_SESSION_ID",
	"AIDER_SESSION_ID",
}

// InAgentContext reports whether pm appears to be running under an AI agent
// session, returning the first env var that indicated the context.
func InAgentContext() (bool, string) {
	for _, v := range agentContextEnvVars {
		if os.Getenv(v) != "" {
			return true, v
		}
	}
	return false, ""
}
