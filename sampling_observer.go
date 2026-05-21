package agent

import (
	"context"
	"encoding/binary"
	"hash"
	"hash/fnv"
	"math"
)

// SamplingFailureStatus limits sampling to observations with a specific failure
// state. The zero value samples both successful and failed observations.
type SamplingFailureStatus int

const (
	// SampleAllObservations allows both successful and failed observations.
	SampleAllObservations SamplingFailureStatus = iota
	// SampleFailedObservations allows only failed observations.
	SampleFailedObservations
	// SampleSuccessfulObservations allows only successful observations.
	SampleSuccessfulObservations
)

// ObservationSampler decides whether an eligible observation passes ratio
// sampling. Implement this interface to make tests or deployments use a
// caller-controlled deterministic source.
type ObservationSampler interface {
	SampleObservation(observation Observation, ratio float64) bool
}

// ObservationSamplerFunc adapts a function into an ObservationSampler.
type ObservationSamplerFunc func(Observation, float64) bool

func (f ObservationSamplerFunc) SampleObservation(observation Observation, ratio float64) bool {
	if f == nil {
		return false
	}
	return f(observation, ratio)
}

// SamplingObserverOptions configures a sampling observer wrapper.
type SamplingObserverOptions struct {
	// Child receives observations selected by the sampler. A nil child makes the
	// sampling observer a no-op.
	Child Observer
	// EventTypes limits sampling to these event types. Empty means all event types.
	EventTypes []EventType
	// FailureStatus limits sampling by Observation.Failed. The zero value allows
	// both successful and failed observations.
	FailureStatus SamplingFailureStatus
	// Ratio is the sample ratio for eligible observations. Values at or below 0
	// drop eligible observations; values at or above 1 keep them.
	Ratio float64
	// AlwaysSampleFailures keeps eligible failed observations regardless of Ratio.
	AlwaysSampleFailures bool
	// Sampler overrides the default deterministic hash sampler.
	Sampler ObservationSampler
}

// SamplingObserver forwards selected sanitized observations to a child observer.
// It only inspects Observation fields and does not add telemetry attributes.
type SamplingObserver struct {
	child                Observer
	eventTypes           map[EventType]struct{}
	failureStatus        SamplingFailureStatus
	ratio                float64
	alwaysSampleFailures bool
	sampler              ObservationSampler
}

// NewSamplingObserver returns an Observer wrapper that forwards selected
// sanitized observations to Child.
func NewSamplingObserver(options SamplingObserverOptions) SamplingObserver {
	eventTypes := make(map[EventType]struct{}, len(options.EventTypes))
	for _, eventType := range options.EventTypes {
		eventTypes[eventType] = struct{}{}
	}
	if len(eventTypes) == 0 {
		eventTypes = nil
	}
	sampler := options.Sampler
	if sampler == nil {
		sampler = deterministicObservationSampler{}
	}
	return SamplingObserver{
		child:                options.Child,
		eventTypes:           eventTypes,
		failureStatus:        options.FailureStatus,
		ratio:                normalizeObservationSampleRatio(options.Ratio),
		alwaysSampleFailures: options.AlwaysSampleFailures,
		sampler:              sampler,
	}
}

func (o SamplingObserver) Observe(ctx context.Context, observation Observation) {
	if o.child == nil || !o.observationEligible(observation) {
		return
	}
	if o.alwaysSampleFailures && observation.Failed {
		observeChild(ctx, o.child, observation)
		return
	}
	ratio := normalizeObservationSampleRatio(o.ratio)
	if ratio <= 0 {
		return
	}
	if ratio >= 1 {
		observeChild(ctx, o.child, observation)
		return
	}
	sampler := o.sampler
	if sampler == nil {
		sampler = deterministicObservationSampler{}
	}
	if sampler.SampleObservation(observation, ratio) {
		observeChild(ctx, o.child, observation)
	}
}

func (o SamplingObserver) observationEligible(observation Observation) bool {
	if len(o.eventTypes) > 0 {
		if _, ok := o.eventTypes[observation.Type]; !ok {
			return false
		}
	}
	switch o.failureStatus {
	case SampleFailedObservations:
		return observation.Failed
	case SampleSuccessfulObservations:
		return !observation.Failed
	default:
		return true
	}
}

type deterministicObservationSampler struct{}

func (deterministicObservationSampler) SampleObservation(observation Observation, ratio float64) bool {
	ratio = normalizeObservationSampleRatio(ratio)
	if ratio <= 0 {
		return false
	}
	if ratio >= 1 {
		return true
	}
	hasher := fnv.New64a()
	writeObservationSampleHash(hasher, observation)
	threshold := uint64(ratio * float64(^uint64(0)))
	return hasher.Sum64() <= threshold
}

func normalizeObservationSampleRatio(ratio float64) float64 {
	if math.IsNaN(ratio) || ratio <= 0 {
		return 0
	}
	if ratio >= 1 {
		return 1
	}
	return ratio
}

func writeObservationSampleHash(hasher hash.Hash64, observation Observation) {
	writeObservationSampleString(hasher, string(observation.Type))
	writeObservationSampleString(hasher, observation.AgentID)
	writeObservationSampleString(hasher, observation.RunID)
	writeObservationSampleString(hasher, observation.SubagentID)
	writeObservationSampleString(hasher, observation.ToolName)
	writeObservationSampleString(hasher, string(observation.ToolRisk))
	writeObservationSampleString(hasher, observation.SkillName)
	writeObservationSampleString(hasher, observation.TraceID)
	writeObservationSampleString(hasher, observation.SpanID)
	writeObservationSampleString(hasher, observation.TraceState)
	writeObservationSampleString(hasher, observation.RequestID)
	writeObservationSampleString(hasher, observation.ParentRequestID)
	writeObservationSampleInt(hasher, observation.Round)
	writeObservationSampleInt64(hasher, observation.Duration.Nanoseconds())
	writeObservationSampleInt(hasher, observation.EstimatedTokens)
	writeObservationSampleInt(hasher, observation.TokenUsage.InputTokens)
	writeObservationSampleInt(hasher, observation.TokenUsage.OutputTokens)
	writeObservationSampleInt(hasher, observation.TokenUsage.TotalTokens)
	writeObservationSampleProviderDiagnostics(hasher, observation.ProviderDiagnostics)
	writeObservationSampleString(hasher, string(observation.ModelErrorSubcategory))
	writeObservationSampleBool(hasher, observation.Approved)
	writeObservationSampleString(hasher, observation.ApprovalReason)
	writeObservationSampleString(hasher, string(observation.ErrorCategory))
	writeObservationSampleBool(hasher, observation.Failed)
}

func writeObservationSampleProviderDiagnostics(hasher hash.Hash64, diagnostics ProviderDiagnostics) {
	writeObservationSampleString(hasher, diagnostics.Provider)
	writeObservationSampleInt(hasher, diagnostics.HTTPStatus)
	writeObservationSampleString(hasher, diagnostics.EndpointHost)
	writeObservationSampleString(hasher, diagnostics.RequestID)
	writeObservationSampleString(hasher, diagnostics.RetryAfter)
	writeObservationSampleString(hasher, diagnostics.RateLimitLimit)
	writeObservationSampleString(hasher, diagnostics.RateLimitRemaining)
	writeObservationSampleString(hasher, diagnostics.RateLimitReset)
}

func writeObservationSampleString(hasher hash.Hash64, value string) {
	writeObservationSampleUint64(hasher, uint64(len(value)))
	_, _ = hasher.Write([]byte(value))
}

func writeObservationSampleBool(hasher hash.Hash64, value bool) {
	if value {
		writeObservationSampleUint64(hasher, 1)
		return
	}
	writeObservationSampleUint64(hasher, 0)
}

func writeObservationSampleInt(hasher hash.Hash64, value int) {
	writeObservationSampleInt64(hasher, int64(value))
}

func writeObservationSampleInt64(hasher hash.Hash64, value int64) {
	writeObservationSampleUint64(hasher, uint64(value))
}

func writeObservationSampleUint64(hasher hash.Hash64, value uint64) {
	var buffer [8]byte
	binary.LittleEndian.PutUint64(buffer[:], value)
	_, _ = hasher.Write(buffer[:])
}
