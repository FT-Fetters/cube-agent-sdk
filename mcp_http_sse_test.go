package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMCPHTTPClientListsCallsRefreshesHealthAndCloses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fixture := newMCPHTTPFixture(t)
	server := httptest.NewServer(fixture)
	defer server.Close()

	client, err := StartMCPClient(ctx, MCPServerConfig{
		Name:      "http-test",
		URL:       server.URL + "/rpc",
		Transport: MCPTransportHTTP,
	})
	if err != nil {
		t.Fatal(err)
	}

	tools, err := client.Tools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(tools))
	}
	if tools[0].Name() != "echo" || tools[0].Description() != "Echo text through MCP HTTP" {
		t.Fatalf("first tool metadata = %q/%q, want echo metadata", tools[0].Name(), tools[0].Description())
	}
	schemaProvider, ok := tools[0].(ToolParametersSchemaProvider)
	if !ok {
		t.Fatal("bridged HTTP MCP tool did not expose a parameter schema")
	}
	wantSchema := &ToolParametersSchema{
		Type:     SchemaTypeObject,
		Required: []string{"text"},
		Properties: map[string]ToolParametersSchema{
			"text": {Type: SchemaTypeString, Description: "Text to echo"},
		},
	}
	if gotSchema := schemaProvider.ParametersSchema(); !reflect.DeepEqual(gotSchema, wantSchema) {
		t.Fatalf("schema = %#v, want %#v", gotSchema, wantSchema)
	}

	result, err := tools[0].Call(ctx, ToolCall{
		ID:        "http-call-1",
		Name:      "echo",
		Arguments: map[string]any{"text": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "mcp http echoed hello" {
		t.Fatalf("result content = %q, want MCP HTTP echo", result.Content)
	}
	if result.CallID != "http-call-1" || result.Name != "echo" {
		t.Fatalf("result identity = %q/%q, want call id and tool name", result.CallID, result.Name)
	}
	if result.Metadata["mcpServer"] != "http-test" || result.Metadata["mcpTool"] != "echo" || result.Metadata["mcpIsError"] != false {
		t.Fatalf("result metadata = %#v, want safe MCP metadata", result.Metadata)
	}

	callsBeforeMissing := fixture.methodCount("tools/call")
	_, err = client.CallTool(ctx, "missing", map[string]any{"text": "secret-argument"})
	if !errors.Is(err, ErrMCPToolNotFound) {
		t.Fatalf("err = %v, want ErrMCPToolNotFound", err)
	}
	if callsAfterMissing := fixture.methodCount("tools/call"); callsAfterMissing != callsBeforeMissing {
		t.Fatalf("tools/call count after missing lookup = %d, want %d", callsAfterMissing, callsBeforeMissing)
	}

	fixture.setTools([]mcpHTTPFixtureTool{{Name: "fresh", Description: "Fresh MCP HTTP tool"}})
	refreshed, err := client.RefreshTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(refreshed) != 1 || refreshed[0].Name != "fresh" {
		t.Fatalf("refreshed tools = %#v, want fresh tool", refreshed)
	}

	if err := client.Health(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Tools(ctx); err == nil {
		t.Fatal("Tools after Close succeeded, want error")
	}
}

func TestMCPHTTPClientRetriesTransientInitializeFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fixture := newMCPHTTPFixture(t)
	fixture.failInitializeOnce(http.StatusServiceUnavailable)
	server := httptest.NewServer(fixture)
	defer server.Close()

	client, err := StartMCPClient(ctx, MCPServerConfig{
		Name:      "retry-test",
		URL:       server.URL + "/rpc",
		Transport: MCPTransportHTTP,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if got := fixture.methodCount("initialize"); got != 2 {
		t.Fatalf("initialize count = %d, want retry to call twice", got)
	}
}

func TestMCPHTTPClientReturnsJSONRPCError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fixture := newMCPHTTPFixture(t)
	fixture.rpcErrorOnCall(-32000, "tool exploded", "rpc-secret-data")
	server := httptest.NewServer(fixture)
	defer server.Close()

	client, err := StartMCPClient(ctx, MCPServerConfig{
		Name:      "rpc-error-test",
		URL:       server.URL + "/rpc",
		Transport: MCPTransportHTTP,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if _, err := client.Tools(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = client.CallTool(ctx, "echo", map[string]any{"text": "hello"})
	if !errors.Is(err, ErrMCPRPC) {
		t.Fatalf("err = %v, want ErrMCPRPC", err)
	}
	var rpcErr *MCPRPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("err = %T, want *MCPRPCError", err)
	}
	if rpcErr.Code != -32000 || rpcErr.Message != "tool exploded" {
		t.Fatalf("rpc error = %#v, want code/message from server", rpcErr)
	}
	if strings.Contains(err.Error(), "rpc-secret-data") {
		t.Fatalf("error leaked JSON-RPC data: %v", err)
	}
}

func TestMCPHTTPClientEmptyNameDoesNotLeakURLSecretsInStartupErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const userSecret = "redaction-user"
	const tokenSecret = "redaction-token"
	const querySecret = "redaction-query"
	const fragmentSecret = "redaction-fragment"
	const bodySecret = "redaction-body"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, bodySecret, http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := StartMCPClient(ctx, MCPServerConfig{
		URL:       mcpURLWithSecrets(t, server.URL+"/rpc", userSecret, tokenSecret, querySecret, fragmentSecret),
		Transport: MCPTransportHTTP,
	})
	if err == nil {
		t.Fatal("StartMCPClient succeeded, want startup error")
	}
	assertDoesNotContain(t, err.Error(), userSecret, tokenSecret, querySecret, fragmentSecret, bodySecret, "?api_key=", "#"+fragmentSecret)
}

func TestMCPSSEClientEmptyNameDoesNotLeakDiscoveredEndpointSecretsInStartupErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const userSecret = "sse-redaction-user"
	const tokenSecret = "sse-redaction-token"
	const querySecret = "sse-redaction-query"
	const fragmentSecret = "sse-redaction-fragment"
	const bodySecret = "sse-redaction-body"
	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: endpoint\n"))
		_, _ = fmt.Fprintf(w, "data: %s\n\n", mcpURLWithSecrets(t, server.URL+"/rpc", userSecret, tokenSecret, querySecret, fragmentSecret))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	})
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, bodySecret, http.StatusInternalServerError)
	})
	server = httptest.NewServer(mux)
	defer server.Close()

	_, err := StartMCPClient(ctx, MCPServerConfig{
		URL:       server.URL + "/events?api_key=configured-sse-secret",
		Transport: MCPTransportSSE,
	})
	if err == nil {
		t.Fatal("StartMCPClient succeeded, want startup error")
	}
	assertDoesNotContain(t, err.Error(), userSecret, tokenSecret, querySecret, fragmentSecret, bodySecret, "configured-sse-secret", "?api_key=", "#"+fragmentSecret)
}

func TestMCPHTTPClientRedactsURLQueryAndResponseBodies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const querySecret = "url-query-secret"
	const bodySecret = "raw-response-body-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, bodySecret, http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := StartMCPClient(ctx, MCPServerConfig{
		Name:      "redaction-test",
		URL:       server.URL + "/rpc?api_key=" + querySecret,
		Transport: MCPTransportHTTP,
	})
	if err == nil {
		t.Fatal("StartMCPClient succeeded, want error")
	}
	got := err.Error()
	for _, secret := range []string{querySecret, bodySecret, "?api_key="} {
		if strings.Contains(got, secret) {
			t.Fatalf("error %q leaked %q", got, secret)
		}
	}
}

func TestMCPHTTPClientDoesNotLeakToolArgumentsOrResponseBodiesInErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const argumentSecret = "argument-secret"
	const bodySecret = "tool-result-secret"
	fixture := newMCPHTTPFixture(t)
	fixture.callHTTPFailure(http.StatusBadGateway, argumentSecret+" "+bodySecret)
	server := httptest.NewServer(fixture)
	defer server.Close()

	client, err := StartMCPClient(ctx, MCPServerConfig{
		Name:      "call-redaction-test",
		URL:       server.URL + "/rpc",
		Transport: MCPTransportHTTP,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if _, err := client.Tools(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = client.CallTool(ctx, "echo", map[string]any{"text": argumentSecret})
	if err == nil {
		t.Fatal("CallTool succeeded, want error")
	}
	got := err.Error()
	for _, secret := range []string{argumentSecret, bodySecret} {
		if strings.Contains(got, secret) {
			t.Fatalf("error %q leaked %q", got, secret)
		}
	}
}

func TestMCPSSEClientDiscoversEndpointAndBridgesToolCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fixture := newMCPHTTPFixture(t)
	mux := http.NewServeMux()
	mux.Handle("/rpc", fixture)
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("SSE method = %s, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: endpoint\n"))
		_, _ = w.Write([]byte("data: /rpc\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client, err := StartMCPClient(ctx, MCPServerConfig{
		Name:      "sse-test",
		URL:       server.URL + "/events?token=sse-url-secret",
		Transport: MCPTransportSSE,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	tools, err := client.Tools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].Name() != "echo" {
		t.Fatalf("SSE tools = %#v, want bridged echo tools", tools)
	}
	result, err := client.CallTool(ctx, "echo", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if result.TextContent() != "mcp http echoed hello" {
		t.Fatalf("SSE call content = %q, want MCP echo", result.TextContent())
	}
}

func TestMCPHTTPClientRejectsAcceptedRequestWithoutJSONRPCResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	_, err := StartMCPClient(ctx, MCPServerConfig{
		Name:      "accepted-without-response-test",
		URL:       server.URL + "/rpc?token=accepted-secret",
		Transport: MCPTransportHTTP,
	})
	if err == nil {
		t.Fatal("StartMCPClient succeeded, want missing response error")
	}
	if strings.Contains(err.Error(), "accepted-secret") || strings.Contains(err.Error(), "?token=") {
		t.Fatalf("error leaked URL query: %v", err)
	}
}

func TestMCPSSEClientReceivesResponsesFromEventStreamWithAcceptedPosts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	responses := make(chan []byte, 16)
	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: endpoint\n"))
		_, _ = w.Write([]byte("data: /rpc\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		for {
			select {
			case <-r.Context().Done():
				return
			case body := <-responses:
				_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", body)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
		}
	})
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		var request mcpHTTPFixtureRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(request.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		responses <- mcpSSEFixtureResponse(t, request)
		w.WriteHeader(http.StatusAccepted)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client, err := StartMCPClient(ctx, MCPServerConfig{
		Name:      "legacy-sse-test",
		URL:       server.URL + "/events?token=sse-response-secret",
		Transport: MCPTransportSSE,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	tools, err := client.Tools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name() != "echo" {
		t.Fatalf("SSE event-stream tools = %#v, want echo", tools)
	}
	result, err := client.CallTool(ctx, "echo", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if result.TextContent() != "mcp sse echoed hello" {
		t.Fatalf("SSE event-stream call content = %q, want MCP echo", result.TextContent())
	}
}

func TestMCPHTTPClientStreamableSSEResponseScansForMatchingID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request mcpHTTPFixtureRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(request.ID) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\n"))
		_, _ = w.Write([]byte(`data: {"jsonrpc":"2.0","id":999,"result":{"ignored":true}}` + "\n\n"))
		_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", mcpSSEFixtureResponse(t, request))
	}))
	defer server.Close()

	client, err := StartMCPClient(ctx, MCPServerConfig{
		Name:      "streamable-http-test",
		URL:       server.URL + "/rpc",
		Transport: MCPTransportHTTP,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	tools, err := client.Tools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name() != "echo" {
		t.Fatalf("streamable HTTP tools = %#v, want echo", tools)
	}
}

type mcpHTTPFixture struct {
	t *testing.T

	mu                      sync.Mutex
	tools                   []mcpHTTPFixtureTool
	methods                 map[string]int
	initializeFailures      int
	initializeFailureStatus int
	callRPCErrorCode        int
	callRPCErrorMessage     string
	callRPCErrorData        any
	callHTTPFailureStatus   int
	callHTTPFailureBody     string
}

type mcpHTTPFixtureTool struct {
	Name        string
	Description string
}

type mcpHTTPFixtureRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpHTTPFixtureCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

func newMCPHTTPFixture(t *testing.T) *mcpHTTPFixture {
	t.Helper()
	return &mcpHTTPFixture{
		t: t,
		tools: []mcpHTTPFixtureTool{
			{Name: "echo", Description: "Echo text through MCP HTTP"},
			{Name: "wave", Description: "Wave through MCP HTTP"},
		},
		methods: make(map[string]int),
	}
}

func (f *mcpHTTPFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		f.t.Fatalf("method = %s, want POST", r.Method)
	}
	var request mcpHTTPFixtureRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		f.t.Fatalf("decode request: %v", err)
	}

	f.mu.Lock()
	f.methods[request.Method]++
	if request.Method == "initialize" && f.initializeFailures > 0 {
		f.initializeFailures--
		status := f.initializeFailureStatus
		f.mu.Unlock()
		http.Error(w, "transient initialize body should be redacted", status)
		return
	}
	tools := append([]mcpHTTPFixtureTool(nil), f.tools...)
	callRPCErrorCode := f.callRPCErrorCode
	callRPCErrorMessage := f.callRPCErrorMessage
	callRPCErrorData := f.callRPCErrorData
	callHTTPFailureStatus := f.callHTTPFailureStatus
	callHTTPFailureBody := f.callHTTPFailureBody
	f.mu.Unlock()

	switch request.Method {
	case "initialize":
		writeMCPHTTPFixtureResult(w, request.ID, map[string]any{
			"protocolVersion": "2025-11-25",
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
			"serverInfo":      map[string]any{"name": "fake-http-mcp", "version": "test"},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "ping":
		writeMCPHTTPFixtureResult(w, request.ID, map[string]any{})
	case "tools/list":
		cursor := ""
		if len(request.Params) > 0 {
			var params struct {
				Cursor string `json:"cursor"`
			}
			if err := json.Unmarshal(request.Params, &params); err != nil {
				f.t.Fatalf("decode list params: %v", err)
			}
			cursor = params.Cursor
		}
		if cursor == "" && len(tools) > 1 {
			writeMCPHTTPFixtureResult(w, request.ID, map[string]any{
				"tools":      []map[string]any{mcpHTTPFixtureToolDescriptor(tools[0])},
				"nextCursor": "page-2",
			})
			return
		}
		start := 0
		if cursor == "page-2" {
			start = 1
		}
		descriptors := make([]map[string]any, 0, len(tools)-start)
		for _, tool := range tools[start:] {
			descriptors = append(descriptors, mcpHTTPFixtureToolDescriptor(tool))
		}
		writeMCPHTTPFixtureResult(w, request.ID, map[string]any{"tools": descriptors})
	case "tools/call":
		if callHTTPFailureStatus != 0 {
			http.Error(w, callHTTPFailureBody, callHTTPFailureStatus)
			return
		}
		var params mcpHTTPFixtureCallParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			f.t.Fatalf("decode call params: %v", err)
		}
		if callRPCErrorCode != 0 {
			writeMCPHTTPFixtureError(w, request.ID, callRPCErrorCode, callRPCErrorMessage, callRPCErrorData)
			return
		}
		text, _ := params.Arguments["text"].(string)
		writeMCPHTTPFixtureResult(w, request.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": "mcp http echoed " + text}},
			"isError": false,
		})
	default:
		writeMCPHTTPFixtureError(w, request.ID, -32601, "method not found", nil)
	}
}

func (f *mcpHTTPFixture) setTools(tools []mcpHTTPFixtureTool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tools = append([]mcpHTTPFixtureTool(nil), tools...)
}

func (f *mcpHTTPFixture) failInitializeOnce(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.initializeFailures = 1
	f.initializeFailureStatus = status
}

func (f *mcpHTTPFixture) rpcErrorOnCall(code int, message string, data any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callRPCErrorCode = code
	f.callRPCErrorMessage = message
	f.callRPCErrorData = data
}

func (f *mcpHTTPFixture) callHTTPFailure(status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callHTTPFailureStatus = status
	f.callHTTPFailureBody = body
}

func (f *mcpHTTPFixture) methodCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.methods[method]
}

func mcpHTTPFixtureToolDescriptor(tool mcpHTTPFixtureTool) map[string]any {
	return map[string]any{
		"name":        tool.Name,
		"description": tool.Description,
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "Text to echo",
				},
			},
			"required": []string{"text"},
		},
	}
}

func writeMCPHTTPFixtureResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(append([]byte(nil), id...)),
		"result":  result,
	})
}

func writeMCPHTTPFixtureError(w http.ResponseWriter, id json.RawMessage, code int, message string, data any) {
	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(append([]byte(nil), id...)),
		"error": map[string]any{
			"code":    code,
			"message": message,
			"data":    data,
		},
	}
	_ = json.NewEncoder(w).Encode(response)
}

func mcpSSEFixtureResponse(t *testing.T, request mcpHTTPFixtureRequest) []byte {
	t.Helper()
	var result any
	switch request.Method {
	case "initialize":
		result = map[string]any{
			"protocolVersion": "2025-11-25",
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
			"serverInfo":      map[string]any{"name": "fake-sse-mcp", "version": "test"},
		}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = map[string]any{
			"tools": []map[string]any{mcpHTTPFixtureToolDescriptor(mcpHTTPFixtureTool{
				Name:        "echo",
				Description: "Echo text through MCP SSE",
			})},
		}
	case "tools/call":
		var params mcpHTTPFixtureCallParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			t.Fatalf("decode call params: %v", err)
		}
		text, _ := params.Arguments["text"].(string)
		result = map[string]any{
			"content": []map[string]any{{"type": "text", "text": "mcp sse echoed " + text}},
			"isError": false,
		}
	default:
		response, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      json.RawMessage(append([]byte(nil), request.ID...)),
			"error":   map[string]any{"code": -32601, "message": "method not found"},
		})
		if err != nil {
			t.Fatalf("encode error response: %v", err)
		}
		return response
	}
	response, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(append([]byte(nil), request.ID...)),
		"result":  result,
	})
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
	return response
}

func mcpURLWithSecrets(t *testing.T, rawURL string, userSecret string, tokenSecret string, querySecret string, fragmentSecret string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword(userSecret, tokenSecret)
	parsed.RawQuery = "api_key=" + querySecret
	parsed.Fragment = fragmentSecret
	return parsed.String()
}

func assertDoesNotContain(t *testing.T, text string, values ...string) {
	t.Helper()
	for _, value := range values {
		if value == "" {
			continue
		}
		if strings.Contains(text, value) {
			t.Fatalf("text %q leaked %q", text, value)
		}
	}
}
