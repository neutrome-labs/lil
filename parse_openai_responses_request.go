package ail

import (
	"encoding/json"
	"fmt"
)

// ─── OpenAI Responses API Parser ─────────────────────────────────────────────

// ResponsesParser parses OpenAI Responses API JSON into AIL.
type ResponsesParser struct{}

func (p *ResponsesParser) ParseRequest(body []byte) (*Program, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("ail: parse responses request: %w", err)
	}

	prog := NewProgram()

	// Model
	if modelRaw, ok := raw["model"]; ok {
		var model string
		if json.Unmarshal(modelRaw, &model) == nil {
			prog.EmitString(SET_MODEL, model)
		}
		delete(raw, "model")
	}

	// Temperature
	if tempRaw, ok := raw["temperature"]; ok {
		var temp float64
		if json.Unmarshal(tempRaw, &temp) == nil {
			prog.EmitFloat(SET_TEMP, temp)
		}
		delete(raw, "temperature")
	}

	// top_p
	if tpRaw, ok := raw["top_p"]; ok {
		var tp float64
		if json.Unmarshal(tpRaw, &tp) == nil {
			prog.EmitFloat(SET_TOPP, tp)
		}
		delete(raw, "top_p")
	}

	// max_output_tokens
	if maxRaw, ok := raw["max_output_tokens"]; ok {
		var max int32
		if json.Unmarshal(maxRaw, &max) == nil {
			prog.EmitInt(SET_MAX, max)
		}
		delete(raw, "max_output_tokens")
	}

	// Stream
	if streamRaw, ok := raw["stream"]; ok {
		var stream bool
		if json.Unmarshal(streamRaw, &stream) == nil && stream {
			prog.Emit(SET_STREAM)
		}
		delete(raw, "stream")
	}

	// Reasoning config
	if reasoningRaw, ok := raw["reasoning"]; ok {
		emitReasoningConfig(prog, reasoningRaw)
		delete(raw, "reasoning")
	}

	// Response format: text.format in Responses API
	if textRaw, ok := raw["text"]; ok {
		var textObj map[string]json.RawMessage
		if json.Unmarshal(textRaw, &textObj) == nil {
			if fmtRaw, ok := textObj["format"]; ok {
				prog.EmitJSON(SET_FMT, fmtRaw)
				delete(textObj, "format")
			}
			// If text had other fields, keep them as EXT_DATA
			for key, val := range textObj {
				prog.EmitKeyJSON(EXT_DATA, "text."+key, val)
			}
		}
		delete(raw, "text")
	}
	// Also accept legacy response_format for backward compat
	if fmtRaw, ok := raw["response_format"]; ok {
		prog.EmitJSON(SET_FMT, fmtRaw)
		delete(raw, "response_format")
	}
	if tcRaw, ok := raw["tool_choice"]; ok {
		prog.EmitJSON(SET_TOOL, tcRaw)
		delete(raw, "tool_choice")
	}

	// Instructions → system message
	if instrRaw, ok := raw["instructions"]; ok {
		var instructions string
		if json.Unmarshal(instrRaw, &instructions) == nil && instructions != "" {
			prog.Emit(MSG_START)
			prog.Emit(ROLE_SYS)
			prog.EmitString(TXT_CHUNK, instructions)
			prog.Emit(MSG_END)
		}
		delete(raw, "instructions")
	}

	// Tools
	if toolsRaw, ok := raw["tools"]; ok {
		var rawTools []json.RawMessage
		if json.Unmarshal(toolsRaw, &rawTools) == nil && len(rawTools) > 0 {
			prog.Emit(DEF_START)
			for _, rt := range rawTools {
				var toolMap map[string]json.RawMessage
				if json.Unmarshal(rt, &toolMap) != nil {
					continue
				}

				if nameRaw, ok := toolMap["name"]; ok {
					var name string
					if json.Unmarshal(nameRaw, &name) == nil && name != "" {
						prog.EmitString(DEF_NAME, name)
					}
					delete(toolMap, "name")
				} else {
					prog.EmitJSON(DEF_RAW, rt)
					continue
				}
				if descRaw, ok := toolMap["description"]; ok {
					var desc string
					if json.Unmarshal(descRaw, &desc) == nil && desc != "" {
						prog.EmitString(DEF_DESC, desc)
					}
					delete(toolMap, "description")
				}
				if paramsRaw, ok := toolMap["parameters"]; ok {
					prog.EmitJSON(DEF_SCHEMA, paramsRaw)
					delete(toolMap, "parameters")
				}
				delete(toolMap, "type") // always "function", reconstructed by emitter

				// Remaining tool-level fields as EXT_DATA (e.g., strict)
				for key, val := range toolMap {
					prog.EmitKeyJSON(EXT_DATA, key, val)
				}
			}
			prog.Emit(DEF_END)
		}
		delete(raw, "tools")
	}

	// Input → messages
	if inputRaw, ok := raw["input"]; ok {
		// Input can be string, or array of messages
		var inputStr string
		if json.Unmarshal(inputRaw, &inputStr) == nil {
			prog.Emit(MSG_START)
			prog.Emit(ROLE_USR)
			prog.EmitString(TXT_CHUNK, inputStr)
			prog.Emit(MSG_END)
		} else {
			// Array of message objects
			var rawMsgs []json.RawMessage
			if json.Unmarshal(inputRaw, &rawMsgs) == nil {
				for _, rm := range rawMsgs {
					var msgMap map[string]json.RawMessage
					if json.Unmarshal(rm, &msgMap) != nil {
						continue
					}

					var itemType string
					if typeRaw, ok := msgMap["type"]; ok {
						json.Unmarshal(typeRaw, &itemType)
					}
					if itemType == "input_file" || itemType == "input_image" || itemType == "input_audio" {
						prog.Emit(MSG_START)
						prog.Emit(ROLE_USR)
						switch itemType {
						case "input_file":
							emitOpenAIFilePart(prog, msgMap)
						case "input_image":
							emitOpenAIImagePart(prog, msgMap)
						case "input_audio":
							emitOpenAIAudioPart(prog, msgMap)
						}
						prog.Emit(MSG_END)
						continue
					}
					if itemType == "function_call" {
						prog.Emit(MSG_START)
						prog.Emit(ROLE_AST)
						var callID, name, arguments string
						if raw, ok := msgMap["call_id"]; ok {
							json.Unmarshal(raw, &callID)
						}
						if raw, ok := msgMap["name"]; ok {
							json.Unmarshal(raw, &name)
						}
						if raw, ok := msgMap["arguments"]; ok {
							json.Unmarshal(raw, &arguments)
						}
						prog.EmitString(CALL_START, callID)
						prog.EmitString(CALL_NAME, name)
						if arguments != "" {
							prog.EmitJSON(CALL_ARGS, json.RawMessage(arguments))
						}
						prog.Emit(CALL_END)
						prog.Emit(MSG_END)
						continue
					}
					if itemType == "function_call_output" {
						prog.Emit(MSG_START)
						prog.Emit(ROLE_TOOL)
						var callID string
						if raw, ok := msgMap["call_id"]; ok {
							json.Unmarshal(raw, &callID)
						}
						prog.EmitString(RESULT_START, callID)
						if outputRaw, ok := msgMap["output"]; ok {
							var output string
							if json.Unmarshal(outputRaw, &output) == nil {
								prog.EmitString(RESULT_DATA, output)
							} else {
								prog.EmitJSON(PART_JSON, outputRaw)
							}
						}
						prog.Emit(RESULT_END)
						prog.Emit(MSG_END)
						continue
					}

					prog.Emit(MSG_START)

					if roleRaw, ok := msgMap["role"]; ok {
						var role string
						if json.Unmarshal(roleRaw, &role) == nil {
							switch role {
							case "system":
								prog.Emit(ROLE_SYS)
							case "developer":
								prog.Emit(ROLE_DEV)
							case "user":
								prog.Emit(ROLE_USR)
							case "assistant":
								prog.Emit(ROLE_AST)
							}
						}
						delete(msgMap, "role")
					}

					if contentRaw, ok := msgMap["content"]; ok {
						var contentStr string
						if json.Unmarshal(contentRaw, &contentStr) == nil {
							prog.EmitString(TXT_CHUNK, contentStr)
						} else {
							var parts []map[string]json.RawMessage
							if json.Unmarshal(contentRaw, &parts) == nil {
								for _, part := range parts {
									var partType string
									if typeRaw, ok := part["type"]; ok {
										json.Unmarshal(typeRaw, &partType)
									}
									switch partType {
									case "input_text", "text":
										if textRaw, ok := part["text"]; ok {
											var text string
											if json.Unmarshal(textRaw, &text) == nil {
												prog.EmitString(TXT_CHUNK, text)
											}
										}
									case "input_image", "image":
										emitOpenAIImagePart(prog, part)
									case "input_file", "file":
										emitOpenAIFilePart(prog, part)
									case "input_audio":
										emitOpenAIAudioPart(prog, part)
									default:
										rawPart, _ := json.Marshal(part)
										prog.EmitJSON(PART_JSON, rawPart)
									}
								}
							}
						}
						delete(msgMap, "content")
					}

					// Remaining per-message fields as EXT_DATA
					for key, val := range msgMap {
						prog.EmitKeyJSON(EXT_DATA, key, val)
					}

					prog.Emit(MSG_END)
				}
			}
		}
		delete(raw, "input")
	}

	// Remaining fields as EXT_DATA
	for key, val := range raw {
		prog.EmitKeyJSON(EXT_DATA, key, val)
	}

	return prog, nil
}

func emitOpenAIImagePart(prog *Program, part map[string]json.RawMessage) {
	if detailRaw, ok := part["detail"]; ok {
		var detail string
		if json.Unmarshal(detailRaw, &detail) == nil && detail != "" {
			prog.EmitKeyVal(SET_META, "detail", detail)
		}
	}
	if urlRaw, ok := part["image_url"]; ok {
		var url string
		if json.Unmarshal(urlRaw, &url) == nil && url != "" {
			ref := prog.AddBuffer([]byte(url))
			prog.EmitKeyVal(SET_META, "source_type", "url")
			prog.EmitRef(IMG_REF, ref)
		}
	}
	if fileIDRaw, ok := part["file_id"]; ok {
		var fileID string
		if json.Unmarshal(fileIDRaw, &fileID) == nil && fileID != "" {
			ref := prog.AddBuffer([]byte(fileID))
			prog.EmitKeyVal(SET_META, "source_type", "file_id")
			prog.EmitRef(IMG_REF, ref)
		}
	}
}

func emitOpenAIFilePart(prog *Program, part map[string]json.RawMessage) {
	if filenameRaw, ok := part["filename"]; ok {
		var filename string
		if json.Unmarshal(filenameRaw, &filename) == nil && filename != "" {
			prog.EmitKeyVal(SET_META, "filename", filename)
		}
	}
	for _, field := range []struct {
		key        string
		sourceType string
	}{
		{"file_data", "base64"},
		{"file_url", "file_url"},
		{"file_id", "file_id"},
	} {
		if raw, ok := part[field.key]; ok {
			var value string
			if json.Unmarshal(raw, &value) == nil && value != "" {
				ref := prog.AddBuffer([]byte(value))
				prog.EmitKeyVal(SET_META, "source_type", field.sourceType)
				prog.EmitRef(FILE_REF, ref)
				return
			}
		}
	}
}

func emitOpenAIAudioPart(prog *Program, part map[string]json.RawMessage) {
	if raw, ok := part["input_audio"]; ok {
		var audio struct {
			Data   string `json:"data"`
			Format string `json:"format"`
		}
		if json.Unmarshal(raw, &audio) == nil && audio.Data != "" {
			ref := prog.AddBuffer([]byte(audio.Data))
			if audio.Format != "" {
				prog.EmitKeyVal(SET_META, "media_type", "audio/"+audio.Format)
			}
			prog.EmitKeyVal(SET_META, "source_type", "base64")
			prog.EmitRef(AUD_REF, ref)
		}
	}
}
