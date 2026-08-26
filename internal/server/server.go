package server

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
)

// ProtocolVersion is the revision this server implements.
const ProtocolVersion = "2025-06-18"

// MCP methods this server answers.
const (
	methodInitialize = "initialize"
	methodListTools  = "tools/list"
	methodCallTool   = "tools/call"
)

// Handler runs one tool.
//
// Returning an error means the tool failed, and that failure reaches the model
// as a tool result rather than as a protocol error. A missing record or an
// impossible argument should be returned as an error here and is not
// exceptional.
type Handler func(ctx context.Context, args json.RawMessage) (string, error)

// Tool is one callable this server publishes.
type Tool struct {
	Name        string
	Title       string
	Description string

	// InputSchema is a JSON Schema object, published verbatim. It is what the
	// model reads to build a call, so it is the tool's real documentation.
	InputSchema json.RawMessage

	Handler Handler
}

// Implementation identifies this server to a client.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Server publishes a set of tools over MCP.
type Server struct {
	info Implementation

	mu    sync.RWMutex
	tools map[string]Tool
	order []string
}

// New returns a server identified by name and version.
func New(name, version string) *Server {
	return &Server{
		info:  Implementation{Name: name, Version: version},
		tools: make(map[string]Tool),
	}
}

// Register adds a tool. A duplicate name panics: two tools shadowing each other
// silently is a mistake in the program, not a condition to handle at runtime.
func (s *Server) Register(tool Tool) {
	if tool.Name == "" || tool.Handler == nil {
		panic("server: a tool needs a name and a handler")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, taken := s.tools[tool.Name]; taken {
		panic("server: tool " + tool.Name + " is already registered")
	}
	s.tools[tool.Name] = tool
	s.order = append(s.order, tool.Name)
	sort.Strings(s.order)
}

// Handle answers one message, returning nil when there is nothing to send back.
func (s *Server) Handle(ctx context.Context, msg *Message) *Message {
	if !msg.IsRequest() {
		// A notification must never be answered, and a response arriving at a
		// server that issued no request has nowhere to go.
		return nil
	}

	switch msg.Method {
	case methodInitialize:
		return s.initialize(msg)
	case methodListTools:
		return s.listTools(msg)
	case methodCallTool:
		return s.callTool(ctx, msg)
	default:
		return errorResponse(msg.ID, CodeMethodNotFound, "unknown method: "+msg.Method)
	}
}

func (s *Server) initialize(msg *Message) *Message {
	// The client's own revision is echoed back when it sent one. A client built
	// against an earlier version speaks a tools surface that has not changed,
	// and turning it away over the number costs interoperability for nothing.
	version := ProtocolVersion
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(msg.Params, &params); err == nil && params.ProtocolVersion != "" {
		version = params.ProtocolVersion
	}

	return resultResponse(msg.ID, map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      s.info,
	})
}

type publishedTool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

func (s *Server) listTools(msg *Message) *Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]publishedTool, 0, len(s.order))
	for _, name := range s.order {
		tool := s.tools[name]
		tools = append(tools, publishedTool{
			Name:        tool.Name,
			Title:       tool.Title,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}
	return resultResponse(msg.ID, map[string]any{"tools": tools})
}

func (s *Server) callTool(ctx context.Context, msg *Message) *Message {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return errorResponse(msg.ID, CodeInvalidParams, "malformed parameters: "+err.Error())
	}

	s.mu.RLock()
	tool, known := s.tools[params.Name]
	s.mu.RUnlock()

	if !known {
		// The client asked for something this server never published, which is
		// a protocol error rather than a tool that failed.
		return errorResponse(msg.ID, CodeInvalidParams, "unknown tool: "+params.Name)
	}

	output, err := tool.Handler(ctx, params.Arguments)
	if err != nil {
		return resultResponse(msg.ID, toolResult(err.Error(), true))
	}
	return resultResponse(msg.ID, toolResult(output, false))
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}
