// Package server implements the server half of the Model Context Protocol,
// directly over JSON-RPC 2.0.
//
// It is written out here rather than imported so this repository stands alone.
// The server is published for other people to run, and a public module that
// depends on a private one cannot be fetched by anybody but its author.
package server

import (
	"encoding/json"
	"fmt"
)

// Version is the only JSON-RPC version MCP allows.
const Version = "2.0"

// Error codes from the JSON-RPC 2.0 specification.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// Message is the single envelope every JSON-RPC frame uses.
//
// Requests, responses and notifications share one wire shape and are told apart
// by which fields are present, which is why decoding produces one type and the
// classification happens afterwards.
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// IsRequest reports whether the peer expects a response.
func (m *Message) IsRequest() bool { return m.Method != "" && len(m.ID) > 0 }

// IsNotification reports whether the message must not be answered.
func (m *Message) IsNotification() bool { return m.Method != "" && len(m.ID) == 0 }

// Error is a JSON-RPC error object: a failure of the protocol itself.
//
// A tool that ran and failed is not this. That travels back as a successful
// response whose result carries isError, because the failure is meant for the
// model to read and work around.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

func errorResponse(id json.RawMessage, code int, message string) *Message {
	return &Message{JSONRPC: Version, ID: id, Error: &Error{Code: code, Message: message}}
}

func resultResponse(id json.RawMessage, payload any) *Message {
	raw, err := json.Marshal(payload)
	if err != nil {
		return errorResponse(id, CodeInternalError, "encode result: "+err.Error())
	}
	return &Message{JSONRPC: Version, ID: id, Result: raw}
}
