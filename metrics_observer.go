package agent

import (
	"context"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultMetricsEventCounterName is incremented for every observation.
	DefaultMetricsEventCounterName = "agent_observations_total"
	// DefaultMetricsFailureCounterName is incremented for failed observations.
	DefaultMetricsFailureCounterName = "agent_observation_failures_total"
	// DefaultMetricsDurationName records positive observation durations.
	DefaultMetricsDurationName = "agent_observation_duration"
	// DefaultMetricsToolLifecycleDurationName records positive tool lifecycle segment durations.
	DefaultMetricsToolLifecycleDurationName = "agent_tool_lifecycle_duration"
)

// MetricLabel is a single low-cardinality metric dimension.
type MetricLabel struct {
	Name  string
	Value string
}

// MetricSink receives metric updates from MetricsObserver. Implement this
// interface to bridge observations to an application's metrics system.
type MetricSink interface {
	// AddCounter records a counter delta with the supplied low-cardinality labels.
	AddCounter(ctx context.Context, name string, delta int64, labels []MetricLabel)
	// RecordDuration records a positive duration with the supplied labels.
	RecordDuration(ctx context.Context, name string, duration time.Duration, labels []MetricLabel)
}

// MetricsObserverOptions configures MetricsObserver.
type MetricsObserverOptions struct {
	// Sink receives counters and duration recordings. A nil sink makes the
	// observer a no-op.
	Sink MetricSink
	// EventCounterName overrides the default event total counter name.
	EventCounterName string
	// FailureCounterName overrides the default failure counter name.
	FailureCounterName string
	// DurationName overrides the default positive duration recording name.
	DurationName string
	// ToolLifecycleDurationName overrides the default tool lifecycle segment duration name.
	ToolLifecycleDurationName string
}

// MetricsObserver records sanitized observation metrics through a caller-owned
// sink. It uses only fields from Observation and intentionally omits run,
// request, trace, and provider request IDs from labels.
type MetricsObserver struct {
	sink               MetricSink
	eventCounterName   string
	failureCounterName string
	durationName       string
	toolLifecycleName  string
}

// NewMetricsObserver returns an Observer that emits counters and positive
// durations without requiring a specific metrics backend.
func NewMetricsObserver(options MetricsObserverOptions) MetricsObserver {
	return MetricsObserver{
		sink:               options.Sink,
		eventCounterName:   defaultMetricName(options.EventCounterName, DefaultMetricsEventCounterName),
		failureCounterName: defaultMetricName(options.FailureCounterName, DefaultMetricsFailureCounterName),
		durationName:       defaultMetricName(options.DurationName, DefaultMetricsDurationName),
		toolLifecycleName:  defaultMetricName(options.ToolLifecycleDurationName, DefaultMetricsToolLifecycleDurationName),
	}
}

func (o MetricsObserver) Observe(ctx context.Context, observation Observation) {
	if o.sink == nil {
		return
	}
	labels := metricsObservationLabels(observation)
	o.sink.AddCounter(ctx, o.eventCounterName, 1, cloneMetricLabels(labels))
	if observation.Failed {
		o.sink.AddCounter(ctx, o.failureCounterName, 1, cloneMetricLabels(labels))
	}
	if observation.Duration > 0 {
		o.sink.RecordDuration(ctx, o.durationName, observation.Duration, cloneMetricLabels(labels))
	}
	o.recordToolLifecycleDurations(ctx, observation)
}

func (o MetricsObserver) recordToolLifecycleDurations(ctx context.Context, observation Observation) {
	if observation.Type != EventAfterTool {
		return
	}
	for _, segment := range []struct {
		phase    string
		duration time.Duration
	}{
		{phase: "validation", duration: observation.ToolTiming.Validation},
		{phase: "approval", duration: observation.ToolTiming.Approval},
		{phase: "execution", duration: observation.ToolTiming.Execution},
	} {
		if segment.duration <= 0 {
			continue
		}
		o.sink.RecordDuration(ctx, o.toolLifecycleName, segment.duration, metricsToolLifecycleLabels(observation, segment.phase))
	}
}

func defaultMetricName(name string, defaultName string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return defaultName
	}
	return name
}

func metricsObservationLabels(observation Observation) []MetricLabel {
	labels := []MetricLabel{
		{Name: TelemetryMetricLabelEvent, Value: strings.TrimSpace(string(observation.Type))},
		{Name: TelemetryMetricLabelFailed, Value: strconv.FormatBool(observation.Failed)},
	}
	labels = appendMetricLabel(labels, TelemetryMetricLabelErrorCategory, string(observation.ErrorCategory))
	labels = appendMetricLabel(labels, TelemetryMetricLabelModelErrorSubcategory, string(observation.ModelErrorSubcategory))
	labels = appendMetricLabel(labels, TelemetryMetricLabelToolName, observation.ToolName)
	labels = appendMetricLabel(labels, TelemetryMetricLabelToolRisk, string(observation.ToolRisk))
	labels = appendMetricLabel(labels, TelemetryMetricLabelProvider, observation.ProviderDiagnostics.Provider)
	if observation.ProviderDiagnostics.HTTPStatus > 0 {
		labels = append(labels, MetricLabel{Name: TelemetryMetricLabelHTTPStatus, Value: strconv.Itoa(observation.ProviderDiagnostics.HTTPStatus)})
	}
	return labels
}

func metricsToolLifecycleLabels(observation Observation, phase string) []MetricLabel {
	labels := []MetricLabel{
		{Name: TelemetryMetricLabelEvent, Value: strings.TrimSpace(string(observation.Type))},
		{Name: TelemetryMetricLabelFailed, Value: strconv.FormatBool(observation.Failed)},
	}
	labels = appendMetricLabel(labels, TelemetryMetricLabelErrorCategory, string(observation.ErrorCategory))
	labels = appendMetricLabel(labels, TelemetryMetricLabelToolRisk, string(observation.ToolRisk))
	labels = appendMetricLabel(labels, TelemetryMetricLabelToolPhase, phase)
	return labels
}

func appendMetricLabel(labels []MetricLabel, name string, value string) []MetricLabel {
	value = strings.TrimSpace(value)
	if value == "" {
		return labels
	}
	return append(labels, MetricLabel{Name: name, Value: value})
}

func cloneMetricLabels(labels []MetricLabel) []MetricLabel {
	return append([]MetricLabel(nil), labels...)
}
