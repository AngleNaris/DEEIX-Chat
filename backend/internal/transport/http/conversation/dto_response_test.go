package conversation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	appconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/conversation"
)

func TestPublicGroupRunResponseMapsThinkAndToolsWithoutInternalSnapshots(t *testing.T) {
	timeline := appconversation.PublicGroupRunTimeline{
		GroupRunID: "run_public", Status: "completed", StartedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0),
		Steps: []appconversation.PublicGroupRunStep{{
			StepID: "step_public", StepType: "member_execute", Actor: appconversation.PublicGroupRunActor{MemberID: "member", Name: "Worker", Type: "worker"},
			Attempts: []appconversation.PublicGroupRunAttempt{{
				AttemptID: "attempt_public", AttemptNumber: 1, Status: "completed", Output: "public output",
				ThinkMarkdown: "public thought", ToolCallsJSON: `[{"name":"public_tool"}]`,
			}},
		}},
	}
	response := toPublicGroupRunTimelineResponse(timeline)
	if got := response.Steps[0].Attempts[0]; got.ThinkMarkdown != "public thought" || got.ToolCallsJSON != `[{"name":"public_tool"}]` {
		t.Fatalf("public fields did not pass through: %#v", got)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"input_snapshot_json", "InputSnapshotJSON", "internal instruction", "secret instruction"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public response leaked %q: %s", forbidden, text)
		}
	}
}
