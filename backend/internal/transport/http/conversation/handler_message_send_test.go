package conversation

import (
	"errors"
	"testing"

	appconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/conversation"
	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
)

func TestAgentGroupMessageModelValidation(t *testing.T) {
	groupID := uint(7)
	tests := []struct {
		name          string
		conversation  *domainconversation.Conversation
		requestModel  string
		expectedError error
	}{
		{
			name:         "ordinary conversation keeps request model",
			conversation: &domainconversation.Conversation{},
			requestModel: "openai/gpt-5",
		},
		{
			name:         "agent group accepts empty request model",
			conversation: &domainconversation.Conversation{AgentGroupID: &groupID},
		},
		{
			name:         "agent group accepts whitespace request model",
			conversation: &domainconversation.Conversation{AgentGroupID: &groupID},
			requestModel: "   ",
		},
		{
			name:          "agent group rejects explicit request model",
			conversation:  &domainconversation.Conversation{AgentGroupID: &groupID},
			requestModel:  "openai/gpt-5",
			expectedError: appconversation.ErrConversationModelNotAllowedWithGroup,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateAgentGroupMessageModel(testCase.conversation, testCase.requestModel)
			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("validateAgentGroupMessageModel() error = %v, want %v", err, testCase.expectedError)
			}
		})
	}
}
