package ail

import (
	"encoding/json"
	"fmt"
)

// ─── Google GenAI Parser ─────────────────────────────────────────────────────

// GoogleGenAIParser parses Google GenAI JSON into AIL.
type GoogleGenAIParser struct{}

func (p *GoogleGenAIParser) ParseRequest(body []byte) (*Program, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("ail: parse google genai request: %w", err)
	}

	prog := NewProgram()

	// Model (in Google this is typically a URL param, but may be in body)
	if modelRaw, ok := raw["model"]; ok {
		var model string
		if json.Unmarshal(modelRaw, &model) == nil {
			prog.EmitString(SET_MODEL, model)
		}
		delete(raw, "model")
	}

	// generation_config / generationConfig
	if gcRaw, key, ok := takeRaw(raw, "generation_config", "generationConfig"); ok {
		var gcMap map[string]json.RawMessage
		if json.Unmarshal(gcRaw, &gcMap) == nil {
			var gc struct {
				Temperature     *float64 `json:"temperature,omitempty"`
				TopP            *float64 `json:"topP,omitempty"`
				MaxOutputTokens *int32   `json:"maxOutputTokens,omitempty"`
				StopSequences   []string `json:"stopSequences,omitempty"`
			}
			if json.Unmarshal(gcRaw, &gc) == nil {
				if gc.Temperature != nil {
					prog.EmitFloat(SET_TEMP, *gc.Temperature)
					delete(gcMap, "temperature")
				}
				if gc.TopP != nil {
					prog.EmitFloat(SET_TOPP, *gc.TopP)
					delete(gcMap, "topP")
					delete(gcMap, "top_p")
				}
				if gc.MaxOutputTokens != nil {
					prog.EmitInt(SET_MAX, *gc.MaxOutputTokens)
					delete(gcMap, "maxOutputTokens")
					delete(gcMap, "max_output_tokens")
				}
				for _, s := range gc.StopSequences {
					prog.EmitString(SET_STOP, s)
				}
				if len(gc.StopSequences) > 0 {
					delete(gcMap, "stopSequences")
					delete(gcMap, "stop_sequences")
				}
			}
			// thinking_config / thinkingConfig inside generation_config
			if tcRaw, tcKey, ok := takeRaw(gcMap, "thinking_config", "thinkingConfig"); ok {
				emitThinkingConfig(prog, tcRaw, "generationConfig.thinkingConfig")
				delete(gcMap, tcKey)
			}
			if fmtJSON, ok := googleFormatFromGenerationConfig(gcMap); ok {
				prog.EmitJSON(SET_FMT, fmtJSON)
				delete(gcMap, "responseMimeType")
				delete(gcMap, "response_mime_type")
				delete(gcMap, "responseSchema")
				delete(gcMap, "response_schema")
				delete(gcMap, "responseJsonSchema")
				delete(gcMap, "response_json_schema")
			}
			for cfgKey, cfgVal := range gcMap {
				prog.EmitKeyJSON(EXT_DATA, "generationConfig."+cfgKey, cfgVal)
			}
		}
		delete(raw, key)
	}

	// safety_settings / safetySettings
	if safetyRaw, key, ok := takeRaw(raw, "safety_settings", "safetySettings"); ok {
		prog.EmitJSON(SET_SAFETY, safetyRaw)
		delete(raw, key)
	}
	if tcRaw, key, ok := takeRaw(raw, "tool_config", "toolConfig"); ok {
		prog.EmitJSON(SET_TOOL, tcRaw)
		delete(raw, key)
	}

	// system_instruction / systemInstruction
	if sysRaw, key, ok := takeRaw(raw, "system_instruction", "systemInstruction"); ok {
		var sysParts struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		}
		if json.Unmarshal(sysRaw, &sysParts) == nil {
			for _, part := range sysParts.Parts {
				prog.Emit(MSG_START)
				prog.Emit(ROLE_SYS)
				prog.EmitString(TXT_CHUNK, part.Text)
				prog.Emit(MSG_END)
			}
		}
		delete(raw, key)
	}

	// Tools
	if toolsRaw, ok := raw["tools"]; ok {
		var rawToolSets []json.RawMessage
		if json.Unmarshal(toolsRaw, &rawToolSets) == nil && len(rawToolSets) > 0 {
			prog.Emit(DEF_START)
			for _, rts := range rawToolSets {
				var tsMap map[string]json.RawMessage
				if json.Unmarshal(rts, &tsMap) != nil {
					continue
				}
				fdRaw, ok := tsMap["functionDeclarations"]
				if !ok {
					prog.EmitJSON(DEF_RAW, rts)
					continue
				}
				delete(tsMap, "functionDeclarations")
				var rawDecls []json.RawMessage
				if json.Unmarshal(fdRaw, &rawDecls) != nil {
					continue
				}
				for _, rd := range rawDecls {
					var fdMap map[string]json.RawMessage
					if json.Unmarshal(rd, &fdMap) != nil {
						continue
					}
					if nameRaw, ok := fdMap["name"]; ok {
						var name string
						if json.Unmarshal(nameRaw, &name) == nil {
							prog.EmitString(DEF_NAME, name)
						}
						delete(fdMap, "name")
					}
					if descRaw, ok := fdMap["description"]; ok {
						var desc string
						if json.Unmarshal(descRaw, &desc) == nil && desc != "" {
							prog.EmitString(DEF_DESC, desc)
						}
						delete(fdMap, "description")
					}
					if paramsRaw, ok := fdMap["parameters"]; ok {
						prog.EmitJSON(DEF_SCHEMA, paramsRaw)
						delete(fdMap, "parameters")
					}

					// Remaining per-declaration fields as EXT_DATA
					for key, val := range fdMap {
						prog.EmitKeyJSON(EXT_DATA, key, val)
					}
				}
				if len(tsMap) > 0 {
					rawTool, _ := json.Marshal(tsMap)
					prog.EmitJSON(DEF_RAW, rawTool)
				}
			}
			prog.Emit(DEF_END)
		}
		delete(raw, "tools")
	}

	// Contents (messages)
	if contentsRaw, ok := raw["contents"]; ok {
		var contents []struct {
			Role  string            `json:"role"`
			Parts []json.RawMessage `json:"parts"`
		}
		if json.Unmarshal(contentsRaw, &contents) == nil {
			for _, content := range contents {
				prog.Emit(MSG_START)

				switch content.Role {
				case "user":
					prog.Emit(ROLE_USR)
				case "model":
					prog.Emit(ROLE_AST)
				case "function":
					prog.Emit(ROLE_TOOL)
				}

				for _, rawPart := range content.Parts {
					var part struct {
						Text             string `json:"text,omitempty"`
						Thought          *bool  `json:"thought,omitempty"`
						ThoughtSignature string `json:"thoughtSignature,omitempty"`
						FunctionCall     *struct {
							Name string          `json:"name"`
							Args json.RawMessage `json:"args"`
						} `json:"functionCall,omitempty"`
						FunctionResponse *struct {
							Name     string          `json:"name"`
							Response json.RawMessage `json:"response"`
						} `json:"functionResponse,omitempty"`
						InlineData *struct {
							MimeType string `json:"mimeType"`
							Data     string `json:"data"`
						} `json:"inlineData,omitempty"`
						InlineDataSnake *struct {
							MimeType string `json:"mime_type"`
							Data     string `json:"data"`
						} `json:"inline_data,omitempty"`
						FileData *struct {
							MimeType string `json:"mimeType"`
							FileURI  string `json:"fileUri"`
						} `json:"fileData,omitempty"`
						FileDataSnake *struct {
							MimeType string `json:"mime_type"`
							FileURI  string `json:"file_uri"`
						} `json:"file_data,omitempty"`
					}
					if json.Unmarshal(rawPart, &part) != nil {
						continue
					}
					handled := false
					if part.Thought != nil && *part.Thought {
						handled = true
						// Thinking part
						prog.Emit(THINK_START)
						if part.Text != "" {
							prog.EmitString(THINK_CHUNK, part.Text)
						}
						if part.ThoughtSignature != "" {
							ref := prog.AddBuffer([]byte(part.ThoughtSignature))
							prog.EmitRef(THINK_REF, ref)
						}
						prog.Emit(THINK_END)
					} else if part.Text != "" {
						handled = true
						prog.EmitString(TXT_CHUNK, part.Text)
					}
					if part.FunctionCall != nil {
						handled = true
						prog.EmitString(CALL_START, "")
						prog.EmitString(CALL_NAME, part.FunctionCall.Name)
						if len(part.FunctionCall.Args) > 0 {
							prog.EmitJSON(CALL_ARGS, part.FunctionCall.Args)
						}
						prog.Emit(CALL_END)
					}
					if part.FunctionResponse != nil {
						handled = true
						prog.EmitString(RESULT_START, part.FunctionResponse.Name)
						prog.EmitString(RESULT_DATA, string(part.FunctionResponse.Response))
						prog.Emit(RESULT_END)
					}
					inlineData := part.InlineData
					if inlineData == nil && part.InlineDataSnake != nil {
						inlineData = &struct {
							MimeType string `json:"mimeType"`
							Data     string `json:"data"`
						}{MimeType: part.InlineDataSnake.MimeType, Data: part.InlineDataSnake.Data}
					}
					if inlineData != nil {
						handled = true
						ref := prog.AddBuffer([]byte(inlineData.Data))
						if inlineData.MimeType != "" {
							prog.EmitKeyVal(SET_META, "media_type", inlineData.MimeType)
						}
						if isAudioMime(inlineData.MimeType) {
							prog.EmitRef(AUD_REF, ref)
						} else if isVideoMime(inlineData.MimeType) {
							prog.EmitRef(VID_REF, ref)
						} else if isImageMime(inlineData.MimeType) {
							prog.EmitRef(IMG_REF, ref)
						} else {
							prog.EmitRef(FILE_REF, ref)
						}
					}
					fileData := part.FileData
					if fileData == nil && part.FileDataSnake != nil {
						fileData = &struct {
							MimeType string `json:"mimeType"`
							FileURI  string `json:"fileUri"`
						}{MimeType: part.FileDataSnake.MimeType, FileURI: part.FileDataSnake.FileURI}
					}
					if fileData != nil {
						handled = true
						ref := prog.AddBuffer([]byte(fileData.FileURI))
						if fileData.MimeType != "" {
							prog.EmitKeyVal(SET_META, "media_type", fileData.MimeType)
						}
						prog.EmitKeyVal(SET_META, "source_type", "file_uri")
						if isAudioMime(fileData.MimeType) {
							prog.EmitRef(AUD_REF, ref)
						} else if isVideoMime(fileData.MimeType) {
							prog.EmitRef(VID_REF, ref)
						} else if isImageMime(fileData.MimeType) {
							prog.EmitRef(IMG_REF, ref)
						} else {
							prog.EmitRef(FILE_REF, ref)
						}
					}
					if !handled {
						prog.EmitJSON(PART_JSON, rawPart)
					}
				}

				prog.Emit(MSG_END)
			}
		}
		delete(raw, "contents")
	}

	// Remaining fields as EXT_DATA
	for key, val := range raw {
		prog.EmitKeyJSON(EXT_DATA, key, val)
	}

	return prog, nil
}

func googleFormatFromGenerationConfig(gcMap map[string]json.RawMessage) (json.RawMessage, bool) {
	result := make(map[string]json.RawMessage)
	for _, key := range []string{"responseMimeType", "response_mime_type", "responseSchema", "response_schema", "responseJsonSchema", "response_json_schema"} {
		if val, ok := gcMap[key]; ok {
			result[key] = val
		}
	}
	if len(result) == 0 {
		return nil, false
	}
	out, _ := json.Marshal(result)
	return out, true
}

func takeRaw(m map[string]json.RawMessage, keys ...string) (json.RawMessage, string, bool) {
	for _, key := range keys {
		if raw, ok := m[key]; ok {
			return raw, key, true
		}
	}
	return nil, "", false
}
