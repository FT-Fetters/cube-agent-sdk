package agent

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"
)

var (
	// ErrReliableRateLimited is returned when a reliable model wrapper rejects a
	// call before model execution because its local rate limit is exhausted.
	ErrReliableRateLimited = errors.New("agent: reliable model rate limited")
	// ErrReliableCircuitOpen is returned when a reliable model wrapper rejects a
	// call because the circuit breaker is open.
	ErrReliableCircuitOpen = errors.New("agent: reliable model circuit open")
	// ErrReliableBudgetExceeded is returned when a reliable model wrapper rejects
	// a call before model execution because the token or cost budget is exhausted.
	ErrReliableBudgetExceeded = errors.New("agent: reliable model budget exceeded")
)

var (
	errReliableModelRequired = errors.New("agent: reliable model requires a model")
	errReliableNilStream     = errors.New("agent: stream model returned nil event channel")
)

// ReliabilityOperation identifies which model entrypoint produced a reliability
// event. Streaming events cover stream startup; mid-stream deltas are not
// retried by the wrapper.
type ReliabilityOperation string

const (
	// ReliabilityOperationGenerate identifies a Model.Generate call.
	ReliabilityOperationGenerate ReliabilityOperation = "generate"
	// ReliabilityOperationStream identifies a StreamModel.Stream startup call.
	ReliabilityOperationStream ReliabilityOperation = "stream"
)

// ReliabilityEventType identifies safe reliability wrapper lifecycle events.
type ReliabilityEventType string

const (
	// ReliabilityEventAttemptStart is emitted immediately before a model attempt.
	ReliabilityEventAttemptStart ReliabilityEventType = "attempt_start"
	// ReliabilityEventAttemptFailure is emitted when the wrapped model returns an error.
	ReliabilityEventAttemptFailure ReliabilityEventType = "attempt_failure"
	// ReliabilityEventRetryScheduled is emitted after a retryable failure and before backoff sleep.
	ReliabilityEventRetryScheduled ReliabilityEventType = "retry_scheduled"
	// ReliabilityEventFinalFailure is emitted when no more attempts will be made.
	ReliabilityEventFinalFailure ReliabilityEventType = "final_failure"
	// ReliabilityEventSuccess is emitted when Generate succeeds or Stream starts.
	ReliabilityEventSuccess ReliabilityEventType = "success"
	// ReliabilityEventBudgetRejected is emitted when token or cost budget rejects a call.
	ReliabilityEventBudgetRejected ReliabilityEventType = "budget_rejected"
	// ReliabilityEventRateRejected is emitted when the local rate limit rejects a call.
	ReliabilityEventRateRejected ReliabilityEventType = "rate_rejected"
	// ReliabilityEventCircuitRejected is emitted when an open circuit rejects a call.
	ReliabilityEventCircuitRejected ReliabilityEventType = "circuit_rejected"
)

// ReliabilityEvent is the safe observability payload emitted by reliable model
// wrappers. It intentionally omits prompt text, messages, tool arguments, tool
// results, raw provider errors, and other sensitive payloads.
type ReliabilityEvent struct {
	Type      ReliabilityEventType
	Operation ReliabilityOperation

	Attempt     int
	MaxAttempts int
	Retryable   bool
	Backoff     time.Duration
	Duration    time.Duration

	EstimatedInputTokens int
	TokenUsage           TokenUsage
	TokenBudgetRemaining int
	CostBudgetRemaining  float64
	// BudgetKind is "token" or "cost" on budget rejection events.
	BudgetKind string

	ProviderDiagnostics   ProviderDiagnostics
	ModelErrorSubcategory ModelErrorSubcategory
}

// ReliabilityObserver receives safe reliability events from NewReliableModel.
type ReliabilityObserver func(context.Context, ReliabilityEvent)

// ReliableBackoffFunc returns the delay before the retry following a failed
// attempt. The attempt argument is one-based and refers to the attempt that just
// failed.
type ReliableBackoffFunc func(attempt int) time.Duration

// ReliableModelOption configures NewReliableModel.
type ReliableModelOption func(*reliableModelConfig)

// NewReliableModel wraps a Model with local timeout, retry, backoff, rate limit,
// circuit breaker, and budget controls. If model implements StreamModel, the
// returned value also implements StreamModel and applies the same pre-start
// controls to Stream calls.
func NewReliableModel(model Model, options ...ReliableModelOption) Model {
	config := defaultReliableModelConfig()
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	config.normalize()

	wrapped := &reliableModel{
		model:  model,
		config: config,
	}
	capabilities, hasCapabilities := CapabilitiesOf(model)
	if streamModel, ok := model.(StreamModel); ok {
		stream := &reliableStreamModel{
			reliableModel: wrapped,
			stream:        streamModel,
		}
		if hasCapabilities {
			return &reliableCapabilityStreamModel{
				reliableStreamModel: stream,
				capabilities:        capabilities,
			}
		}
		return stream
	}
	if hasCapabilities {
		capabilities.Streaming = false
		return &reliableCapabilityModel{
			reliableModel: wrapped,
			capabilities:  capabilities,
		}
	}
	return wrapped
}

// WithReliableMaxAttempts sets the maximum number of total attempts, including
// the first try. Values less than one are normalized to one.
func WithReliableMaxAttempts(maxAttempts int) ReliableModelOption {
	return func(config *reliableModelConfig) {
		config.maxAttempts = maxAttempts
	}
}

// WithReliableBackoff sets the retry delay function. Nil restores the default
// capped exponential backoff.
func WithReliableBackoff(backoff ReliableBackoffFunc) ReliableModelOption {
	return func(config *reliableModelConfig) {
		config.backoff = backoff
	}
}

// WithReliablePerAttemptTimeout bounds each Generate attempt. For Stream calls,
// it bounds stream startup only; the wrapper does not retry after a stream has
// started returning events.
func WithReliablePerAttemptTimeout(timeout time.Duration) ReliableModelOption {
	return func(config *reliableModelConfig) {
		config.perAttemptTimeout = timeout
	}
}

// WithReliableTotalTimeout bounds the total elapsed Generate operation,
// including retries and backoff. For Stream calls, it bounds startup attempts.
func WithReliableTotalTimeout(timeout time.Duration) ReliableModelOption {
	return func(config *reliableModelConfig) {
		config.totalTimeout = timeout
	}
}

// WithReliableRateLimit enables a local fixed-window rate limiter. Calls beyond
// maxRequests in interval are rejected immediately with ErrReliableRateLimited.
func WithReliableRateLimit(maxRequests int, interval time.Duration) ReliableModelOption {
	return func(config *reliableModelConfig) {
		config.rateLimit = maxRequests
		config.rateLimitInterval = interval
	}
}

// WithReliableCircuitBreaker opens the circuit after failureThreshold
// consecutive final model failures and rejects calls until cooldown elapses.
func WithReliableCircuitBreaker(failureThreshold int, cooldown time.Duration) ReliableModelOption {
	return func(config *reliableModelConfig) {
		config.circuitFailureThreshold = failureThreshold
		config.circuitCooldown = cooldown
	}
}

// WithReliableTokenBudget caps cumulative estimated and reported tokens across
// calls made through this wrapper. Input is estimated before each attempt;
// provider-reported usage reconciles the budget after successful responses.
func WithReliableTokenBudget(maxTokens int) ReliableModelOption {
	return func(config *reliableModelConfig) {
		config.maxTokens = maxTokens
	}
}

// WithReliableCostBudget caps cumulative estimated and reported cost across
// calls made through this wrapper. Prices are expressed per 1,000 tokens in the
// caller's preferred currency unit.
func WithReliableCostBudget(maxCost float64, inputCostPer1KTokens float64, outputCostPer1KTokens float64) ReliableModelOption {
	return func(config *reliableModelConfig) {
		config.maxCost = maxCost
		config.inputCostPer1KTokens = inputCostPer1KTokens
		config.outputCostPer1KTokens = outputCostPer1KTokens
	}
}

// WithReliableTokenCounter sets the token estimator used before a request is
// sent. Nil restores ApproxTokenCounter.
func WithReliableTokenCounter(counter TokenCounter) ReliableModelOption {
	return func(config *reliableModelConfig) {
		config.tokenCounter = counter
	}
}

// WithReliabilityObserver registers a safe callback for reliability events. The
// callback is best-effort; panics are recovered so observation cannot alter model
// behavior.
func WithReliabilityObserver(observer ReliabilityObserver) ReliableModelOption {
	return func(config *reliableModelConfig) {
		config.observer = observer
	}
}

type reliableModelConfig struct {
	maxAttempts       int
	backoff           ReliableBackoffFunc
	perAttemptTimeout time.Duration
	totalTimeout      time.Duration

	rateLimit         int
	rateLimitInterval time.Duration

	circuitFailureThreshold int
	circuitCooldown         time.Duration

	maxTokens             int
	maxCost               float64
	inputCostPer1KTokens  float64
	outputCostPer1KTokens float64
	tokenCounter          TokenCounter
	observer              ReliabilityObserver
	now                   func() time.Time
	sleep                 func(context.Context, time.Duration) error
}

func defaultReliableModelConfig() reliableModelConfig {
	return reliableModelConfig{
		maxAttempts:  3,
		backoff:      defaultReliableBackoff,
		tokenCounter: ApproxTokenCounter{},
		now:          time.Now,
		sleep:        reliableSleep,
	}
}

func (config *reliableModelConfig) normalize() {
	if config.maxAttempts < 1 {
		config.maxAttempts = 1
	}
	if config.backoff == nil {
		config.backoff = defaultReliableBackoff
	}
	if config.perAttemptTimeout < 0 {
		config.perAttemptTimeout = 0
	}
	if config.totalTimeout < 0 {
		config.totalTimeout = 0
	}
	if config.rateLimit < 1 || config.rateLimitInterval <= 0 {
		config.rateLimit = 0
		config.rateLimitInterval = 0
	}
	if config.circuitFailureThreshold < 1 || config.circuitCooldown <= 0 {
		config.circuitFailureThreshold = 0
		config.circuitCooldown = 0
	}
	if config.maxTokens < 1 {
		config.maxTokens = 0
	}
	if config.maxCost <= 0 {
		config.maxCost = 0
	}
	if config.inputCostPer1KTokens < 0 {
		config.inputCostPer1KTokens = 0
	}
	if config.outputCostPer1KTokens < 0 {
		config.outputCostPer1KTokens = 0
	}
	if config.tokenCounter == nil {
		config.tokenCounter = ApproxTokenCounter{}
	}
	if config.now == nil {
		config.now = time.Now
	}
	if config.sleep == nil {
		config.sleep = reliableSleep
	}
}

type reliableModel struct {
	model  Model
	config reliableModelConfig

	mu                 sync.Mutex
	rateWindowStart    time.Time
	rateWindowRequests int
	circuitFailures    int
	circuitOpenedUntil time.Time
	usedTokens         int
	usedCost           float64
}

func (m *reliableModel) Generate(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if m.config.totalTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.config.totalTimeout)
		defer cancel()
	}

	estimatedTokens := m.estimateInputTokens(request)
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			m.emitFailureEvent(ctx, ReliabilityOperationGenerate, ReliabilityEventFinalFailure, attempt, err, 0, false, estimatedTokens)
			return ModelResponse{}, err
		}

		reservation, err := m.beginAttempt(ctx, ReliabilityOperationGenerate, attempt, estimatedTokens)
		if err != nil {
			return ModelResponse{}, err
		}

		attemptCtx := ctx
		var cancel context.CancelFunc
		if m.config.perAttemptTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, m.config.perAttemptTimeout)
		}
		started := m.config.now()
		response, err := m.callGenerate(attemptCtx, request)
		duration := m.config.now().Sub(started)
		if cancel != nil {
			cancel()
		}

		if err == nil {
			reservation.reconcile(response.Usage)
			m.recordCircuitSuccess()
			m.emitReliabilityEvent(ctx, m.reliabilityEvent(ReliabilityEvent{
				Type:                 ReliabilityEventSuccess,
				Operation:            ReliabilityOperationGenerate,
				Attempt:              attempt,
				MaxAttempts:          m.config.maxAttempts,
				Duration:             duration,
				EstimatedInputTokens: estimatedTokens,
				TokenUsage:           response.Usage,
			}))
			return response, nil
		}

		retryable := attempt < m.config.maxAttempts && m.shouldRetry(ctx, err)
		m.emitFailureEvent(ctx, ReliabilityOperationGenerate, ReliabilityEventAttemptFailure, attempt, err, duration, retryable, estimatedTokens)
		if !retryable {
			m.recordCircuitAttemptFailure(ctx, err)
			m.emitFailureEvent(ctx, ReliabilityOperationGenerate, ReliabilityEventFinalFailure, attempt, err, duration, false, estimatedTokens)
			return ModelResponse{}, err
		}

		backoff := m.retryBackoff(attempt)
		m.emitReliabilityEvent(ctx, m.reliabilityEvent(ReliabilityEvent{
			Type:                 ReliabilityEventRetryScheduled,
			Operation:            ReliabilityOperationGenerate,
			Attempt:              attempt,
			MaxAttempts:          m.config.maxAttempts,
			Backoff:              backoff,
			EstimatedInputTokens: estimatedTokens,
		}))
		if err := m.config.sleep(ctx, backoff); err != nil {
			m.emitFailureEvent(ctx, ReliabilityOperationGenerate, ReliabilityEventFinalFailure, attempt, err, 0, false, estimatedTokens)
			return ModelResponse{}, err
		}
	}
}

func (m *reliableModel) callGenerate(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	if m.model == nil {
		return ModelResponse{}, errReliableModelRequired
	}
	return m.model.Generate(ctx, request)
}

type reliableStreamModel struct {
	*reliableModel
	stream StreamModel
}

func (m *reliableStreamModel) Stream(ctx context.Context, request ModelRequest) (<-chan StreamEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	estimatedTokens := m.estimateInputTokens(request)
	startedOperation := m.config.now()
	var totalDeadline time.Time
	if m.config.totalTimeout > 0 {
		totalDeadline = startedOperation.Add(m.config.totalTimeout)
	}

	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			m.emitFailureEvent(ctx, ReliabilityOperationStream, ReliabilityEventFinalFailure, attempt, err, 0, false, estimatedTokens)
			return nil, err
		}
		if !totalDeadline.IsZero() && !m.config.now().Before(totalDeadline) {
			err := context.DeadlineExceeded
			m.emitFailureEvent(ctx, ReliabilityOperationStream, ReliabilityEventFinalFailure, attempt, err, 0, false, estimatedTokens)
			return nil, err
		}

		reservation, err := m.beginAttempt(ctx, ReliabilityOperationStream, attempt, estimatedTokens)
		if err != nil {
			return nil, err
		}

		startTimeout := m.streamStartTimeout(totalDeadline)
		started := m.config.now()
		events, cancel, err := m.callStreamStart(ctx, request, startTimeout)
		duration := m.config.now().Sub(started)
		if err == nil {
			m.recordCircuitSuccess()
			m.emitReliabilityEvent(ctx, m.reliabilityEvent(ReliabilityEvent{
				Type:                 ReliabilityEventSuccess,
				Operation:            ReliabilityOperationStream,
				Attempt:              attempt,
				MaxAttempts:          m.config.maxAttempts,
				Duration:             duration,
				EstimatedInputTokens: estimatedTokens,
			}))
			return m.wrapStream(ctx, events, reservation, cancel), nil
		}

		retryable := attempt < m.config.maxAttempts && m.shouldRetry(ctx, err)
		m.emitFailureEvent(ctx, ReliabilityOperationStream, ReliabilityEventAttemptFailure, attempt, err, duration, retryable, estimatedTokens)
		if !retryable {
			m.recordCircuitAttemptFailure(ctx, err)
			m.emitFailureEvent(ctx, ReliabilityOperationStream, ReliabilityEventFinalFailure, attempt, err, duration, false, estimatedTokens)
			return nil, err
		}

		backoff := m.retryBackoff(attempt)
		m.emitReliabilityEvent(ctx, m.reliabilityEvent(ReliabilityEvent{
			Type:                 ReliabilityEventRetryScheduled,
			Operation:            ReliabilityOperationStream,
			Attempt:              attempt,
			MaxAttempts:          m.config.maxAttempts,
			Backoff:              backoff,
			EstimatedInputTokens: estimatedTokens,
		}))
		if err := m.sleepWithDeadline(ctx, backoff, totalDeadline); err != nil {
			m.emitFailureEvent(ctx, ReliabilityOperationStream, ReliabilityEventFinalFailure, attempt, err, 0, false, estimatedTokens)
			return nil, err
		}
	}
}

func (m *reliableStreamModel) callStreamStart(ctx context.Context, request ModelRequest, timeout time.Duration) (<-chan StreamEvent, context.CancelFunc, error) {
	if m.stream == nil {
		return nil, nil, ErrStreamingUnsupported
	}
	streamCtx, cancel := context.WithCancel(ctx)
	type streamStartResult struct {
		events <-chan StreamEvent
		err    error
	}
	results := make(chan streamStartResult, 1)
	go func() {
		events, err := m.stream.Stream(streamCtx, request)
		results <- streamStartResult{events: events, err: err}
	}()

	if timeout <= 0 {
		result := <-results
		if result.err != nil {
			cancel()
			return nil, nil, result.err
		}
		if result.events == nil {
			cancel()
			return nil, nil, errReliableNilStream
		}
		return result.events, cancel, nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-results:
		if result.err != nil {
			cancel()
			return nil, nil, result.err
		}
		if result.events == nil {
			cancel()
			return nil, nil, errReliableNilStream
		}
		return result.events, cancel, nil
	case <-ctx.Done():
		cancel()
		return nil, nil, ctx.Err()
	case <-timer.C:
		cancel()
		return nil, nil, context.DeadlineExceeded
	}
}

func (m *reliableStreamModel) streamStartTimeout(totalDeadline time.Time) time.Duration {
	timeout := m.config.perAttemptTimeout
	if !totalDeadline.IsZero() {
		remaining := time.Until(totalDeadline)
		if remaining <= 0 {
			return time.Nanosecond
		}
		if timeout <= 0 || remaining < timeout {
			timeout = remaining
		}
	}
	return timeout
}

func (m *reliableModel) wrapStream(ctx context.Context, events <-chan StreamEvent, reservation *reliableBudgetReservation, cancel context.CancelFunc) <-chan StreamEvent {
	out := make(chan StreamEvent)
	go func() {
		defer close(out)
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				if event.Type == StreamEventDone {
					reservation.reconcile(event.Usage)
				}
				select {
				case <-ctx.Done():
					return
				case out <- event:
				}
			}
		}
	}()
	return out
}

func (m *reliableModel) beginAttempt(ctx context.Context, operation ReliabilityOperation, attempt int, estimatedTokens int) (*reliableBudgetReservation, error) {
	if m.circuitIsOpen() {
		m.emitReliabilityEvent(ctx, m.reliabilityEvent(ReliabilityEvent{
			Type:                 ReliabilityEventCircuitRejected,
			Operation:            operation,
			Attempt:              attempt,
			MaxAttempts:          m.config.maxAttempts,
			EstimatedInputTokens: estimatedTokens,
		}))
		return nil, ErrReliableCircuitOpen
	}

	reservation, budgetKind, ok := m.reserveBudget(estimatedTokens)
	if !ok {
		m.emitReliabilityEvent(ctx, m.reliabilityEvent(ReliabilityEvent{
			Type:                 ReliabilityEventBudgetRejected,
			Operation:            operation,
			Attempt:              attempt,
			MaxAttempts:          m.config.maxAttempts,
			EstimatedInputTokens: estimatedTokens,
			BudgetKind:           budgetKind,
		}))
		return nil, ErrReliableBudgetExceeded
	}

	if !m.allowRate() {
		reservation.release()
		m.emitReliabilityEvent(ctx, m.reliabilityEvent(ReliabilityEvent{
			Type:                 ReliabilityEventRateRejected,
			Operation:            operation,
			Attempt:              attempt,
			MaxAttempts:          m.config.maxAttempts,
			EstimatedInputTokens: estimatedTokens,
		}))
		return nil, ErrReliableRateLimited
	}

	m.emitReliabilityEvent(ctx, m.reliabilityEvent(ReliabilityEvent{
		Type:                 ReliabilityEventAttemptStart,
		Operation:            operation,
		Attempt:              attempt,
		MaxAttempts:          m.config.maxAttempts,
		EstimatedInputTokens: estimatedTokens,
	}))
	return reservation, nil
}

func (m *reliableModel) shouldRetry(ctx context.Context, err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || ctx.Err() != nil {
		return false
	}
	if category := classifyError(err); category != "" && category != ErrorCategoryModel {
		return false
	}
	if diagnostics, ok := ProviderDiagnosticsFromError(err); ok {
		status := diagnostics.HTTPStatus
		switch {
		case status == 408 || status == 429:
			return true
		case status >= 500 && status <= 599:
			return true
		case status == 401 || status == 403:
			return false
		case status >= 400 && status <= 499:
			return false
		}
	}
	if subcategory, ok := ModelErrorSubcategoryFromError(err); ok {
		switch subcategory {
		case ModelErrorSubcategoryTimeout, ModelErrorSubcategoryRateLimited, ModelErrorSubcategoryServerError:
			return true
		case ModelErrorSubcategoryAuth, ModelErrorSubcategoryBadRequest, ModelErrorSubcategoryDecodeError:
			return false
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}

func (m *reliableModel) emitFailureEvent(ctx context.Context, operation ReliabilityOperation, eventType ReliabilityEventType, attempt int, err error, duration time.Duration, retryable bool, estimatedTokens int) {
	diagnostics, subcategory := reliableFailureMetadata(err)
	m.emitReliabilityEvent(ctx, m.reliabilityEvent(ReliabilityEvent{
		Type:                  eventType,
		Operation:             operation,
		Attempt:               attempt,
		MaxAttempts:           m.config.maxAttempts,
		Retryable:             retryable,
		Duration:              duration,
		EstimatedInputTokens:  estimatedTokens,
		ProviderDiagnostics:   diagnostics,
		ModelErrorSubcategory: subcategory,
	}))
}

func reliableFailureMetadata(err error) (ProviderDiagnostics, ModelErrorSubcategory) {
	diagnostics, _ := ProviderDiagnosticsFromError(err)
	if diagnostics.HTTPStatus == 408 {
		return diagnostics, ModelErrorSubcategoryTimeout
	}
	if diagnostics.HTTPStatus == 429 {
		return diagnostics, ModelErrorSubcategoryRateLimited
	}
	if diagnostics.HTTPStatus >= 500 && diagnostics.HTTPStatus <= 599 {
		return diagnostics, ModelErrorSubcategoryServerError
	}
	if subcategory, ok := ModelErrorSubcategoryFromError(err); ok {
		return diagnostics, subcategory
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return diagnostics, ModelErrorSubcategoryTimeout
	}
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return diagnostics, ModelErrorSubcategoryTimeout
	}
	return diagnostics, ModelErrorSubcategoryUnknown
}

func (m *reliableModel) emitReliabilityEvent(ctx context.Context, event ReliabilityEvent) {
	if m.config.observer == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	m.config.observer(ctx, event)
}

func (m *reliableModel) reliabilityEvent(event ReliabilityEvent) ReliabilityEvent {
	tokenRemaining, costRemaining := m.budgetRemaining()
	event.TokenBudgetRemaining = tokenRemaining
	event.CostBudgetRemaining = costRemaining
	return event
}

func (m *reliableModel) retryBackoff(attempt int) time.Duration {
	backoff := m.config.backoff(attempt)
	if backoff < 0 {
		return 0
	}
	return backoff
}

func defaultReliableBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	backoff := 100 * time.Millisecond
	for i := 1; i < attempt; i++ {
		backoff *= 2
		if backoff >= 5*time.Second {
			return 5 * time.Second
		}
	}
	return backoff
}

func reliableSleep(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (m *reliableModel) sleepWithDeadline(ctx context.Context, delay time.Duration, deadline time.Time) error {
	if delay <= 0 {
		return nil
	}
	if deadline.IsZero() {
		return m.config.sleep(ctx, delay)
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return context.DeadlineExceeded
	}
	if delay > remaining {
		delay = remaining
	}
	return m.config.sleep(ctx, delay)
}

func (m *reliableModel) allowRate() bool {
	if m.config.rateLimit == 0 {
		return true
	}
	now := m.config.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rateWindowStart.IsZero() || now.Sub(m.rateWindowStart) >= m.config.rateLimitInterval {
		m.rateWindowStart = now
		m.rateWindowRequests = 0
	}
	if m.rateWindowRequests >= m.config.rateLimit {
		return false
	}
	m.rateWindowRequests++
	return true
}

func (m *reliableModel) circuitIsOpen() bool {
	if m.config.circuitFailureThreshold == 0 {
		return false
	}
	now := m.config.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.circuitOpenedUntil.IsZero() {
		return false
	}
	if now.Before(m.circuitOpenedUntil) {
		return true
	}
	m.circuitOpenedUntil = time.Time{}
	m.circuitFailures = 0
	return false
}

func (m *reliableModel) recordCircuitSuccess() {
	if m.config.circuitFailureThreshold == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.circuitFailures = 0
	m.circuitOpenedUntil = time.Time{}
}

func (m *reliableModel) recordCircuitAttemptFailure(ctx context.Context, err error) {
	if m.isContextFailureFromParent(ctx, err) {
		return
	}
	if !m.isCircuitFailure(err) {
		return
	}
	m.recordCircuitFailure()
}

func (m *reliableModel) isContextFailureFromParent(ctx context.Context, err error) bool {
	if ctx == nil || ctx.Err() == nil || err == nil {
		return false
	}
	if errors.Is(err, ctx.Err()) {
		return true
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return errors.Is(ctx.Err(), context.Canceled) && errors.Is(err, context.Canceled)
}

func (m *reliableModel) isCircuitFailure(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if category := classifyError(err); category != "" && category != ErrorCategoryModel {
		return false
	}
	diagnostics, hasDiagnostics := ProviderDiagnosticsFromError(err)
	if hasDiagnostics {
		status := diagnostics.HTTPStatus
		if status == 408 || status == 429 || (status >= 500 && status <= 599) {
			return true
		}
		if status >= 400 && status <= 499 {
			return false
		}
	}
	subcategory, hasSubcategory := ModelErrorSubcategoryFromError(err)
	if hasSubcategory {
		switch subcategory {
		case ModelErrorSubcategoryTimeout, ModelErrorSubcategoryRateLimited, ModelErrorSubcategoryServerError:
			return true
		case ModelErrorSubcategoryAuth, ModelErrorSubcategoryBadRequest, ModelErrorSubcategoryDecodeError:
			return false
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}

func (m *reliableModel) recordCircuitFailure() {
	if m.config.circuitFailureThreshold == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.circuitFailures++
	if m.circuitFailures >= m.config.circuitFailureThreshold {
		m.circuitOpenedUntil = m.config.now().Add(m.config.circuitCooldown)
	}
}

type reliableBudgetReservation struct {
	model           *reliableModel
	active          bool
	estimatedTokens int
	estimatedCost   float64
	released        bool
}

func (m *reliableModel) reserveBudget(estimatedTokens int) (*reliableBudgetReservation, string, bool) {
	if estimatedTokens < 0 {
		estimatedTokens = 0
	}
	estimatedCost := reliableInputCost(estimatedTokens, m.config.inputCostPer1KTokens)
	active := m.config.maxTokens > 0 || m.config.maxCost > 0
	reservation := &reliableBudgetReservation{
		model:           m,
		active:          active,
		estimatedTokens: estimatedTokens,
		estimatedCost:   estimatedCost,
	}
	if !active {
		return reservation, "", true
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config.maxTokens > 0 && m.usedTokens+estimatedTokens > m.config.maxTokens {
		return nil, "token", false
	}
	if m.config.maxCost > 0 && m.usedCost+estimatedCost > m.config.maxCost+1e-9 {
		return nil, "cost", false
	}
	m.usedTokens += estimatedTokens
	m.usedCost += estimatedCost
	return reservation, "", true
}

func (reservation *reliableBudgetReservation) release() {
	if reservation == nil || !reservation.active || reservation.released {
		return
	}
	reservation.model.mu.Lock()
	defer reservation.model.mu.Unlock()
	reservation.model.usedTokens -= reservation.estimatedTokens
	if reservation.model.usedTokens < 0 {
		reservation.model.usedTokens = 0
	}
	reservation.model.usedCost -= reservation.estimatedCost
	if reservation.model.usedCost < 0 {
		reservation.model.usedCost = 0
	}
	reservation.released = true
}

func (reservation *reliableBudgetReservation) reconcile(usage TokenUsage) {
	if reservation == nil || !reservation.active || reservation.released {
		return
	}
	actualTokens := reliableUsageTokens(usage)
	if actualTokens == 0 {
		actualTokens = reservation.estimatedTokens
	}
	actualCost := reliableUsageCost(usage, reservation.model.config)
	if actualCost == 0 {
		actualCost = reservation.estimatedCost
	}

	reservation.model.mu.Lock()
	defer reservation.model.mu.Unlock()
	reservation.model.usedTokens += actualTokens - reservation.estimatedTokens
	if reservation.model.usedTokens < 0 {
		reservation.model.usedTokens = 0
	}
	reservation.model.usedCost += actualCost - reservation.estimatedCost
	if reservation.model.usedCost < 0 {
		reservation.model.usedCost = 0
	}
}

func (m *reliableModel) budgetRemaining() (int, float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var tokenRemaining int
	if m.config.maxTokens > 0 {
		tokenRemaining = m.config.maxTokens - m.usedTokens
		if tokenRemaining < 0 {
			tokenRemaining = 0
		}
	}
	var costRemaining float64
	if m.config.maxCost > 0 {
		costRemaining = m.config.maxCost - m.usedCost
		if costRemaining < 0 {
			costRemaining = 0
		}
	}
	return tokenRemaining, costRemaining
}

func (m *reliableModel) estimateInputTokens(request ModelRequest) int {
	counter := m.config.tokenCounter
	total := 0
	if request.SystemPrompt != "" {
		total += reliableCountTokens(counter, Message{Role: RoleSystem, Content: request.SystemPrompt})
	}
	for _, message := range request.Messages {
		total += reliableCountTokens(counter, message)
	}
	return total
}

func reliableCountTokens(counter TokenCounter, message Message) int {
	if counter == nil {
		counter = ApproxTokenCounter{}
	}
	count := counter.Count(message)
	if count < 0 {
		return 0
	}
	return count
}

func reliableUsageTokens(usage TokenUsage) int {
	if usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	total := 0
	if usage.InputTokens > 0 {
		total += usage.InputTokens
	}
	if usage.OutputTokens > 0 {
		total += usage.OutputTokens
	}
	return total
}

func reliableInputCost(tokens int, costPer1KTokens float64) float64 {
	if tokens <= 0 || costPer1KTokens <= 0 {
		return 0
	}
	return float64(tokens) * costPer1KTokens / 1000
}

func reliableUsageCost(usage TokenUsage, config reliableModelConfig) float64 {
	input := usage.InputTokens
	output := usage.OutputTokens
	if input > 0 || output > 0 {
		return reliableInputCost(input, config.inputCostPer1KTokens) + reliableInputCost(output, config.outputCostPer1KTokens)
	}
	if usage.TotalTokens <= 0 {
		return 0
	}
	return reliableInputCost(usage.TotalTokens, math.Max(config.inputCostPer1KTokens, config.outputCostPer1KTokens))
}
