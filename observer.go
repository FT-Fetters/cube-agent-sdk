package agent

import "context"

func notifyObserver(ctx context.Context, observer Observer, event Event) {
	if observer == nil {
		return
	}
	observation := ObservationFromEvent(event)
	// Telemetry is best-effort and must not change agent behavior.
	defer func() {
		_ = recover()
	}()
	observer.Observe(ctx, observation)
}
