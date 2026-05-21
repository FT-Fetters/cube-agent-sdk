package agent

import (
	"context"
	"reflect"
	"testing"
)

func TestSamplingObserverFiltersByEventType(t *testing.T) {
	ctx := context.Background()
	recorder := &recordingObserver{}
	observer := NewSamplingObserver(SamplingObserverOptions{
		Child:      recorder,
		EventTypes: []EventType{EventAfterModel, EventAfterTool},
		Ratio:      1,
	})
	beforeModel := Observation{Type: EventBeforeModel, RequestID: "before-model"}
	afterModel := Observation{Type: EventAfterModel, RequestID: "after-model"}
	afterTool := Observation{Type: EventAfterTool, RequestID: "after-tool"}

	observer.Observe(ctx, beforeModel)
	observer.Observe(ctx, afterModel)
	observer.Observe(ctx, afterTool)

	want := []Observation{afterModel, afterTool}
	if !reflect.DeepEqual(recorder.Observations(), want) {
		t.Fatalf("observations = %#v, want %#v", recorder.Observations(), want)
	}
}

func TestSamplingObserverFiltersByFailureStatus(t *testing.T) {
	ctx := context.Background()
	successRecorder := &recordingObserver{}
	failureRecorder := &recordingObserver{}
	success := Observation{Type: EventAfterModel, RequestID: "success"}
	failure := Observation{Type: EventAfterModel, RequestID: "failure", Failed: true, ErrorCategory: ErrorCategoryModel}

	NewSamplingObserver(SamplingObserverOptions{
		Child:         successRecorder,
		FailureStatus: SampleSuccessfulObservations,
		Ratio:         1,
	}).Observe(ctx, success)
	NewSamplingObserver(SamplingObserverOptions{
		Child:         successRecorder,
		FailureStatus: SampleSuccessfulObservations,
		Ratio:         1,
	}).Observe(ctx, failure)

	NewSamplingObserver(SamplingObserverOptions{
		Child:         failureRecorder,
		FailureStatus: SampleFailedObservations,
		Ratio:         1,
	}).Observe(ctx, success)
	NewSamplingObserver(SamplingObserverOptions{
		Child:         failureRecorder,
		FailureStatus: SampleFailedObservations,
		Ratio:         1,
	}).Observe(ctx, failure)

	if !reflect.DeepEqual(successRecorder.Observations(), []Observation{success}) {
		t.Fatalf("success observations = %#v, want success only", successRecorder.Observations())
	}
	if !reflect.DeepEqual(failureRecorder.Observations(), []Observation{failure}) {
		t.Fatalf("failure observations = %#v, want failure only", failureRecorder.Observations())
	}
}

func TestSamplingObserverAlwaysSamplesFailuresBeforeRatio(t *testing.T) {
	ctx := context.Background()
	recorder := &recordingObserver{}
	observer := NewSamplingObserver(SamplingObserverOptions{
		Child:                recorder,
		Ratio:                0,
		AlwaysSampleFailures: true,
	})
	success := Observation{Type: EventAfterModel, RequestID: "success"}
	failure := Observation{Type: EventAfterModel, RequestID: "failure", Failed: true, ErrorCategory: ErrorCategoryModel}

	observer.Observe(ctx, success)
	observer.Observe(ctx, failure)

	if !reflect.DeepEqual(recorder.Observations(), []Observation{failure}) {
		t.Fatalf("observations = %#v, want failed observation", recorder.Observations())
	}
}

func TestSamplingObserverAppliesRatioToEligibleObservations(t *testing.T) {
	ctx := context.Background()
	recorder := &recordingObserver{}
	var sampled []string
	observer := NewSamplingObserver(SamplingObserverOptions{
		Child: recorder,
		Ratio: 0.25,
		Sampler: ObservationSamplerFunc(func(observation Observation, ratio float64) bool {
			if ratio != 0.25 {
				t.Fatalf("ratio = %v, want 0.25", ratio)
			}
			sampled = append(sampled, observation.RequestID)
			return observation.RequestID == "keep"
		}),
	})
	drop := Observation{Type: EventAfterModel, RequestID: "drop"}
	keep := Observation{Type: EventAfterModel, RequestID: "keep"}

	observer.Observe(ctx, drop)
	observer.Observe(ctx, keep)

	if !reflect.DeepEqual(sampled, []string{"drop", "keep"}) {
		t.Fatalf("sampled request IDs = %#v, want both eligible observations", sampled)
	}
	if !reflect.DeepEqual(recorder.Observations(), []Observation{keep}) {
		t.Fatalf("observations = %#v, want sampled observation", recorder.Observations())
	}
}

func TestSamplingObserverNilChildIsNoop(t *testing.T) {
	ctx := context.Background()
	var nilObserver Observer

	NewSamplingObserver(SamplingObserverOptions{
		Child: nil,
		Ratio: 1,
	}).Observe(ctx, Observation{Type: EventAfterModel, RequestID: "nil-child"})
	NewSamplingObserver(SamplingObserverOptions{
		Child: nilObserver,
		Ratio: 1,
	}).Observe(ctx, Observation{Type: EventAfterModel, RequestID: "typed-nil-child"})
	SamplingObserver{}.Observe(ctx, Observation{Type: EventAfterModel, RequestID: "zero-value"})
}

func TestSamplingObserverDeterministicDefaultDecisions(t *testing.T) {
	ctx := context.Background()
	observations := []Observation{
		{Type: EventAfterModel, AgentID: "agent", RequestID: "request-a", Round: 1},
		{Type: EventAfterModel, AgentID: "agent", RequestID: "request-b", Round: 1},
		{Type: EventAfterTool, AgentID: "agent", RequestID: "request-c", ToolName: "echo"},
		{Type: EventAfterApproval, AgentID: "agent", RequestID: "request-d", Approved: true},
	}
	first := sampleObservationsForTest(ctx, observations)
	second := sampleObservationsForTest(ctx, observations)

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("default sampled observations = %#v, want deterministic repeat %#v", second, first)
	}
}

func sampleObservationsForTest(ctx context.Context, observations []Observation) []Observation {
	recorder := &recordingObserver{}
	observer := NewSamplingObserver(SamplingObserverOptions{
		Child: recorder,
		Ratio: 0.5,
	})
	for _, observation := range observations {
		observer.Observe(ctx, observation)
	}
	return recorder.Observations()
}
