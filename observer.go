package agent

import "context"

// MultiObserver forwards each observation to multiple child observers.
// Nil children are ignored. Each child is isolated so a panic in one observer
// cannot prevent later observers from receiving the same observation.
type MultiObserver []Observer

// Observers returns a fan-out observer for the non-nil child observers.
func Observers(observers ...Observer) MultiObserver {
	group := make(MultiObserver, 0, len(observers))
	for _, observer := range observers {
		if observer != nil {
			group = append(group, observer)
		}
	}
	return group
}

// Observe forwards observation to each non-nil child observer.
func (m MultiObserver) Observe(ctx context.Context, observation Observation) {
	for _, observer := range m {
		if observer == nil {
			continue
		}
		observeChild(ctx, observer, observation)
	}
}

func observeChild(ctx context.Context, observer Observer, observation Observation) {
	// Telemetry fan-out is best-effort for each child independently.
	defer func() {
		_ = recover()
	}()
	observer.Observe(ctx, observation)
}

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
