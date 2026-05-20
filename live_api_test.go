package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLiveAPIModelRun(t *testing.T) {
	config := requireLiveModelConfigForTest(t)
	t.Logf("live api type=%s model=%s base_url=%s", config.APIType, config.Model, safeBaseURLForLiveTest(config.BaseURL))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	model, err := NewModel(config)
	if err != nil {
		logLiveAPIDebugForTest(t, err, config)
		t.Fatal(formatLiveAPIModelConstructionErrorForTest(err))
	}

	var observations []Observation
	bot, err := New(
		Config{SystemPrompt: "You are verifying an SDK integration. Answer in one short sentence."},
		model,
		WithObserver(ObserverFunc(func(ctx context.Context, observation Observation) {
			observations = append(observations, observation)
		})),
	)
	if err != nil {
		t.Fatal(err)
	}

	reply, err := bot.Run(ctx, "Reply with the exact phrase: live api check ok")
	if err != nil {
		logLiveAPIDebugForTest(t, err, config)
		t.Fatal(formatLiveAPIErrorForTest(err))
	}
	if strings.TrimSpace(reply.Content) == "" {
		t.Fatal("reply content is empty")
	}

	t.Logf("assistant: %s", strings.TrimSpace(reply.Content))
	logLiveAPIObservationsForTest(t, observations)
}
