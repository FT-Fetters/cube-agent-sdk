package stdio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cubence/cube-agent-sdk/internal/core"
	"github.com/cubence/cube-agent-sdk/internal/schema"
)

const mcpProtocolVersion = "2025-11-25"

var (
	ErrMCPProcessExited = errors.New("agent: mcp process exited")
	ErrMCPRPC           = errors.New("agent: mcp json-rpc error")
	ErrMCPToolNotFound  = errors.New("agent: mcp tool not found")
)

type MCPServerConfig = core.MCPServerConfig
type MCPTransport = core.MCPTransport
type ToolParametersSchema = schema.ToolParametersSchema
type SchemaType = schema.SchemaType
type ToolCall = core.ToolCall
type ToolResult = core.ToolResult

const (
	MCPTransportStdio = core.MCPTransportStdio
	SchemaTypeString  = schema.SchemaTypeString
	SchemaTypeNumber  = schema.SchemaTypeNumber
	SchemaTypeInteger = schema.SchemaTypeInteger
	SchemaTypeBoolean = schema.SchemaTypeBoolean
	SchemaTypeObject  = schema.SchemaTypeObject
	SchemaTypeArray   = schema.SchemaTypeArray
)

// MCPRPCError is returned when an MCP server responds with a JSON-RPC error.
type MCPRPCError struct {
	Code    int `json:"code"`
	Message string
	Data    any `json:"data,omitempty"`
}

func (e *MCPRPCError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return fmt.Sprintf("%v: code %d", ErrMCPRPC, e.Code)
	}
	return fmt.Sprintf("%v: code %d: %s", ErrMCPRPC, e.Code, e.Message)
}

func (e *MCPRPCError) Unwrap() error {
	return ErrMCPRPC
}

// MCPToolDescriptor describes a tool returned by MCP tools/list.
type MCPToolDescriptor struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// MCPContent is one content item returned by MCP tools/call.
type MCPContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// MCPToolCallResult is the decoded result from MCP tools/call.
type MCPToolCallResult struct {
	Content           []MCPContent   `json:"content,omitempty"`
	IsError           bool           `json:"isError,omitempty"`
	StructuredContent map[string]any `json:"structuredContent,omitempty"`
}

// TextContent returns MCP text content blocks joined for use as a ToolResult.
func (r MCPToolCallResult) TextContent() string {
	parts := make([]string, 0, len(r.Content))
	for _, content := range r.Content {
		if content.Type == "text" {
			parts = append(parts, content.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// MCPStdioClient manages one stdio MCP server subprocess.
type MCPStdioClient struct {
	name string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	writeMu sync.Mutex
	nextID  int64

	mu          sync.Mutex
	closed      bool
	processDone bool
	processErr  error
	pending     map[string]chan mcpPendingResponse
	waitDone    chan struct{}
	closeOnce   sync.Once

	toolsMu sync.Mutex
	tools   []MCPToolDescriptor
	toolSet map[string]struct{}
}

type mcpRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type mcpRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *MCPRPCError    `json:"error,omitempty"`
}

type mcpPendingResponse struct {
	response mcpRPCResponse
	err      error
}

type mcpListToolsResult struct {
	Tools      []MCPToolDescriptor `json:"tools"`
	NextCursor string              `json:"nextCursor,omitempty"`
}

type mcpCallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// StartMCPStdioClient launches a stdio MCP server and completes initialize.
func StartMCPStdioClient(ctx context.Context, config MCPServerConfig) (*MCPStdioClient, error) {
	if config.Transport != "" && config.Transport != MCPTransportStdio {
		return nil, fmt.Errorf("agent: mcp server %q uses unsupported transport %q", config.Name, config.Transport)
	}
	command := strings.TrimSpace(config.Command)
	if command == "" {
		return nil, errors.New("agent: mcp stdio command is required")
	}

	cmd := exec.Command(command, config.Args...)
	cmd.Env = append(os.Environ(), envPairs(config.Env)...)
	cmd.Stderr = io.Discard

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("agent: open mcp stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("agent: open mcp stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("agent: start mcp server %q: %w", config.Name, err)
	}

	client := &MCPStdioClient{
		name:     config.Name,
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		pending:  make(map[string]chan mcpPendingResponse),
		waitDone: make(chan struct{}),
	}
	if client.name == "" {
		client.name = command
	}

	go client.readLoop()
	go client.waitLoop()

	if err := client.initialize(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func envPairs(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	pairs := make([]string, 0, len(env))
	for key, value := range env {
		pairs = append(pairs, key+"="+value)
	}
	return pairs
}

func (c *MCPStdioClient) initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "cube-agent-sdk",
			"version": "0.0.0",
		},
	}
	var result json.RawMessage
	if err := c.sendRequest(ctx, "initialize", params, &result); err != nil {
		return c.mcpError("mcp.initialize", "", fmt.Errorf("agent: initialize mcp server %q: %w", c.name, err))
	}
	if err := c.sendNotification("notifications/initialized", nil); err != nil {
		return c.mcpError("mcp.initialize", "", fmt.Errorf("agent: send initialized notification to mcp server %q: %w", c.name, err))
	}
	return nil
}

// Close shuts down the server process by closing stdin, then killing it if needed.
func (c *MCPStdioClient) Close() error {
	if c == nil {
		return nil
	}
	var closeErr error
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()

		if c.stdin != nil {
			closeErr = c.stdin.Close()
		}

		select {
		case <-c.waitDone:
		case <-time.After(2 * time.Second):
			if c.cmd != nil && c.cmd.Process != nil {
				if err := c.cmd.Process.Kill(); err != nil && closeErr == nil {
					closeErr = err
				}
			}
			<-c.waitDone
		}
	})
	return closeErr
}

// ListTools calls MCP tools/list and refreshes the client's known tool cache.
func (c *MCPStdioClient) ListTools(ctx context.Context) ([]MCPToolDescriptor, error) {
	var all []MCPToolDescriptor
	var cursor string
	for {
		var params any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		var result mcpListToolsResult
		if err := c.sendRequest(ctx, "tools/list", params, &result); err != nil {
			return nil, c.mcpError("mcp.tools.list", "", err)
		}
		all = append(all, result.Tools...)
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	c.rememberTools(all)
	return cloneMCPToolDescriptors(all), nil
}

// Tools discovers MCP tools and adapts them to SDK Tool values.
func (c *MCPStdioClient) Tools(ctx context.Context) ([]core.Tool, error) {
	descriptors, err := c.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	tools := make([]core.Tool, 0, len(descriptors))
	for _, descriptor := range descriptors {
		tools = append(tools, &mcpStdioTool{
			client:      c,
			name:        descriptor.Name,
			description: descriptor.Description,
			parameters:  toolParametersSchemaFromMCP(descriptor.InputSchema),
		})
	}
	return tools, nil
}

// CallTool invokes an MCP tool by name using tools/call.
func (c *MCPStdioClient) CallTool(ctx context.Context, name string, arguments map[string]any) (MCPToolCallResult, error) {
	if err := c.ensureKnownTool(ctx, name); err != nil {
		return MCPToolCallResult{}, err
	}
	var result MCPToolCallResult
	err := c.sendRequest(ctx, "tools/call", mcpCallToolParams{
		Name:      name,
		Arguments: core.CloneAnyMap(arguments),
	}, &result)
	if err != nil {
		return MCPToolCallResult{}, c.mcpError("mcp.tool.call", name, err)
	}
	return result, nil
}

func (c *MCPStdioClient) ensureKnownTool(ctx context.Context, name string) error {
	c.toolsMu.Lock()
	_, ok := c.toolSet[name]
	loaded := c.toolSet != nil
	c.toolsMu.Unlock()
	if ok {
		return nil
	}
	if !loaded {
		if _, err := c.ListTools(ctx); err != nil {
			return err
		}
		c.toolsMu.Lock()
		_, ok = c.toolSet[name]
		c.toolsMu.Unlock()
		if ok {
			return nil
		}
	}
	return c.mcpError("mcp.tool.lookup", name, fmt.Errorf("%w: %s", ErrMCPToolNotFound, name))
}

func (c *MCPStdioClient) rememberTools(tools []MCPToolDescriptor) {
	c.toolsMu.Lock()
	defer c.toolsMu.Unlock()
	c.tools = cloneMCPToolDescriptors(tools)
	c.toolSet = make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		c.toolSet[tool.Name] = struct{}{}
	}
}

func cloneMCPToolDescriptors(tools []MCPToolDescriptor) []MCPToolDescriptor {
	if len(tools) == 0 {
		return nil
	}
	cloned := make([]MCPToolDescriptor, len(tools))
	for i, tool := range tools {
		cloned[i] = tool
		cloned[i].InputSchema = core.CloneAnyMap(tool.InputSchema)
	}
	return cloned
}

func (c *MCPStdioClient) sendRequest(ctx context.Context, method string, params any, result any) error {
	id := atomic.AddInt64(&c.nextID, 1)
	key := strconv.FormatInt(id, 10)
	responseCh := make(chan mcpPendingResponse, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("agent: mcp stdio client is closed")
	}
	if c.processDone {
		err := c.processExitedError(c.processErr)
		c.mu.Unlock()
		return err
	}
	c.pending[key] = responseCh
	c.mu.Unlock()

	request := mcpRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if err := c.writeMessage(request); err != nil {
		c.removePending(key)
		return err
	}

	select {
	case <-ctx.Done():
		c.removePending(key)
		return ctx.Err()
	case pending := <-responseCh:
		if pending.err != nil {
			return pending.err
		}
		if pending.response.Error != nil {
			return pending.response.Error
		}
		if result == nil {
			return nil
		}
		if len(pending.response.Result) == 0 {
			return errors.New("agent: mcp response missing result")
		}
		if err := json.Unmarshal(pending.response.Result, result); err != nil {
			return fmt.Errorf("agent: decode mcp response for %s: %w", method, err)
		}
		return nil
	}
}

func (c *MCPStdioClient) sendNotification(method string, params any) error {
	return c.writeMessage(mcpRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
}

func (c *MCPStdioClient) writeMessage(message any) error {
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("agent: encode mcp message: %w", err)
	}
	body = append(body, '\n')

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.stdin.Write(body); err != nil {
		if c.hasProcessExited() {
			return c.processExitedError(c.processErr)
		}
		return fmt.Errorf("agent: write mcp message: %w", err)
	}
	return nil
}

func (c *MCPStdioClient) readLoop() {
	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		var response mcpRPCResponse
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			c.failPending(fmt.Errorf("agent: decode mcp stdout: %w", err))
			return
		}
		if len(response.ID) == 0 {
			continue
		}
		key := string(response.ID)
		c.mu.Lock()
		responseCh := c.pending[key]
		delete(c.pending, key)
		c.mu.Unlock()
		if responseCh != nil {
			responseCh <- mcpPendingResponse{response: response}
		}
	}
	if err := scanner.Err(); err != nil {
		c.failPending(fmt.Errorf("agent: read mcp stdout: %w", err))
		return
	}
	c.failPending(c.processExitedError(nil))
}

func (c *MCPStdioClient) waitLoop() {
	err := c.cmd.Wait()
	c.mu.Lock()
	c.processDone = true
	c.processErr = err
	c.mu.Unlock()
	c.failPending(c.processExitedError(err))
	close(c.waitDone)
}

func (c *MCPStdioClient) failPending(err error) {
	c.mu.Lock()
	pending := c.pending
	c.pending = make(map[string]chan mcpPendingResponse)
	c.mu.Unlock()
	for _, responseCh := range pending {
		responseCh <- mcpPendingResponse{err: err}
	}
}

func (c *MCPStdioClient) removePending(key string) {
	c.mu.Lock()
	delete(c.pending, key)
	c.mu.Unlock()
}

func (c *MCPStdioClient) hasProcessExited() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.processDone
}

func (c *MCPStdioClient) processExitedError(err error) error {
	if err == nil {
		return fmt.Errorf("%w: %s", ErrMCPProcessExited, c.name)
	}
	return fmt.Errorf("%w: %s: %v", ErrMCPProcessExited, c.name, err)
}

func (c *MCPStdioClient) mcpError(operation string, toolName string, err error) error {
	if err == nil {
		return nil
	}
	wrapped := &core.AgentError{
		Category:  core.ErrorCategoryMCP,
		Operation: operation,
		Cause:     err,
	}
	wrapped.ToolName = toolName
	return wrapped
}

type mcpStdioTool struct {
	client      *MCPStdioClient
	name        string
	description string
	parameters  *ToolParametersSchema
}

func (t *mcpStdioTool) Name() string {
	return t.name
}

func (t *mcpStdioTool) Description() string {
	return t.description
}

func (t *mcpStdioTool) ParametersSchema() *ToolParametersSchema {
	return schema.Clone(t.parameters)
}

func (t *mcpStdioTool) Call(ctx context.Context, call ToolCall) (ToolResult, error) {
	result, err := t.client.CallTool(ctx, t.name, call.Arguments)
	if err != nil {
		return ToolResult{}, err
	}
	metadata := map[string]any{
		"mcpServer":  t.client.name,
		"mcpTool":    t.name,
		"mcpIsError": result.IsError,
	}
	if len(result.StructuredContent) > 0 {
		metadata["mcpStructuredContent"] = core.CloneAnyMap(result.StructuredContent)
	}
	return ToolResult{
		CallID:   call.ID,
		Name:     call.Name,
		Content:  result.TextContent(),
		Metadata: metadata,
	}, nil
}

func toolParametersSchemaFromMCP(schema map[string]any) *ToolParametersSchema {
	if len(schema) == 0 {
		return nil
	}
	converted := convertMCPSchema(schema)
	return &converted
}

func convertMCPSchema(schema map[string]any) ToolParametersSchema {
	converted := ToolParametersSchema{
		Type:        schemaTypeFromAny(schema["type"]),
		Description: stringFromAny(schema["description"]),
		Required:    stringsFromAnySlice(schema["required"]),
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		converted.Properties = make(map[string]ToolParametersSchema, len(properties))
		for name, property := range properties {
			propertySchema, ok := property.(map[string]any)
			if !ok {
				continue
			}
			converted.Properties[name] = convertMCPSchema(propertySchema)
		}
	}
	if items, ok := schema["items"].(map[string]any); ok {
		itemSchema := convertMCPSchema(items)
		converted.Items = &itemSchema
	}
	return converted
}

func schemaTypeFromAny(value any) SchemaType {
	switch typed := value.(type) {
	case string:
		return schemaTypeFromString(typed)
	case []any:
		for _, item := range typed {
			if schemaType := schemaTypeFromAny(item); schemaType != "" {
				return schemaType
			}
		}
	}
	return ""
}

func schemaTypeFromString(value string) SchemaType {
	switch SchemaType(value) {
	case SchemaTypeString, SchemaTypeNumber, SchemaTypeInteger, SchemaTypeBoolean, SchemaTypeObject, SchemaTypeArray:
		return SchemaType(value)
	default:
		return ""
	}
}

func stringFromAny(value any) string {
	text, _ := value.(string)
	return text
}

func stringsFromAnySlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	strings := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if ok {
			strings = append(strings, text)
		}
	}
	return strings
}
