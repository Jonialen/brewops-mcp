package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// ServeStdio runs the server over MCP's stdio transport: newline-delimited
// JSON-RPC frames in on one stream, out on the other.
//
// Nothing but frames may be written to out. A server that prints a banner or a
// log line there corrupts the stream for its client, which is why every
// diagnostic in this program goes to stderr.
func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		line, readErr := reader.ReadBytes('\n')

		if frame := bytes.TrimSpace(line); len(frame) > 0 {
			if response := s.handleFrame(ctx, frame); response != nil {
				encoded, err := json.Marshal(response)
				if err != nil {
					return fmt.Errorf("encode frame: %w", err)
				}
				if _, err := out.Write(append(encoded, '\n')); err != nil {
					return fmt.Errorf("write frame: %w", err)
				}
			}
		}

		if readErr != nil {
			if readErr == io.EOF {
				// The client closed its end: an ordinary shutdown.
				return nil
			}
			return fmt.Errorf("read frame: %w", readErr)
		}
	}
}

func (s *Server) handleFrame(ctx context.Context, frame []byte) *Message {
	var msg Message
	if err := json.Unmarshal(frame, &msg); err != nil {
		// A frame that will not parse carries no id to answer against, so the
		// error the specification defines for it uses a null one.
		return errorResponse(json.RawMessage("null"), CodeParseError,
			"malformed JSON: "+err.Error())
	}
	return s.Handle(ctx, &msg)
}
