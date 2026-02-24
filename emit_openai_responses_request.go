package ail

import (
	"encoding/json"
)

// ─── OpenAI Responses API Emitter ────────────────────────────────────────────

// ResponsesEmitter converts an AIL Program into OpenAI Responses API JSON.
type ResponsesEmitter struct{}

func (e *ResponsesEmitter) EmitRequest(prog *Program) ([]byte, error) {
	result := make(map[string]any)
	ec := NewExtrasCollector()
	var input []map[string]any
	var tools []map[string]any
	var systemText string

	var currentMsg map[string]any
	var currentRole string
	var textContent string

	// Tool call state (within an assistant message)
	var currentCallID string
	var currentCallName string
	var currentCallArgs string
	inCall := false

	// Tool result state (within a tool message)
	var currentResultCallID string
	var currentResultData string
	inResult := false

	// Tool definition state
	var currentTool map[string]any
	inToolDefs := false

	for _, inst := range prog.Code {
		switch inst.Op {
		// Config
		case SET_MODEL:
			result["model"] = inst.Str
		case SET_TEMP:
			result["temperature"] = inst.Num
		case SET_TOPP:
			result["top_p"] = inst.Num
		case SET_MAX:
			result["max_output_tokens"] = inst.Int
		case SET_STREAM:
			result["stream"] = true
		case SET_THINK:
			result["reasoning"] = json.RawMessage(inst.JSON)

		case SET_FMT:
			result["text"] = map[string]any{
				"format": json.RawMessage(inst.JSON),
			}

		// Messages
		case MSG_START:
			ec.Push()
			currentMsg = make(map[string]any)
			currentRole = ""
			textContent = ""
			inCall = false
			inResult = false

		case ROLE_SYS:
			currentRole = "system"
		case ROLE_USR:
			currentRole = "user"
		case ROLE_AST:
			currentRole = "assistant"
		case ROLE_TOOL:
			currentRole = "tool"

		case TXT_CHUNK:
			textContent += inst.Str

		// Tool calls (assistant → function_call items)
		case CALL_START:
			inCall = true
			currentCallID = inst.Str
			currentCallName = ""
			currentCallArgs = ""

		case CALL_NAME:
			if inCall {
				currentCallName = inst.Str
			}

		case CALL_ARGS:
			if inCall {
				currentCallArgs = string(inst.JSON)
			}

		case CALL_END:
			if inCall {
				// Responses API: each function call is a separate input item.
				// Flush any pending text message first.
				if currentMsg != nil && textContent != "" {
					currentMsg["role"] = currentRole
					currentMsg["content"] = textContent
					ec.MergeInto(currentMsg)
					input = append(input, currentMsg)
					currentMsg = make(map[string]any)
					textContent = ""
				}

				callItem := map[string]any{
					"type":      "function_call",
					"call_id":   currentCallID,
					"name":      currentCallName,
					"arguments": currentCallArgs,
				}
				input = append(input, callItem)
				inCall = false
			}

		// Tool results (tool → function_call_output items)
		case RESULT_START:
			inResult = true
			currentResultCallID = inst.Str
			currentResultData = ""

		case RESULT_DATA:
			if inResult {
				currentResultData = inst.Str
			}

		case RESULT_END:
			if inResult {
				resultItem := map[string]any{
					"type":    "function_call_output",
					"call_id": currentResultCallID,
					"output":  currentResultData,
				}
				input = append(input, resultItem)
				inResult = false
			}

		case MSG_END:
			if currentMsg != nil {
				if currentRole == "system" {
					// Responses API: system goes to "instructions"
					if systemText != "" && textContent != "" {
						systemText += "\n\n"
					}
					systemText += textContent
				} else if currentRole == "tool" {
					// Tool results already emitted via RESULT_END above.
					// If there was no RESULT block but textContent exists, emit as output.
					if !inResult && textContent != "" && currentResultCallID == "" {
						// Fallback: bare tool message with text content.
						toolMsg := map[string]any{
							"role":    currentRole,
							"content": textContent,
						}
						ec.MergeInto(toolMsg)
						input = append(input, toolMsg)
					}
				} else {
					// user or assistant text message
					if textContent != "" {
						currentMsg["role"] = currentRole
						currentMsg["content"] = textContent
						ec.MergeInto(currentMsg)
						input = append(input, currentMsg)
					}
					// If assistant message had only tool calls (no text),
					// the function_call items were already emitted via CALL_END.
				}
				currentMsg = nil
			}
			ec.Pop()

		// Tool definitions (Responses API: flat structure)
		case DEF_START:
			ec.Push()
			inToolDefs = true
			currentTool = nil

		case DEF_NAME:
			if inToolDefs {
				if currentTool != nil {
					ec.MergeInto(currentTool)
					tools = append(tools, currentTool)
				}
				currentTool = map[string]any{
					"type": "function",
					"name": inst.Str,
				}
			}

		case DEF_DESC:
			if currentTool != nil {
				currentTool["description"] = inst.Str
			}

		case DEF_SCHEMA:
			if currentTool != nil {
				currentTool["parameters"] = json.RawMessage(inst.JSON)
			}

		case DEF_END:
			if inToolDefs && currentTool != nil {
				ec.MergeInto(currentTool)
				tools = append(tools, currentTool)
				currentTool = nil
			}
			ec.Pop()
			inToolDefs = false

		// Extensions
		case SET_META:
			if inst.Key == "media_type" {
				// consumed by IMG_REF / AUD_REF
			} else if ec.Depth() > 0 {
				ec.AddString(inst.Key, inst.Str)
			} else {
				result[inst.Key] = inst.Str
			}

		case EXT_DATA:
			ec.AddJSON(inst.Key, inst.JSON)
		}
	}

	if systemText != "" {
		result["instructions"] = systemText
	}
	if input != nil {
		result["input"] = input
	}
	if tools != nil {
		result["tools"] = tools
	}

	ec.MergeInto(result)
	return json.Marshal(result)
}
