package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/cubence/cube-agent-sdk/internal/core"
	"github.com/cubence/cube-agent-sdk/internal/schema"
)

const ProtocolVersion = "2025-11-25"

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
	MCPTransportSSE   = core.MCPTransportSSE
	MCPTransportHTTP  = core.MCPTransportHTTP
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
	return fmt.Sprintf("%v: code %d", ErrMCPRPC, e.Code)
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

type Transport interface {
	SendRequest(ctx context.Context, method string, params any, result any) error
	SendNotification(ctx context.Context, method string, params any) error
	Close() error
}

// Client provides the transport-neutral MCP runtime used by stdio, HTTP, and SSE.
type Client struct {
	name      string
	transport Transport

	mu     sync.Mutex
	closed bool

	toolsMu sync.Mutex
	tools   []MCPToolDescriptor
	toolSet map[string]struct{}
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *MCPRPCError    `json:"error,omitempty"`
}

type ListToolsResult struct {
	Tools      []MCPToolDescriptor `json:"tools"`
	NextCursor string              `json:"nextCursor,omitempty"`
}

type CallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// NewClient initializes a transport-neutral MCP client.
func NewClient(ctx context.Context, config MCPServerConfig, transport Transport, fallbackName string) (*Client, error) {
	if transport == nil {
		return nil, errors.New("agent: mcp transport is required")
	}
	name := strings.TrimSpace(config.Name)
	if name == "" {
		name = fallbackName
	}
	if name == "" {
		name = string(config.Transport)
	}
	client := &Client{name: name, transport: transport}
	if err := client.initialize(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (c *Client) Name() string {
	if c == nil {
		return ""
	}
	return c.name
}

func (c *Client) initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "cube-agent-sdk",
			"version": "0.0.0",
		},
	}
	var result json.RawMessage
	if err := c.sendRequest(ctx, "initialize", params, &result); err != nil {
		return MCPError("mcp.initialize", "", fmt.Errorf("agent: initialize mcp server %q: %w", c.name, err))
	}
	if err := c.sendNotification(ctx, "notifications/initialized", nil); err != nil {
		return MCPError("mcp.initialize", "", fmt.Errorf("agent: send initialized notification to mcp server %q: %w", c.name, err))
	}
	return nil
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	transport := c.transport
	c.mu.Unlock()
	if transport == nil {
		return nil
	}
	return transport.Close()
}

// ListTools calls MCP tools/list and refreshes the client's known tool cache.
func (c *Client) ListTools(ctx context.Context) ([]MCPToolDescriptor, error) {
	return c.listTools(ctx, "mcp.tools.list")
}

// RefreshTools explicitly refreshes the client's known tool cache.
func (c *Client) RefreshTools(ctx context.Context) ([]MCPToolDescriptor, error) {
	return c.listTools(ctx, "mcp.tools.refresh")
}

func (c *Client) listTools(ctx context.Context, operation string) ([]MCPToolDescriptor, error) {
	var all []MCPToolDescriptor
	var cursor string
	for {
		var params any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		var result ListToolsResult
		if err := c.sendRequest(ctx, "tools/list", params, &result); err != nil {
			return nil, MCPError(operation, "", err)
		}
		all = append(all, result.Tools...)
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	c.rememberTools(all)
	return CloneToolDescriptors(all), nil
}

// Tools discovers MCP tools and adapts them to SDK Tool values.
func (c *Client) Tools(ctx context.Context) ([]core.Tool, error) {
	descriptors, err := c.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	tools := make([]core.Tool, 0, len(descriptors))
	for _, descriptor := range descriptors {
		tools = append(tools, &tool{
			client:      c,
			name:        descriptor.Name,
			description: descriptor.Description,
			parameters:  ToolParametersSchemaFromMCP(descriptor.InputSchema),
		})
	}
	return tools, nil
}

// CallTool invokes an MCP tool by name using tools/call.
func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (MCPToolCallResult, error) {
	if err := c.ensureKnownTool(ctx, name); err != nil {
		return MCPToolCallResult{}, err
	}
	var result MCPToolCallResult
	err := c.sendRequest(ctx, "tools/call", CallToolParams{
		Name:      name,
		Arguments: core.CloneAnyMap(arguments),
	}, &result)
	if err != nil {
		return MCPToolCallResult{}, MCPError("mcp.tool.call", name, err)
	}
	return result, nil
}

// Health verifies that the server can respond to a lightweight MCP ping.
func (c *Client) Health(ctx context.Context) error {
	var result json.RawMessage
	if err := c.sendRequest(ctx, "ping", map[string]any{}, &result); err != nil {
		return MCPError("mcp.health", "", err)
	}
	return nil
}

func (c *Client) ensureKnownTool(ctx context.Context, name string) error {
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
	return MCPError("mcp.tool.lookup", name, fmt.Errorf("%w: %s", ErrMCPToolNotFound, name))
}

func (c *Client) rememberTools(tools []MCPToolDescriptor) {
	c.toolsMu.Lock()
	defer c.toolsMu.Unlock()
	c.tools = CloneToolDescriptors(tools)
	c.toolSet = make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		c.toolSet[tool.Name] = struct{}{}
	}
}

func (c *Client) sendRequest(ctx context.Context, method string, params any, result any) error {
	if err := c.ensureOpen(); err != nil {
		return err
	}
	return c.transport.SendRequest(ctx, method, params, result)
}

func (c *Client) sendNotification(ctx context.Context, method string, params any) error {
	if err := c.ensureOpen(); err != nil {
		return err
	}
	return c.transport.SendNotification(ctx, method, params)
}

func (c *Client) ensureOpen() error {
	if c == nil {
		return errors.New("agent: mcp client is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("agent: mcp client is closed")
	}
	return nil
}

func CloneToolDescriptors(tools []MCPToolDescriptor) []MCPToolDescriptor {
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

// MCPError wraps transport and JSON-RPC failures with SDK error classification.
func MCPError(operation string, toolName string, err error) error {
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

type tool struct {
	client      *Client
	name        string
	description string
	parameters  *ToolParametersSchema
}

func (t *tool) Name() string {
	return t.name
}

func (t *tool) Description() string {
	return t.description
}

func (t *tool) ParametersSchema() *ToolParametersSchema {
	return schema.Clone(t.parameters)
}

func (t *tool) Call(ctx context.Context, call ToolCall) (ToolResult, error) {
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

func ToolParametersSchemaFromMCP(schemaMap map[string]any) *ToolParametersSchema {
	if len(schemaMap) == 0 {
		return nil
	}
	converted := ConvertMCPSchema(schemaMap)
	return &converted
}

func ConvertMCPSchema(schemaMap map[string]any) ToolParametersSchema {
	converted := ToolParametersSchema{
		Type:        schemaTypeFromAny(schemaMap["type"]),
		Description: stringFromAny(schemaMap["description"]),
		Required:    stringsFromAnySlice(schemaMap["required"]),
	}
	if properties, ok := schemaMap["properties"].(map[string]any); ok {
		converted.Properties = make(map[string]ToolParametersSchema, len(properties))
		for name, property := range properties {
			propertySchema, ok := property.(map[string]any)
			if !ok {
				continue
			}
			converted.Properties[name] = ConvertMCPSchema(propertySchema)
		}
	}
	if items, ok := schemaMap["items"].(map[string]any); ok {
		itemSchema := ConvertMCPSchema(items)
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
	stringsValue := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if ok {
			stringsValue = append(stringsValue, text)
		}
	}
	return stringsValue
}
