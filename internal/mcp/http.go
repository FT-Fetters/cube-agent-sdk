package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const maxHTTPResponseBytes = 10 * 1024 * 1024

// StartHTTPClient creates an MCP client that sends JSON-RPC requests over HTTP.
func StartHTTPClient(ctx context.Context, config MCPServerConfig) (*Client, error) {
	if config.Transport != MCPTransportHTTP {
		return nil, fmt.Errorf("agent: mcp server %q uses unsupported transport %q", config.Name, config.Transport)
	}
	endpoint, err := validateEndpoint(config.URL, "http")
	if err != nil {
		return nil, err
	}
	transport := newHTTPTransport(endpoint, nil)
	return NewClient(ctx, config, transport, "mcp-http")
}

// StartSSEClient creates an MCP client using an SSE endpoint event to discover
// the HTTP JSON-RPC endpoint used for client-to-server requests. Responses are
// read from JSON-RPC message events on the same SSE stream.
func StartSSEClient(ctx context.Context, config MCPServerConfig) (*Client, error) {
	if config.Transport != MCPTransportSSE {
		return nil, fmt.Errorf("agent: mcp server %q uses unsupported transport %q", config.Name, config.Transport)
	}
	sseURL, err := validateEndpoint(config.URL, "sse")
	if err != nil {
		return nil, err
	}
	connection, err := discoverSSEEndpoint(ctx, sseURL, http.DefaultClient)
	if err != nil {
		return nil, err
	}
	stream := newSSEResponseStream(connection.reader, connection.body)
	stream.Start()
	transport := newHTTPTransport(connection.endpoint, stream)
	client, err := NewClient(ctx, config, transport, "mcp-sse")
	if err != nil {
		_ = transport.Close()
		return nil, err
	}
	return client, nil
}

type httpTransport struct {
	endpoint string
	client   *http.Client
	sse      *sseResponseStream

	nextID int64

	mu     sync.Mutex
	closed bool
}

type httpStatusError struct {
	status int
	url    string
}

type httpRequestError struct {
	method    string
	url       string
	transient bool
}

type sseEndpointConnection struct {
	endpoint string
	reader   *bufio.Reader
	body     io.Closer
}

type sseEvent struct {
	name string
	data string
}

type ssePendingResponse struct {
	response RPCResponse
	err      error
}

type sseResponseStream struct {
	reader *bufio.Reader
	body   io.Closer

	mu        sync.Mutex
	closed    bool
	pending   map[string]chan ssePendingResponse
	closeOnce sync.Once
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("agent: mcp http request to %s failed with status %d", e.url, e.status)
}

func (e *httpRequestError) Error() string {
	return fmt.Sprintf("agent: mcp http request for %s to %s failed", e.method, e.url)
}

func newHTTPTransport(endpoint string, stream *sseResponseStream) *httpTransport {
	return &httpTransport{
		endpoint: endpoint,
		client:   http.DefaultClient,
		sse:      stream,
	}
}

func (t *httpTransport) SendRequest(ctx context.Context, method string, params any, result any) error {
	id := atomic.AddInt64(&t.nextID, 1)
	request := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	return t.sendWithRetry(ctx, method, request, result)
}

func (t *httpTransport) SendNotification(ctx context.Context, method string, params any) error {
	request := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return t.sendWithRetry(ctx, method, request, nil)
}

func (t *httpTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	stream := t.sse
	t.mu.Unlock()
	if stream != nil {
		return stream.Close()
	}
	return nil
}

func (t *httpTransport) sendWithRetry(ctx context.Context, method string, request rpcRequest, result any) error {
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("agent: encode mcp message: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < maxHTTPAttempts(method); attempt++ {
		if attempt > 0 {
			if err := sleepWithContext(ctx, httpBackoff(attempt)); err != nil {
				return err
			}
		}
		lastErr = t.sendOnce(ctx, method, request, body, result)
		if lastErr == nil {
			return nil
		}
		if !isTransientHTTPError(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

func (t *httpTransport) sendOnce(ctx context.Context, method string, request rpcRequest, body []byte, result any) error {
	if err := t.ensureOpen(); err != nil {
		return err
	}

	var responseCh <-chan ssePendingResponse
	var cleanup func()
	if request.ID != 0 && t.sse != nil {
		var err error
		responseCh, cleanup, err = t.sse.Register(strconv.FormatInt(request.ID, 10))
		if err != nil {
			return err
		}
		defer cleanup()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("agent: create mcp http request for %s to %s", method, safeURL(t.endpoint))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := t.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return &httpRequestError{method: method, url: safeURL(t.endpoint), transient: isTransientNetworkError(err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusAccepted {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		if request.ID == 0 {
			return nil
		}
		if responseCh != nil {
			response, err := waitForSSEResponse(ctx, responseCh)
			if err != nil {
				return err
			}
			return handleRPCResponse(response, method, result)
		}
		return errors.New("agent: mcp response missing result")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return &httpStatusError{status: resp.StatusCode, url: safeURL(t.endpoint)}
	}

	response, err := decodeHTTPRPCResponse(resp.Body, resp.Header.Get("Content-Type"), method, responseID(request.ID))
	if err != nil {
		return err
	}
	return handleRPCResponse(response, method, result)
}

func (t *httpTransport) ensureOpen() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return errors.New("agent: mcp http client is closed")
	}
	return nil
}

func handleRPCResponse(response RPCResponse, method string, result any) error {
	if response.Error != nil {
		return response.Error
	}
	if result == nil {
		return nil
	}
	if len(response.Result) == 0 {
		return errors.New("agent: mcp response missing result")
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		return fmt.Errorf("agent: decode mcp response for %s: %w", method, err)
	}
	return nil
}

func decodeHTTPRPCResponse(body io.Reader, contentType string, method string, expectedID string) (RPCResponse, error) {
	limited := io.LimitReader(body, maxHTTPResponseBytes)
	if strings.HasPrefix(strings.ToLower(contentType), "text/event-stream") {
		return decodeSSERPCResponse(limited, method, expectedID)
	}
	var response RPCResponse
	if err := json.NewDecoder(limited).Decode(&response); err != nil {
		return RPCResponse{}, fmt.Errorf("agent: decode mcp response for %s: %w", method, err)
	}
	if expectedID != "" && responseIDKey(response.ID) != "" && responseIDKey(response.ID) != expectedID {
		return RPCResponse{}, fmt.Errorf("agent: mcp response for %s had unexpected id", method)
	}
	return response, nil
}

func decodeSSERPCResponse(body io.Reader, method string, expectedID string) (RPCResponse, error) {
	reader := bufio.NewReader(io.LimitReader(body, maxHTTPResponseBytes))
	for {
		event, err := readSSEEvent(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return RPCResponse{}, fmt.Errorf("agent: mcp sse response for %s missing JSON-RPC message", method)
			}
			return RPCResponse{}, fmt.Errorf("agent: read mcp sse response for %s: %w", method, err)
		}
		if event.name != "message" {
			continue
		}
		var response RPCResponse
		if err := json.Unmarshal([]byte(event.data), &response); err != nil {
			return RPCResponse{}, fmt.Errorf("agent: decode mcp sse response for %s: %w", method, err)
		}
		if expectedID == "" || responseIDKey(response.ID) == expectedID {
			return response, nil
		}
	}
}

func discoverSSEEndpoint(ctx context.Context, sseURL string, client *http.Client) (*sseEndpointConnection, error) {
	var connection *sseEndpointConnection
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := sleepWithContext(ctx, httpBackoff(attempt)); err != nil {
				return nil, err
			}
		}
		connection, lastErr = discoverSSEEndpointOnce(ctx, sseURL, client)
		if lastErr == nil {
			return connection, nil
		}
		if !isTransientHTTPError(lastErr) {
			return nil, lastErr
		}
	}
	return nil, lastErr
}

func discoverSSEEndpointOnce(ctx context.Context, sseURL string, client *http.Client) (*sseEndpointConnection, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("agent: create mcp sse request to %s", safeURL(sseURL))
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, &httpRequestError{method: "sse.connect", url: safeURL(sseURL), transient: isTransientNetworkError(err)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, &httpStatusError{status: resp.StatusCode, url: safeURL(sseURL)}
	}

	reader := bufio.NewReader(resp.Body)
	for {
		event, err := readSSEEvent(reader)
		if err != nil {
			_ = resp.Body.Close()
			if errors.Is(err, io.EOF) {
				return nil, errors.New("agent: mcp sse endpoint event missing")
			}
			return nil, fmt.Errorf("agent: read mcp sse endpoint event from %s: %w", safeURL(sseURL), err)
		}
		if event.name != "endpoint" {
			continue
		}
		endpoint, err := resolveSSEEndpoint(sseURL, event.data)
		if err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		return &sseEndpointConnection{endpoint: endpoint, reader: reader, body: resp.Body}, nil
	}
}

func newSSEResponseStream(reader *bufio.Reader, body io.Closer) *sseResponseStream {
	return &sseResponseStream{
		reader:  reader,
		body:    body,
		pending: make(map[string]chan ssePendingResponse),
	}
}

func (s *sseResponseStream) Start() {
	go s.readLoop()
}

func (s *sseResponseStream) Register(id string) (<-chan ssePendingResponse, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, errors.New("agent: mcp sse stream is closed")
	}
	responseCh := make(chan ssePendingResponse, 1)
	s.pending[id] = responseCh
	cleanup := func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}
	return responseCh, cleanup, nil
}

func (s *sseResponseStream) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		closeErr = s.body.Close()
		s.failPending(errors.New("agent: mcp sse stream is closed"))
	})
	return closeErr
}

func (s *sseResponseStream) readLoop() {
	for {
		event, err := readSSEEvent(s.reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.failPending(errors.New("agent: mcp sse stream closed"))
				return
			}
			s.failPending(fmt.Errorf("agent: read mcp sse stream: %w", err))
			return
		}
		if event.name != "message" {
			continue
		}
		var response RPCResponse
		if err := json.Unmarshal([]byte(event.data), &response); err != nil {
			s.failPending(fmt.Errorf("agent: decode mcp sse message: %w", err))
			return
		}
		s.deliver(response)
	}
}

func (s *sseResponseStream) deliver(response RPCResponse) {
	id := responseIDKey(response.ID)
	if id == "" {
		return
	}
	s.mu.Lock()
	responseCh := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if responseCh != nil {
		responseCh <- ssePendingResponse{response: response}
	}
}

func (s *sseResponseStream) failPending(err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	pending := s.pending
	s.pending = make(map[string]chan ssePendingResponse)
	s.mu.Unlock()
	for _, responseCh := range pending {
		responseCh <- ssePendingResponse{err: err}
	}
}

func waitForSSEResponse(ctx context.Context, responseCh <-chan ssePendingResponse) (RPCResponse, error) {
	select {
	case <-ctx.Done():
		return RPCResponse{}, ctx.Err()
	case pending := <-responseCh:
		if pending.err != nil {
			return RPCResponse{}, pending.err
		}
		return pending.response, nil
	}
}

func readSSEEvent(reader *bufio.Reader) (sseEvent, error) {
	event := sseEvent{name: "message"}
	var data []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && len(data) > 0 {
				event.data = strings.Join(data, "\n")
				return event, nil
			}
			return sseEvent{}, err
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			if len(data) == 0 {
				event.name = "message"
				continue
			}
			event.data = strings.Join(data, "\n")
			return event, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event.name = value
		case "data":
			data = append(data, value)
		}
	}
}

func resolveSSEEndpoint(sseURL string, eventData string) (string, error) {
	endpoint := strings.TrimSpace(eventData)
	if endpoint == "" {
		return "", errors.New("agent: mcp sse endpoint event is empty")
	}
	if strings.HasPrefix(endpoint, "{") {
		var payload struct {
			URL      string `json:"url"`
			URI      string `json:"uri"`
			Endpoint string `json:"endpoint"`
		}
		if err := json.Unmarshal([]byte(endpoint), &payload); err != nil {
			return "", errors.New("agent: decode mcp sse endpoint event")
		}
		switch {
		case payload.URL != "":
			endpoint = payload.URL
		case payload.URI != "":
			endpoint = payload.URI
		case payload.Endpoint != "":
			endpoint = payload.Endpoint
		default:
			return "", errors.New("agent: mcp sse endpoint event missing URL")
		}
	}
	base, err := url.Parse(sseURL)
	if err != nil {
		return "", fmt.Errorf("agent: parse mcp sse url %s", safeURL(sseURL))
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", errors.New("agent: parse mcp sse endpoint")
	}
	return base.ResolveReference(parsed).String(), nil
}

func validateEndpoint(rawURL string, transport string) (string, error) {
	endpoint := strings.TrimSpace(rawURL)
	if endpoint == "" {
		return "", fmt.Errorf("agent: mcp %s url is required", transport)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("agent: parse mcp %s url", transport)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("agent: mcp %s url must use http or https", transport)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("agent: mcp %s url requires a host", transport)
	}
	return endpoint, nil
}

func safeURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "<redacted-url>"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}

func responseID(id int64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

func responseIDKey(id json.RawMessage) string {
	if len(id) == 0 {
		return ""
	}
	var number int64
	if err := json.Unmarshal(id, &number); err == nil {
		return strconv.FormatInt(number, 10)
	}
	var text string
	if err := json.Unmarshal(id, &text); err == nil {
		return text
	}
	return ""
}

func maxHTTPAttempts(method string) int {
	if method == "tools/call" {
		return 1
	}
	return 3
}

func httpBackoff(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 20 * time.Millisecond
	case 2:
		return 50 * time.Millisecond
	default:
		return 100 * time.Millisecond
	}
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isTransientHTTPError(err error) bool {
	if err == nil {
		return false
	}
	var requestErr *httpRequestError
	if errors.As(err, &requestErr) {
		return requestErr.transient
	}
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		return statusErr.status == http.StatusRequestTimeout ||
			statusErr.status == http.StatusTooManyRequests ||
			statusErr.status == http.StatusInternalServerError ||
			statusErr.status == http.StatusBadGateway ||
			statusErr.status == http.StatusServiceUnavailable ||
			statusErr.status == http.StatusGatewayTimeout
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return isTransientNetworkError(err)
}

func isTransientNetworkError(err error) bool {
	var netErr interface{ Temporary() bool }
	if errors.As(err, &netErr) && netErr.Temporary() {
		return true
	}
	// url.Error often wraps connection resets and refused connections without
	// exposing a Temporary method, so startup paths get a small retry window.
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}
