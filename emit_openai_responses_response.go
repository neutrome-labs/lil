package ail

import "encoding/json"

func (e *ResponsesEmitter) EmitResponse(prog *Program) ([]byte, error) {
	result := map[string]any{
		"object": "response",
	}
	ec := NewExtrasCollector()

	var output []map[string]any
	var currentMsg map[string]any
	var msgContent []map[string]any
	var textContent string
	inMessage := false

	inThinking := false
	var thinkingText string

	var currentCall map[string]any

	for _, inst := range prog.Code {
		switch inst.Op {
		case RESP_ID:
			result["id"] = inst.Str
		case RESP_MODEL:
			result["model"] = inst.Str
		case USAGE:
			result["usage"] = responsesUsage(inst.JSON)

		case THINK_START:
			inThinking = true
			thinkingText = ""
		case THINK_CHUNK:
			if inThinking {
				thinkingText += inst.Str
			}
		case THINK_END:
			if inThinking {
				output = append(output, map[string]any{
					"type": "reasoning",
					"summary": []map[string]any{{
						"type": "summary_text",
						"text": thinkingText,
					}},
				})
			}
			inThinking = false

		case MSG_START:
			ec.Push()
			inMessage = true
			currentMsg = map[string]any{
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
			}
			msgContent = nil
			textContent = ""

		case TXT_CHUNK:
			if inMessage {
				textContent += inst.Str
			}

		case PART_JSON:
			if inMessage {
				if textContent != "" {
					msgContent = append(msgContent, map[string]any{
						"type": "output_text",
						"text": textContent,
					})
					textContent = ""
				}
				msgContent = append(msgContent, rawMap(inst.JSON))
			} else {
				output = append(output, rawMap(inst.JSON))
			}

		case CALL_START:
			ec.Push()
			currentCall = map[string]any{
				"type":    "function_call",
				"call_id": inst.Str,
				"status":  "completed",
			}
		case CALL_NAME:
			if currentCall != nil {
				currentCall["name"] = inst.Str
			}
		case CALL_ARGS:
			if currentCall != nil {
				currentCall["arguments"] = string(inst.JSON)
			}
		case CALL_END:
			if currentCall != nil {
				ec.MergeInto(currentCall)
				output = append(output, currentCall)
				currentCall = nil
			}
			ec.Pop()

		case RESP_DONE:
			result["status"] = "completed"

		case MSG_END:
			if inMessage {
				if textContent != "" {
					msgContent = append(msgContent, map[string]any{
						"type": "output_text",
						"text": textContent,
					})
				}
				if len(msgContent) > 0 {
					currentMsg["content"] = msgContent
					ec.MergeInto(currentMsg)
					output = append(output, currentMsg)
				}
				inMessage = false
			}
			ec.Pop()

		case EXT_DATA:
			ec.AddJSON(inst.Key, inst.JSON)
		case SET_META:
			if inst.Key != "media_type" && inst.Key != "source_type" {
				ec.AddString(inst.Key, inst.Str)
			}
		}
	}

	if output != nil {
		result["output"] = output
	}
	ec.MergeInto(result)
	return json.Marshal(result)
}

func responsesUsage(j json.RawMessage) any {
	var std struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	}
	if json.Unmarshal(j, &std) == nil && (std.PromptTokens != 0 || std.CompletionTokens != 0 || std.TotalTokens != 0) {
		return map[string]any{
			"input_tokens":  std.PromptTokens,
			"output_tokens": std.CompletionTokens,
			"total_tokens":  std.TotalTokens,
		}
	}
	return json.RawMessage(j)
}
