package ail

import (
	"encoding/json"
	"strings"
)

// ReassembleStream converts a program containing streaming opcodes
// (STREAM_START, STREAM_DELTA, STREAM_TOOL_DELTA, STREAM_END, etc.)
// into a program with full message opcodes (MSG_START, TXT_CHUNK,
// CALL_START/CALL_NAME/CALL_ARGS/CALL_END, MSG_END, etc.).
//
// This is the inverse of streaming: the LLM's streamed response is
// reassembled into a single assistant response message as if it had been
// returned non-streaming. Metadata opcodes (RESP_ID, RESP_MODEL, USAGE,
// RESP_DONE) and non-stream opcodes are preserved as-is.
//
// If the program contains no streaming opcodes, it is returned unmodified.
func ReassembleStream(prog *Program) *Program {
	if prog == nil {
		return nil
	}

	// Quick check: if no streaming opcodes, return as-is.
	hasStream := false
	for _, inst := range prog.Code {
		switch inst.Op {
		case STREAM_START, STREAM_DELTA, STREAM_TOOL_DELTA, STREAM_THINK_DELTA, STREAM_END:
			hasStream = true
		}
		if hasStream {
			break
		}
	}
	if !hasStream {
		return prog
	}

	result := NewProgram()
	result.Buffers = prog.Buffers

	var textBuf strings.Builder
	var thinkBuf strings.Builder
	inMessage := false
	inThinking := false

	// Accumulate tool calls by index.
	type toolAcc struct {
		ID   string
		Name string
		Args strings.Builder
	}
	var tools []*toolAcc
	toolByIdx := make(map[int]*toolAcc)

	flushText := func() {
		if textBuf.Len() > 0 {
			result.EmitString(TXT_CHUNK, textBuf.String())
			textBuf.Reset()
		}
	}

	flushThinking := func() {
		if thinkBuf.Len() > 0 {
			if !inThinking {
				result.Emit(THINK_START)
				inThinking = true
			}
			result.EmitString(THINK_CHUNK, thinkBuf.String())
			thinkBuf.Reset()
		}
		if inThinking {
			result.Emit(THINK_END)
			inThinking = false
		}
	}

	flushTools := func() {
		for _, tc := range tools {
			result.EmitString(CALL_START, tc.ID)
			if tc.Name != "" {
				result.EmitString(CALL_NAME, tc.Name)
			}
			if args := tc.Args.String(); args != "" {
				result.EmitJSON(CALL_ARGS, json.RawMessage(args))
			}
			result.Emit(CALL_END)
		}
		tools = nil
		toolByIdx = make(map[int]*toolAcc)
	}

	for _, inst := range prog.Code {
		switch inst.Op {
		case STREAM_START:
			if !inMessage {
				result.Emit(MSG_START)
				result.Emit(ROLE_AST)
				inMessage = true
			}

		case STREAM_DELTA:
			textBuf.WriteString(inst.Str)

		case STREAM_THINK_DELTA:
			// If we were accumulating text, flush it first.
			if textBuf.Len() > 0 && !inThinking {
				// Thinking comes before text in most models, but handle edge cases.
			}
			if !inThinking {
				flushText()
				result.Emit(THINK_START)
				inThinking = true
			}
			thinkBuf.WriteString(inst.Str)

		case STREAM_TOOL_DELTA:
			// Flush thinking if pending (thinking comes before tool calls).
			if inThinking {
				if thinkBuf.Len() > 0 {
					result.EmitString(THINK_CHUNK, thinkBuf.String())
					thinkBuf.Reset()
				}
				result.Emit(THINK_END)
				inThinking = false
			}
			// Flush text before tools.
			flushText()

			// Parse the tool delta JSON.
			var td struct {
				Index     int    `json:"index"`
				ID        string `json:"id,omitempty"`
				Name      string `json:"name,omitempty"`
				Arguments string `json:"arguments,omitempty"`
			}
			if json.Unmarshal(inst.JSON, &td) != nil {
				continue
			}

			tc, ok := toolByIdx[td.Index]
			if !ok {
				tc = &toolAcc{}
				toolByIdx[td.Index] = tc
				tools = append(tools, tc)
			}
			if td.ID != "" {
				tc.ID = td.ID
			}
			if td.Name != "" {
				tc.Name = td.Name
			}
			if td.Arguments != "" {
				tc.Args.WriteString(td.Arguments)
			}

		case STREAM_END:
			flushThinking()
			flushText()
			flushTools()
			if inMessage {
				result.Emit(MSG_END)
				inMessage = false
			}

		// Metadata — preserve as-is.
		case RESP_ID, RESP_MODEL, RESP_DONE, USAGE, EXT_DATA, SET_META:
			result.Code = append(result.Code, cloneInstruction(inst))

		default:
			// Non-stream opcodes (MSG_START, TXT_CHUNK, CALL_START, etc.)
			// pass through unchanged — supports mixed programs.
			result.Code = append(result.Code, cloneInstruction(inst))
		}
	}

	// If the stream was never properly terminated (no STREAM_END), flush now.
	flushThinking()
	flushText()
	flushTools()
	if inMessage {
		result.Emit(MSG_END)
	}

	return result
}
