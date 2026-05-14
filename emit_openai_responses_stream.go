package ail

import "encoding/json"

func (e *ResponsesEmitter) EmitStreamChunk(prog *Program) ([]byte, error) {
	ec := NewExtrasCollector()
	var respID string
	var model string
	var event map[string]any

	for _, inst := range prog.Code {
		switch inst.Op {
		case RESP_ID:
			respID = inst.Str
			if resp, ok := eventResponse(event); ok {
				resp["id"] = respID
			}
		case RESP_MODEL:
			model = inst.Str
			if resp, ok := eventResponse(event); ok {
				resp["model"] = model
			}

		case STREAM_START:
			event = map[string]any{
				"type": "response.created",
				"response": map[string]any{
					"id":     respID,
					"object": "response",
					"model":  model,
					"status": "in_progress",
				},
			}

		case STREAM_DELTA:
			event = map[string]any{
				"type":  "response.output_text.delta",
				"delta": inst.Str,
			}
		case STREAM_THINK_DELTA:
			event = map[string]any{
				"type":  "response.reasoning_summary_text.delta",
				"delta": inst.Str,
			}

		case STREAM_TOOL_DELTA:
			var delta map[string]any
			if json.Unmarshal(inst.JSON, &delta) == nil {
				if args, ok := delta["arguments"]; ok {
					event = map[string]any{
						"type":         "response.function_call_arguments.delta",
						"delta":        args,
						"output_index": delta["index"],
					}
					if id, ok := delta["id"]; ok {
						event["item_id"] = id
					}
				} else {
					item := map[string]any{
						"type": "function_call",
					}
					if id, ok := delta["id"]; ok {
						item["call_id"] = id
						item["id"] = id
					}
					if name, ok := delta["name"]; ok {
						item["name"] = name
					}
					event = map[string]any{
						"type":         "response.output_item.added",
						"output_index": delta["index"],
						"item":         item,
					}
				}
			}

		case RESP_DONE:
			itemType := "message"
			if inst.Str == "tool_calls" {
				itemType = "function_call"
			}
			event = map[string]any{
				"type": "response.output_item.done",
				"item": map[string]any{
					"type":   itemType,
					"status": "completed",
				},
			}

		case USAGE:
			event = map[string]any{
				"type": "response.completed",
				"response": map[string]any{
					"id":     respID,
					"object": "response",
					"model":  model,
					"status": "completed",
					"usage":  responsesUsage(inst.JSON),
				},
			}

		case STREAM_END:
			if event == nil || event["type"] != "response.completed" {
				event = map[string]any{
					"type": "response.completed",
					"response": map[string]any{
						"id":     respID,
						"object": "response",
						"model":  model,
						"status": "completed",
					},
				}
			}

		case EXT_DATA:
			ec.AddJSON(inst.Key, inst.JSON)
		case SET_META:
			if inst.Key != "media_type" && inst.Key != "source_type" {
				ec.AddString(inst.Key, inst.Str)
			}
		}
	}

	if event == nil {
		event = map[string]any{}
	}
	ec.MergeInto(event)
	return json.Marshal(event)
}

func eventResponse(event map[string]any) (map[string]any, bool) {
	if event == nil {
		return nil, false
	}
	resp, ok := event["response"].(map[string]any)
	return resp, ok
}
