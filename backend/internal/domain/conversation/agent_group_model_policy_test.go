package conversation

import "testing"

func TestAgentGroupRequestModelPolicy(t *testing.T) {
	tests := []struct {
		name                     string
		isAgentGroupConversation bool
		requestModel             string
		allowed                  bool
	}{
		{
			name:         "ordinary conversation accepts selected model",
			requestModel: "openai/gpt-5",
			allowed:      true,
		},
		{
			name:                     "agent group accepts empty model",
			isAgentGroupConversation: true,
			allowed:                  true,
		},
		{
			name:                     "agent group accepts whitespace model",
			isAgentGroupConversation: true,
			requestModel:             "   ",
			allowed:                  true,
		},
		{
			name:                     "agent group rejects selected model",
			isAgentGroupConversation: true,
			requestModel:             "openai/gpt-5",
			allowed:                  false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			allowed := AgentGroupRequestModelAllowed(
				testCase.isAgentGroupConversation,
				testCase.requestModel,
			)
			if allowed != testCase.allowed {
				t.Fatalf("AgentGroupRequestModelAllowed() = %v, want %v", allowed, testCase.allowed)
			}
		})
	}
}
