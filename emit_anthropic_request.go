package lil

import (
	"encoding/json"
)

// ─── Anthropic Messages Emitter ──────────────────────────────────────────────

// AnthropicEmitter converts an LIL Program into Anthropic Messages API JSON.
type AnthropicEmitter struct{}

func (e *AnthropicEmitter) EmitRequest(prog *Program) ([]byte, error) {
	result := make(map[string]any)
	ec := NewExtrasCollector()
	var messages []map[string]any
	var tools []map[string]any
	var systemText string

	var currentRole string
	var contentBlocks []any
	var simpleText string
	inMessage := false
	needsToolResultWrap := false
	var currentToolCallID string
	var lastMediaType string
	var lastSourceType string
	var lastTitle string
	var lastContext string
	var thinkingMode string
	var thinkingBudget int32

	// Thinking block state
	inThinking := false
	var thinkingText string
	var thinkingSignature string

	// Tool definition state
	var currentTool map[string]any
	inToolDefs := false

	// Stop sequences
	var stopSeqs []string

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
			result["max_tokens"] = inst.Int
		case SET_STOP:
			stopSeqs = append(stopSeqs, inst.Str)
		case SET_STREAM:
			result["stream"] = true

		case SET_REASON_MODE:
			thinkingMode = inst.Str
		case SET_REASON_BUDGET:
			thinkingBudget = inst.Int
		case SET_TOOL:
			result["tool_choice"] = json.RawMessage(inst.JSON)

		// Messages
		case MSG_START:
			ec.Push()
			inMessage = true
			currentRole = ""
			contentBlocks = nil
			simpleText = ""
			needsToolResultWrap = false
			currentToolCallID = ""
			lastMediaType, lastSourceType, lastTitle, lastContext = "", "", "", ""

		case ROLE_SYS:
			currentRole = "system"
		case ROLE_DEV:
			currentRole = "system"
		case ROLE_USR:
			currentRole = "user"
		case ROLE_AST:
			currentRole = "assistant"
		case ROLE_TOOL:
			// Anthropic: tool results go in a "user" message with tool_result content blocks
			currentRole = "user"
			needsToolResultWrap = true

		case TXT_CHUNK:
			if inMessage {
				simpleText += inst.Str
			}

		case THINK_START:
			inThinking = true
			thinkingText = ""
			thinkingSignature = ""

		case THINK_CHUNK:
			if inThinking {
				thinkingText += inst.Str
			}

		case THINK_REF:
			if inThinking && int(inst.Ref) < len(prog.Buffers) {
				thinkingSignature = string(prog.Buffers[inst.Ref])
			}

		case THINK_END:
			if inThinking && inMessage {
				// Flush any text before thinking
				if simpleText != "" {
					contentBlocks = append(contentBlocks, map[string]any{
						"type": "text",
						"text": simpleText,
					})
					simpleText = ""
				}
				block := map[string]any{
					"type":     "thinking",
					"thinking": thinkingText,
				}
				if thinkingSignature != "" {
					block["signature"] = thinkingSignature
				}
				contentBlocks = append(contentBlocks, block)
			}
			inThinking = false

		case IMG_REF:
			if inMessage {
				data := ""
				if int(inst.Ref) < len(prog.Buffers) {
					data = string(prog.Buffers[inst.Ref])
				}
				// Flush text first
				if simpleText != "" {
					contentBlocks = append(contentBlocks, map[string]any{
						"type": "text",
						"text": simpleText,
					})
					simpleText = ""
				}
				mediaType := lastMediaType
				if mediaType == "" {
					mediaType = "image/png"
				}
				sourceType := lastSourceType
				lastMediaType = ""
				lastSourceType = ""
				contentBlocks = append(contentBlocks, map[string]any{
					"type":   "image",
					"source": anthropicSource(data, mediaType, sourceType),
				})
			}

		case FILE_REF, VID_REF:
			if inMessage {
				data := ""
				if int(inst.Ref) < len(prog.Buffers) {
					data = string(prog.Buffers[inst.Ref])
				}
				if simpleText != "" {
					contentBlocks = append(contentBlocks, map[string]any{
						"type": "text",
						"text": simpleText,
					})
					simpleText = ""
				}
				mediaType := lastMediaType
				if mediaType == "" {
					mediaType = "application/pdf"
				}
				block := map[string]any{
					"type":   "document",
					"source": anthropicSource(data, mediaType, lastSourceType),
				}
				if lastTitle != "" {
					block["title"] = lastTitle
				}
				if lastContext != "" {
					block["context"] = lastContext
				}
				lastMediaType, lastSourceType, lastTitle, lastContext = "", "", "", ""
				contentBlocks = append(contentBlocks, block)
			}

		case PART_JSON:
			if inMessage {
				if simpleText != "" {
					contentBlocks = append(contentBlocks, map[string]any{
						"type": "text",
						"text": simpleText,
					})
					simpleText = ""
				}
				contentBlocks = append(contentBlocks, json.RawMessage(inst.JSON))
			}

		case CALL_START:
			if inMessage {
				ec.Push()
				// Flush text
				if simpleText != "" {
					contentBlocks = append(contentBlocks, map[string]any{
						"type": "text",
						"text": simpleText,
					})
					simpleText = ""
				}
				contentBlocks = append(contentBlocks, map[string]any{
					"type": "tool_use",
					"id":   inst.Str,
				})
			}

		case CALL_NAME:
			if len(contentBlocks) > 0 {
				last := contentBlocks[len(contentBlocks)-1].(map[string]any)
				if last["type"] == "tool_use" {
					last["name"] = inst.Str
				}
			}

		case CALL_ARGS:
			if len(contentBlocks) > 0 {
				last := contentBlocks[len(contentBlocks)-1].(map[string]any)
				if last["type"] == "tool_use" {
					last["input"] = json.RawMessage(inst.JSON)
				}
			}

		case CALL_END:
			if len(contentBlocks) > 0 {
				last := contentBlocks[len(contentBlocks)-1].(map[string]any)
				if last["type"] == "tool_use" {
					ec.MergeInto(last)
				}
			}
			ec.Pop()

		case RESULT_START:
			currentToolCallID = inst.Str

		case RESULT_DATA:
			if needsToolResultWrap {
				// Flush text
				if simpleText != "" {
					contentBlocks = append(contentBlocks, map[string]any{
						"type": "text",
						"text": simpleText,
					})
					simpleText = ""
				}
				contentBlocks = append(contentBlocks, map[string]any{
					"type":        "tool_result",
					"tool_use_id": currentToolCallID,
					"content":     inst.Str,
				})
			} else {
				simpleText += inst.Str
			}

		case RESULT_END:
			// tracked via needsToolResultWrap

		case MSG_END:
			if inMessage {
				if currentRole == "system" {
					// Anthropic: system is top-level, not in messages
					if systemText != "" && simpleText != "" {
						systemText += "\n\n"
					}
					systemText += simpleText
				} else {
					msg := map[string]any{"role": currentRole}
					if len(contentBlocks) > 0 {
						// Flush remaining text
						if simpleText != "" {
							contentBlocks = append(contentBlocks, map[string]any{
								"type": "text",
								"text": simpleText,
							})
						}
						msg["content"] = contentBlocks
					} else if simpleText != "" {
						msg["content"] = simpleText
					}
					ec.MergeInto(msg)
					messages = append(messages, msg)
				}
				inMessage = false
			}
			ec.Pop()

		// Tool definitions
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
				currentTool = map[string]any{"name": inst.Str}
			}

		case DEF_DESC:
			if currentTool != nil {
				currentTool["description"] = inst.Str
			}

		case DEF_SCHEMA:
			if currentTool != nil {
				currentTool["input_schema"] = json.RawMessage(inst.JSON)
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
			} else if inst.Key == "title" {
				lastTitle = inst.Str
			} else if inst.Key == "context" {
				lastContext = inst.Str
			} else if ec.Depth() > 0 {
				ec.AddString(inst.Key, inst.Str)
			} else {
				meta, _ := result["metadata"].(map[string]any)
				if meta == nil {
					meta = make(map[string]any)
				}
				meta[inst.Key] = inst.Str
				result["metadata"] = meta
			}

		case EXT_DATA:
			ec.AddJSON(inst.Key, inst.JSON)
		}
	}

	if systemText != "" {
		result["system"] = systemText
	}
	if messages != nil {
		result["messages"] = messages
	}
	if tools != nil {
		result["tools"] = unwrapRawObjects(tools)
	}
	if len(stopSeqs) > 0 {
		result["stop_sequences"] = stopSeqs
	}
	thinkingExtras := prefixedExtras(ec, "thinking")
	if thinkingMode != "" || thinkingBudget > 0 || len(thinkingExtras) > 0 {
		thinking := make(map[string]any)
		for key, val := range thinkingExtras {
			thinking[key] = val
		}
		if thinkingMode != "" {
			thinking["type"] = thinkingMode
		}
		if thinkingBudget > 0 {
			thinking["budget_tokens"] = thinkingBudget
			if thinkingMode == "" {
				thinking["type"] = "enabled"
			}
		}
		result["thinking"] = thinking
	}

	ec.MergeInto(result)
	return json.Marshal(result)
}

func anthropicSource(data, mediaType, sourceType string) map[string]any {
	switch sourceType {
	case "url", "file_url":
		return map[string]any{
			"type": "url",
			"url":  data,
		}
	case "file_id":
		return map[string]any{
			"type":    "file",
			"file_id": data,
		}
	default:
		return map[string]any{
			"type":       "base64",
			"media_type": mediaType,
			"data":       data,
		}
	}
}
