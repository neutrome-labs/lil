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
	var reasoningEffort string

	var currentMsg map[string]any
	var currentRole string
	var textContent string
	var contentParts []any
	var isMultimodal bool
	var lastMediaType string
	var lastSourceType string
	var lastFilename string
	var lastDetail string

	// Tool call state (within an assistant message)
	var currentCallID string
	var currentCallName string
	var currentCallArgs string
	inCall := false

	// Tool result state (within a tool message)
	var currentResultCallID string
	var currentResultData string
	var currentResultRaw json.RawMessage
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
		case SET_REASON_EFFORT:
			reasoningEffort = inst.Str

		case SET_FMT:
			result["text"] = map[string]any{
				"format": json.RawMessage(inst.JSON),
			}
		case SET_TOOL:
			result["tool_choice"] = json.RawMessage(inst.JSON)

		// Messages
		case MSG_START:
			ec.Push()
			currentMsg = make(map[string]any)
			currentRole = ""
			textContent = ""
			contentParts = nil
			isMultimodal = false
			inCall = false
			inResult = false
			currentResultRaw = nil

		case ROLE_SYS:
			currentRole = "system"
		case ROLE_DEV:
			currentRole = "developer"
		case ROLE_USR:
			currentRole = "user"
		case ROLE_AST:
			currentRole = "assistant"
		case ROLE_TOOL:
			currentRole = "tool"

		case TXT_CHUNK:
			if isMultimodal {
				contentParts = append(contentParts, map[string]any{
					"type": "input_text",
					"text": inst.Str,
				})
			} else {
				textContent += inst.Str
			}

		case IMG_REF:
			isMultimodal = true
			if textContent != "" {
				contentParts = append(contentParts, map[string]any{"type": "input_text", "text": textContent})
				textContent = ""
			}
			data := refString(prog, inst.Ref)
			contentParts = append(contentParts, openAIResponsesImagePart(data, lastSourceType, lastDetail))
			lastMediaType, lastSourceType, lastFilename, lastDetail = "", "", "", ""

		case AUD_REF:
			isMultimodal = true
			if textContent != "" {
				contentParts = append(contentParts, map[string]any{"type": "input_text", "text": textContent})
				textContent = ""
			}
			data := refString(prog, inst.Ref)
			contentParts = append(contentParts, map[string]any{
				"type": "input_audio",
				"input_audio": map[string]any{
					"data":   data,
					"format": audioFormatFromMime(lastMediaType),
				},
			})
			lastMediaType, lastSourceType, lastFilename, lastDetail = "", "", "", ""

		case FILE_REF, VID_REF:
			isMultimodal = true
			if textContent != "" {
				contentParts = append(contentParts, map[string]any{"type": "input_text", "text": textContent})
				textContent = ""
			}
			data := refString(prog, inst.Ref)
			contentParts = append(contentParts, openAIResponsesFilePart(data, lastSourceType, lastFilename))
			lastMediaType, lastSourceType, lastFilename, lastDetail = "", "", "", ""

		case PART_JSON:
			if inResult {
				currentResultRaw = inst.JSON
				break
			}
			isMultimodal = true
			if textContent != "" {
				contentParts = append(contentParts, map[string]any{"type": "input_text", "text": textContent})
				textContent = ""
			}
			contentParts = append(contentParts, json.RawMessage(inst.JSON))

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
				if currentMsg != nil && (textContent != "" || len(contentParts) > 0) {
					currentMsg["role"] = currentRole
					if len(contentParts) > 0 {
						currentMsg["content"] = contentParts
					} else {
						currentMsg["content"] = textContent
					}
					ec.MergeInto(currentMsg)
					input = append(input, currentMsg)
					currentMsg = make(map[string]any)
					textContent = ""
					contentParts = nil
					isMultimodal = false
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
			currentResultRaw = nil

		case RESULT_DATA:
			if inResult {
				currentResultData = inst.Str
			}

		case RESULT_END:
			if inResult {
				resultItem := map[string]any{
					"type":    "function_call_output",
					"call_id": currentResultCallID,
				}
				if currentResultRaw != nil {
					resultItem["output"] = json.RawMessage(currentResultRaw)
				} else {
					resultItem["output"] = currentResultData
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
					if textContent != "" || len(contentParts) > 0 {
						currentMsg["role"] = currentRole
						if len(contentParts) > 0 {
							currentMsg["content"] = contentParts
						} else {
							currentMsg["content"] = textContent
						}
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

		case DEF_RAW:
			if inToolDefs {
				if currentTool != nil {
					ec.MergeInto(currentTool)
					tools = append(tools, currentTool)
					currentTool = nil
				}
				tools = append(tools, map[string]any{"_raw": json.RawMessage(inst.JSON)})
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
				lastMediaType = inst.Str
			} else if inst.Key == "source_type" {
				lastSourceType = inst.Str
			} else if inst.Key == "filename" {
				lastFilename = inst.Str
			} else if inst.Key == "detail" {
				lastDetail = inst.Str
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
	if reasoning := mergeReasoningConfig(prefixedExtras(ec, "reasoning"), reasoningEffort); reasoning != nil {
		result["reasoning"] = reasoning
	}
	if input != nil {
		result["input"] = input
	}
	if tools != nil {
		result["tools"] = unwrapRawObjects(tools)
	}

	ec.MergeInto(result)
	return json.Marshal(result)
}

func openAIResponsesImagePart(data, sourceType, detail string) map[string]any {
	part := map[string]any{"type": "input_image"}
	if sourceType == "file_id" {
		part["file_id"] = data
	} else {
		part["image_url"] = data
	}
	if detail != "" {
		part["detail"] = detail
	}
	return part
}

func openAIResponsesFilePart(data, sourceType, filename string) map[string]any {
	part := map[string]any{"type": "input_file"}
	switch sourceType {
	case "file_id":
		part["file_id"] = data
	case "file_url", "url":
		part["file_url"] = data
	default:
		part["file_data"] = data
	}
	if filename != "" {
		part["filename"] = filename
	}
	return part
}

func refString(prog *Program, ref uint32) string {
	if int(ref) >= len(prog.Buffers) {
		return ""
	}
	return string(prog.Buffers[ref])
}

func audioFormatFromMime(mime string) string {
	const prefix = "audio/"
	if len(mime) > len(prefix) && mime[:len(prefix)] == prefix {
		return mime[len(prefix):]
	}
	return "wav"
}
