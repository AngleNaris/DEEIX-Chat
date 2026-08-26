package conversation

import (
	"errors"
	"strings"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
)

const maxCredentialStreamBufferBytes = 8 * 1024 * 1024

var errCredentialStreamBufferLimitExceeded = errors.New("credential-safe stream buffer limit exceeded")

// Credential writes are only observable after the model returns a tool call.
// Buffer follow-up generations after an observed attempt, but do not delay every
// ordinary conversation merely because credential tools are available.
func shouldBufferCredentialStream(credentialAttempted bool) bool {
	return credentialAttempted
}

type credentialStreamBuffer struct {
	events []llm.GenerateStreamEvent
	bytes  int
}

func (b *credentialStreamBuffer) add(event llm.GenerateStreamEvent) (llm.GenerateStreamEvent, bool, error) {
	immediate := llm.GenerateStreamEvent{
		Usage:      event.Usage,
		ResponseID: event.ResponseID,
	}
	hasImmediate := immediate.Usage != (llm.Usage{}) || strings.TrimSpace(immediate.ResponseID) != ""

	buffered := cloneGenerateStreamEvent(event)
	buffered.Usage = llm.Usage{}
	buffered.ResponseID = ""
	buffered.GeneratedImage = nil
	buffered.GeneratedImageIndex = 0
	buffered.GeneratedImagePartial = false
	hasBuffered := credentialBufferedStreamEventHasContent(buffered)
	hadSideEffect := hasImmediate || hasBuffered || event.GeneratedImage != nil
	if !hasBuffered {
		return immediate, hadSideEffect, nil
	}

	eventBytes := credentialBufferedStreamEventBytes(buffered)
	if eventBytes > maxCredentialStreamBufferBytes-b.bytes {
		return immediate, true, errCredentialStreamBufferLimitExceeded
	}
	b.events = append(b.events, buffered)
	b.bytes += eventBytes
	return immediate, true, nil
}

func credentialBufferedStreamEventHasContent(event llm.GenerateStreamEvent) bool {
	return event.Delta != "" || event.Reasoning != nil || event.ServerToolCall != nil
}

func credentialBufferedStreamEventBytes(event llm.GenerateStreamEvent) int {
	size := len(event.Delta)
	if event.Reasoning != nil {
		size += len(event.Reasoning.EventType) + len(event.Reasoning.ItemID) + len(event.Reasoning.Status) +
			len(event.Reasoning.Kind) + len(event.Reasoning.Text) + len(event.Reasoning.Signature) +
			len(event.Reasoning.EncryptedContent)
	}
	if event.ServerToolCall != nil {
		size += len(event.ServerToolCall.ToolCallID) + len(event.ServerToolCall.ToolType) +
			len(event.ServerToolCall.ToolName) + len(event.ServerToolCall.ArgumentsJSON) +
			len(event.ServerToolCall.ThoughtSignature) + len(event.ServerToolCall.Status) +
			len(event.ServerToolCall.OutputJSON) + len(event.ServerToolCall.ErrorJSON)
	}
	return size
}

func cloneGenerateStreamEvent(event llm.GenerateStreamEvent) llm.GenerateStreamEvent {
	cloned := event
	if event.Reasoning != nil {
		reasoning := *event.Reasoning
		cloned.Reasoning = &reasoning
	}
	if event.ServerToolCall != nil {
		toolCall := *event.ServerToolCall
		cloned.ServerToolCall = &toolCall
	}
	if event.GeneratedImage != nil {
		image := *event.GeneratedImage
		cloned.GeneratedImage = &image
	}
	return cloned
}

func flushCredentialBufferedStreamEvents(
	events []llm.GenerateStreamEvent,
	attempts []credentialWrite,
	handle func(llm.GenerateStreamEvent) error,
) error {
	if len(events) == 0 || handle == nil {
		return nil
	}

	var deltaText strings.Builder
	var reasoningText strings.Builder
	firstDelta := -1
	firstReasoning := -1
	for index, event := range events {
		if event.Delta != "" {
			if firstDelta < 0 {
				firstDelta = index
			}
			deltaText.WriteString(event.Delta)
		}
		if event.Reasoning != nil && event.Reasoning.Text != "" {
			if firstReasoning < 0 {
				firstReasoning = index
			}
			reasoningText.WriteString(event.Reasoning.Text)
		}
	}
	cleanDelta, _ := applyCredentialReplacements(deltaText.String(), attempts, nil)
	cleanReasoning, _ := applyCredentialReplacements(reasoningText.String(), attempts, nil)

	for index, event := range events {
		event = sanitizeGenerateStreamEventCredentialAttempts(event, attempts)
		if event.Delta != "" {
			if index == firstDelta {
				event.Delta = cleanDelta
			} else {
				event.Delta = ""
			}
		}
		if event.Reasoning != nil && event.Reasoning.Text != "" {
			if index == firstReasoning {
				event.Reasoning.Text = cleanReasoning
			} else {
				event.Reasoning.Text = ""
			}
		}
		if event.Delta == "" && event.Reasoning != nil && event.Reasoning.Text == "" &&
			event.Usage == (llm.Usage{}) && event.ServerToolCall == nil &&
			event.GeneratedImage == nil && strings.TrimSpace(event.ResponseID) == "" {
			continue
		}
		if err := handle(event); err != nil {
			return err
		}
	}
	return nil
}
