package conversation

import "strings"

// AgentGroupRequestModelAllowed reports whether a request-level model is valid
// for the target conversation. Agent Group members resolve their own models.
func AgentGroupRequestModelAllowed(isAgentGroupConversation bool, requestModel string) bool {
	return !isAgentGroupConversation || strings.TrimSpace(requestModel) == ""
}
