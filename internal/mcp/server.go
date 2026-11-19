package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/mohammedrefat/go-anything/internal/mcp/sanitize"
)

// Standard JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// JSONRPCRequest represents an incoming JSON-RPC 2.0 request or notification.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"` // string, number, or null
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents an outgoing JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError defines structured JSON-RPC error fields.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema defines the JSON schema for tool arguments.
type InputSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]PropertyDef `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
}

// PropertyDef defines an individual schema property.
type PropertyDef struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// ToolCallParams defines parameters passed to tools/call.
type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ContentItem represents a content element returned by a tool.
type ContentItem struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}

// ToolCallResult represents the structured return payload of a tool.
type ToolCallResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// Server executes the Model Context Protocol over stdio.
// It speaks JSON-RPC 2.0 over stdin/stdout with zero network listeners and zero telemetry.
type Server struct {
	sanitizer *sanitize.Sanitizer
	tools     map[string]ToolHandler
	mu        sync.RWMutex
}

// ToolHandler handles an individual MCP tool execution.
type ToolHandler func(ctx context.Context, args json.RawMessage) (*ToolCallResult, error)

// NewServer creates a new MCP Server instance.
func NewServer(opts ...sanitize.Option) *Server {
	s := &Server{
		sanitizer: sanitize.New(opts...),
		tools:     make(map[string]ToolHandler),
	}
	s.registerStandardTools()
	return s
}

// Serve reads JSON-RPC 2.0 requests line-by-line from in and writes responses to out.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	// Support large payloads (up to 10MB)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var writeMu sync.Mutex
	sendResponse := func(resp *JSONRPCResponse) error {
		data, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_, err = fmt.Fprintf(out, "%s\n", string(data))
		return err
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = sendResponse(&JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      nil,
				Error: &JSONRPCError{
					Code:    CodeParseError,
					Message: fmt.Sprintf("parse error: %v", err),
				},
			})
			continue
		}

		// Handle request
		resp := s.handleRequest(ctx, &req)
		if resp != nil && req.ID != nil { // Notifications have nil ID and expect no response
			if err := sendResponse(resp); err != nil {
				return err
			}
		}
	}

	return scanner.Err()
}

func (s *Server) handleRequest(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "go-anything",
					"version": "v1alpha1",
				},
			},
		}

	case "notifications/initialized", "initialized":
		// Initialized notification
		return nil

	case "ping":
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{},
		}

	case "tools/list":
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"tools": GetToolDefinitions(),
			},
		}

	case "tools/call":
		var params ToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &JSONRPCError{
					Code:    CodeInvalidParams,
					Message: fmt.Sprintf("invalid tool call parameters: %v", err),
				},
			}
		}

		// Execution Rule 4: approve, promote, lift, and routing changes are NEVER exposed via MCP
		switch params.Name {
		case "approve", "promote", "lift", "routing", "toggle_shadow", "kill_switch":
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: &ToolCallResult{
					Content: []ContentItem{
						{
							Type: "text",
							Text: fmt.Sprintf("FORBIDDEN: '%s' is strictly a CLI-only command per Execution Rule 4. It requires human judgment and is never exposed to AI agents via MCP.", params.Name),
						},
					},
					IsError: true,
				},
			}
		}

		s.mu.RLock()
		handler, ok := s.tools[params.Name]
		s.mu.RUnlock()

		if !ok {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &JSONRPCError{
					Code:    CodeMethodNotFound,
					Message: fmt.Sprintf("unknown tool: %q", params.Name),
				},
			}
		}

		result, err := handler(ctx, params.Arguments)
		if err != nil {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: &ToolCallResult{
					Content: []ContentItem{
						{
							Type: "text",
							Text: fmt.Sprintf("Error executing %s: %v", params.Name, err),
						},
					},
					IsError: true,
				},
			}
		}

		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
		}

	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    CodeMethodNotFound,
				Message: fmt.Sprintf("method not found: %s", req.Method),
			},
		}
	}
}

// RunStdio starts the MCP server over standard input and standard output.
func (s *Server) RunStdio(ctx context.Context) error {
	return s.Serve(ctx, os.Stdin, os.Stdout)
}
