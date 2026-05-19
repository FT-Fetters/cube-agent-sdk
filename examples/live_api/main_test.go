package main

import (
	"testing"
	"time"

	agent "github.com/cubence/cube-agent-sdk"
)

func TestFormatObservationLineIncludesSafeMetadata(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 34, 56, 789000000, time.UTC)
	observation := agent.Observation{
		Type:            agent.EventAfterTool,
		AgentID:         "agent-1",
		SubagentID:      "worker-1",
		ToolName:        "echo",
		ToolRisk:        agent.ToolRiskRead,
		SkillName:       "live-test",
		RequestID:       "req-1",
		Round:           3,
		Duration:        1250 * time.Millisecond,
		EstimatedTokens: 321,
		Approved:        true,
		ApprovalReason:  "allowed for live test",
		ErrorCategory:   agent.ErrorCategoryTool,
		Failed:          true,
	}

	line := formatObservationLine(7, now, observation)
	want := "observation=7 time=2026-05-18T12:34:56.789Z event=after_tool status=failed agent=agent-1 subagent=worker-1 request=req-1 round=3 duration=1.25s estimated_tokens=321 tool=echo risk=read skill=live-test approved=true approval_reason=\"allowed for live test\" error_category=tool"

	if line != want {
		t.Fatalf("formatObservationLine() = %q, want %q", line, want)
	}
}
