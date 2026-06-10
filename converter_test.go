package lil

import (
	"encoding/json"
	"testing"
)

func TestChatCompletionsRequestRoundTrip(t *testing.T) {
	input := `{
		"model": "gpt-4o",
		"temperature": 0.7,
		"max_tokens": 1024,
		"stream": true,
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Hello!"},
			{"role": "assistant", "content": "Hi there!"},
			{"role": "user", "content": "What is 2+2?"}
		],
		"tools": [
			{
				"type": "function",
				"function": {
					"name": "calculator",
					"description": "Do math",
					"parameters": {"type": "object", "properties": {"expr": {"type": "string"}}}
				}
			}
		]
	}`

	parser := &ChatCompletionsParser{}
	prog, err := parser.ParseRequest([]byte(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Verify program structure
	if m := prog.GetModel(); m != "gpt-4o" {
		t.Errorf("model: got %q, want gpt-4o", m)
	}
	if !prog.IsStreaming() {
		t.Error("expected streaming")
	}

	// Verify disassembly includes key instructions
	asm := prog.Disasm()
	t.Logf("Disassembly:\n%s", asm)

	// Emit back to Chat Completions
	emitter := &ChatCompletionsEmitter{}
	out, err := emitter.EmitRequest(prog)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	// Parse the output to verify structure
	var result map[string]json.RawMessage
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	// Model
	var model string
	json.Unmarshal(result["model"], &model)
	if model != "gpt-4o" {
		t.Errorf("output model: got %q, want gpt-4o", model)
	}

	// Messages
	var messages []map[string]any
	json.Unmarshal(result["messages"], &messages)
	if len(messages) != 4 {
		t.Errorf("output messages count: got %d, want 4", len(messages))
	}
	if messages[0]["role"] != "system" {
		t.Errorf("first message role: got %v, want system", messages[0]["role"])
	}

	// Tools
	var tools []map[string]any
	json.Unmarshal(result["tools"], &tools)
	if len(tools) != 1 {
		t.Errorf("output tools count: got %d, want 1", len(tools))
	}

	// Stream
	var stream bool
	json.Unmarshal(result["stream"], &stream)
	if !stream {
		t.Error("output stream: expected true")
	}
}

func TestChatCompletionsToolCallRoundTrip(t *testing.T) {
	input := `{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "What is the weather in NYC?"},
			{
				"role": "assistant",
				"content": null,
				"tool_calls": [{
					"id": "call_abc123",
					"type": "function",
					"function": {
						"name": "get_weather",
						"arguments": "{\"location\":\"NYC\"}"
					}
				}]
			},
			{
				"role": "tool",
				"tool_call_id": "call_abc123",
				"content": "72°F, sunny"
			}
		]
	}`

	parser := &ChatCompletionsParser{}
	prog, err := parser.ParseRequest([]byte(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	t.Logf("Disassembly:\n%s", prog.Disasm())

	emitter := &ChatCompletionsEmitter{}
	out, err := emitter.EmitRequest(prog)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(out, &result)

	var messages []map[string]any
	json.Unmarshal(result["messages"], &messages)
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}

	// Tool message should have tool_call_id
	toolMsg := messages[2]
	if toolMsg["role"] != "tool" {
		t.Errorf("tool message role: got %v", toolMsg["role"])
	}
	if toolMsg["tool_call_id"] != "call_abc123" {
		t.Errorf("tool_call_id: got %v", toolMsg["tool_call_id"])
	}
}

func TestChatCompletionsToAnthropicConversion(t *testing.T) {
	input := `{
		"model": "claude-3-opus",
		"temperature": 0.5,
		"max_tokens": 2048,
		"messages": [
			{"role": "system", "content": "You are a scientist."},
			{"role": "user", "content": "Explain quantum physics."}
		],
		"tools": [
			{
				"type": "function",
				"function": {
					"name": "search",
					"description": "Search the web",
					"parameters": {"type": "object", "properties": {"query": {"type": "string"}}}
				}
			}
		]
	}`

	// Parse as Chat Completions
	parser := &ChatCompletionsParser{}
	prog, err := parser.ParseRequest([]byte(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	t.Logf("LIL Program:\n%s", prog.Disasm())

	// Emit as Anthropic
	emitter := &AnthropicEmitter{}
	out, err := emitter.EmitRequest(prog)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(out, &result)

	// System should be top-level in Anthropic
	var system string
	json.Unmarshal(result["system"], &system)
	if system != "You are a scientist." {
		t.Errorf("anthropic system: got %q", system)
	}

	// Messages should NOT contain system message
	var messages []map[string]any
	json.Unmarshal(result["messages"], &messages)
	if len(messages) != 1 {
		t.Errorf("anthropic messages: got %d, want 1 (user only)", len(messages))
	}
	if messages[0]["role"] != "user" {
		t.Errorf("first message role: got %v, want user", messages[0]["role"])
	}

	// Tools should use input_schema (not parameters)
	var tools []map[string]any
	json.Unmarshal(result["tools"], &tools)
	if len(tools) != 1 {
		t.Fatalf("anthropic tools count: got %d, want 1", len(tools))
	}
	if tools[0]["name"] != "search" {
		t.Errorf("tool name: got %v", tools[0]["name"])
	}
	if _, ok := tools[0]["input_schema"]; !ok {
		t.Error("tool should have input_schema, not parameters")
	}

	// max_tokens should be present (required in Anthropic)
	var maxTokens float64
	json.Unmarshal(result["max_tokens"], &maxTokens)
	if int(maxTokens) != 2048 {
		t.Errorf("max_tokens: got %v, want 2048", maxTokens)
	}
}

func TestChatCompletionsToGoogleConversion(t *testing.T) {
	input := `{
		"model": "gemini-pro",
		"temperature": 0.3,
		"max_tokens": 512,
		"messages": [
			{"role": "system", "content": "Be concise."},
			{"role": "user", "content": "Hello!"}
		]
	}`

	parser := &ChatCompletionsParser{}
	prog, err := parser.ParseRequest([]byte(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	emitter := &GoogleGenAIEmitter{}
	out, err := emitter.EmitRequest(prog)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	t.Logf("Google output: %s", string(out))

	var result map[string]json.RawMessage
	json.Unmarshal(out, &result)

	// system_instruction should exist
	if _, ok := result["system_instruction"]; !ok {
		t.Error("expected system_instruction in Google output")
	}

	// contents should have user message with role "user"
	var contents []map[string]any
	json.Unmarshal(result["contents"], &contents)
	if len(contents) != 1 {
		t.Fatalf("contents: got %d, want 1", len(contents))
	}
	if contents[0]["role"] != "user" {
		t.Errorf("role: got %v, want user", contents[0]["role"])
	}

	// generation_config
	var genConfig map[string]any
	json.Unmarshal(result["generation_config"], &genConfig)
	if genConfig["temperature"] != 0.3 {
		t.Errorf("temperature: got %v, want 0.3", genConfig["temperature"])
	}
}

func TestChatCompletionsToResponsesConversion(t *testing.T) {
	input := `{
		"model": "gpt-4o",
		"messages": [
			{"role": "system", "content": "Be helpful"},
			{"role": "user", "content": "Hello"}
		],
		"max_tokens": 100,
		"stream": true
	}`

	parser := &ChatCompletionsParser{}
	prog, err := parser.ParseRequest([]byte(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	emitter := &ResponsesEmitter{}
	out, err := emitter.EmitRequest(prog)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	t.Logf("Responses output: %s", string(out))

	var result map[string]json.RawMessage
	json.Unmarshal(out, &result)

	// instructions (from system message)
	var instructions string
	json.Unmarshal(result["instructions"], &instructions)
	if instructions != "Be helpful" {
		t.Errorf("instructions: got %q, want 'Be helpful'", instructions)
	}

	// input (from user message)
	var input2 []map[string]any
	json.Unmarshal(result["input"], &input2)
	if len(input2) != 1 {
		t.Fatalf("input: got %d, want 1", len(input2))
	}

	// max_output_tokens
	var maxOut float64
	json.Unmarshal(result["max_output_tokens"], &maxOut)
	if int(maxOut) != 100 {
		t.Errorf("max_output_tokens: got %v, want 100", maxOut)
	}
}

func TestChatCompletionsResponseParse(t *testing.T) {
	resp := `{
		"id": "chatcmpl-abc123",
		"object": "chat.completion",
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello! How can I help?"
			},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 8, "total_tokens": 18}
	}`

	parser := &ChatCompletionsParser{}
	prog, err := parser.ParseResponse([]byte(resp))
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}

	t.Logf("Response LIL:\n%s", prog.Disasm())

	// Emit back
	emitter := &ChatCompletionsEmitter{}
	out, err := emitter.EmitResponse(prog)
	if err != nil {
		t.Fatalf("emit response: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(out, &result)

	var id string
	json.Unmarshal(result["id"], &id)
	if id != "chatcmpl-abc123" {
		t.Errorf("id: got %q", id)
	}

	var choices []map[string]any
	json.Unmarshal(result["choices"], &choices)
	if len(choices) != 1 {
		t.Fatalf("choices: got %d", len(choices))
	}
}

func TestChatCompletionsResponseParsesReasoningField(t *testing.T) {
	resp := `{
		"id": "gen-1778838029",
		"object": "chat.completion",
		"model": "deepseek/deepseek-v4-flash-20260423:free",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "The number of r's is 2.",
				"reasoning": "Let's break it down."
			},
			"finish_reason": "stop"
		}]
	}`

	parser := &ChatCompletionsParser{}
	prog, err := parser.ParseResponse([]byte(resp))
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}

	var foundThinking bool
	for _, th := range prog.Thinkings() {
		if prog.ThinkingText(th) == "Let's break it down." {
			foundThinking = true
		}
	}
	if !foundThinking {
		t.Fatalf("missing reasoning in LIL:\n%s", prog.Disasm())
	}

	out, err := ConvertResponse([]byte(resp), StyleChatCompletions, StyleResponses)
	if err != nil {
		t.Fatalf("convert response: %v", err)
	}
	var converted struct {
		Output []struct {
			Type    string `json:"type"`
			Summary []struct {
				Text string `json:"text"`
			} `json:"summary"`
		} `json:"output"`
	}
	if err := json.Unmarshal(out, &converted); err != nil {
		t.Fatalf("unmarshal converted: %v", err)
	}
	if len(converted.Output) == 0 || converted.Output[0].Type != "reasoning" || len(converted.Output[0].Summary) == 0 || converted.Output[0].Summary[0].Text != "Let's break it down." {
		t.Fatalf("converted response lost reasoning: %s", out)
	}
}

func TestStreamChunkRoundTrip(t *testing.T) {
	// First chunk: role
	chunk1 := `{
		"id": "chatcmpl-abc",
		"model": "gpt-4o",
		"choices": [{"index": 0, "delta": {"role": "assistant"}, "finish_reason": null}]
	}`

	// Content chunk
	chunk2 := `{
		"id": "chatcmpl-abc",
		"model": "gpt-4o",
		"choices": [{"index": 0, "delta": {"content": "Hello"}, "finish_reason": null}]
	}`

	// Final chunk
	chunk3 := `{
		"id": "chatcmpl-abc",
		"model": "gpt-4o",
		"choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}]
	}`

	parser := &ChatCompletionsParser{}
	emitter := &ChatCompletionsEmitter{}

	for i, chunk := range []string{chunk1, chunk2, chunk3} {
		prog, err := parser.ParseStreamChunk([]byte(chunk))
		if err != nil {
			t.Fatalf("chunk %d parse: %v", i, err)
		}
		t.Logf("Chunk %d LIL:\n%s", i, prog.Disasm())

		out, err := emitter.EmitStreamChunk(prog)
		if err != nil {
			t.Fatalf("chunk %d emit: %v", i, err)
		}
		t.Logf("Chunk %d output: %s", i, string(out))
	}
}

func TestConvertRequest(t *testing.T) {
	input := `{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "Hi"}
		]
	}`

	// Same style passthrough
	out, err := ConvertRequest([]byte(input), "openai-chat-completions", "openai-chat-completions")
	if err != nil {
		t.Fatalf("passthrough: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(out, &result)
	var model string
	json.Unmarshal(result["model"], &model)
	if model != "gpt-4" {
		t.Errorf("passthrough model: got %q", model)
	}

	// Cross-style conversion
	out, err = ConvertRequest([]byte(input), "openai-chat-completions", "anthropic-messages")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	json.Unmarshal(out, &result)
	var msgs []map[string]any
	json.Unmarshal(result["messages"], &msgs)
	if len(msgs) != 1 {
		t.Errorf("anthropic messages: got %d, want 1", len(msgs))
	}
}

func TestExtDataPassthrough(t *testing.T) {
	input := `{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "Hi"}],
		"response_format": {"type": "json_object"},
		"seed": 42,
		"logprobs": true
	}`

	parser := &ChatCompletionsParser{}
	prog, err := parser.ParseRequest([]byte(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// response_format should be SET_FMT, not EXT_DATA
	fmtCount := 0
	extCount := 0
	for _, inst := range prog.Code {
		if inst.Op == SET_FMT {
			fmtCount++
		}
		if inst.Op == EXT_DATA {
			extCount++
		}
	}
	if fmtCount != 1 {
		t.Errorf("expected 1 SET_FMT, got %d\n%s", fmtCount, prog.Disasm())
	}
	if extCount < 2 {
		t.Errorf("expected at least 2 EXT_DATA (seed, logprobs), got %d\n%s", extCount, prog.Disasm())
	}

	// Emit back - should survive round-trip
	emitter := &ChatCompletionsEmitter{}
	out, err := emitter.EmitRequest(prog)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(out, &result)
	if _, ok := result["response_format"]; !ok {
		t.Error("response_format should survive round-trip via SET_FMT")
	}
	if _, ok := result["seed"]; !ok {
		t.Error("seed should survive round-trip via EXT_DATA")
	}
}

func TestResponseFormatChatToResponses(t *testing.T) {
	input := `{
		"model": "gpt-4o",
		"messages": [{"role": "user", "content": "List 3 colors in JSON."}],
		"response_format": {"type": "json_object"}
	}`

	out, err := ConvertRequest([]byte(input), StyleChatCompletions, StyleResponses)
	if err != nil {
		t.Fatalf("convert chat→responses: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(out, &result)

	// Must NOT have top-level response_format (Responses API rejects it)
	if _, ok := result["response_format"]; ok {
		t.Error("response_format must not appear at top level in Responses API output")
	}

	// Must have text.format
	textRaw, ok := result["text"]
	if !ok {
		t.Fatal("expected 'text' key in Responses API output")
	}
	var textObj map[string]json.RawMessage
	if err := json.Unmarshal(textRaw, &textObj); err != nil {
		t.Fatalf("unmarshal text: %v", err)
	}
	fmtRaw, ok := textObj["format"]
	if !ok {
		t.Fatal("expected 'text.format' key in Responses API output")
	}
	var fmtObj map[string]string
	json.Unmarshal(fmtRaw, &fmtObj)
	if fmtObj["type"] != "json_object" {
		t.Errorf("text.format.type = %q, want json_object", fmtObj["type"])
	}
}

func TestReasoningAndThinkingTypedOpcodes(t *testing.T) {
	chatInput := `{
		"model": "gpt-5",
		"reasoning_effort": "xhigh",
		"messages": [{"role": "user", "content": "Think hard."}]
	}`
	prog, err := (&ChatCompletionsParser{}).ParseRequest([]byte(chatInput))
	if err != nil {
		t.Fatalf("parse chat: %v", err)
	}
	foundEffort := false
	for _, inst := range prog.Code {
		if inst.Op == SET_REASON_EFFORT && inst.Str == "xhigh" {
			foundEffort = true
		}
	}
	if !foundEffort {
		t.Fatalf("missing SET_REASON_EFFORT:\n%s", prog.Disasm())
	}
	out, err := (&ChatCompletionsEmitter{}).EmitRequest(prog)
	if err != nil {
		t.Fatalf("emit chat: %v", err)
	}
	var chatOut map[string]any
	json.Unmarshal(out, &chatOut)
	if chatOut["reasoning_effort"] != "xhigh" {
		t.Fatalf("reasoning_effort = %v, want xhigh", chatOut["reasoning_effort"])
	}

	responsesInput := `{
		"model": "gpt-5",
		"reasoning": {"effort": "high", "summary": "auto"},
		"input": "Hi"
	}`
	prog, err = (&ResponsesParser{}).ParseRequest([]byte(responsesInput))
	if err != nil {
		t.Fatalf("parse responses: %v", err)
	}
	foundEffort = false
	foundSummary := false
	for _, inst := range prog.Code {
		if inst.Op == SET_REASON_EFFORT && inst.Str == "high" {
			foundEffort = true
		}
		if inst.Op == EXT_DATA && inst.Key == "reasoning.summary" {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Fatalf("missing reasoning.summary EXT_DATA:\n%s", prog.Disasm())
	}
	if !foundEffort {
		t.Fatalf("missing Responses SET_REASON_EFFORT:\n%s", prog.Disasm())
	}
	out, err = (&ResponsesEmitter{}).EmitRequest(prog)
	if err != nil {
		t.Fatalf("emit responses: %v", err)
	}
	var responsesOut map[string]any
	json.Unmarshal(out, &responsesOut)
	reasoning := responsesOut["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" || reasoning["summary"] != "auto" {
		t.Fatalf("bad reasoning output: %s", out)
	}

	anthropicInput := `{
		"model": "claude-3",
		"max_tokens": 1000,
		"thinking": {"type": "enabled", "budget_tokens": 2048},
		"messages": [{"role": "user", "content": "Hi"}]
	}`
	prog, err = (&AnthropicParser{}).ParseRequest([]byte(anthropicInput))
	if err != nil {
		t.Fatalf("parse anthropic: %v", err)
	}
	foundMode, foundBudget := false, false
	for _, inst := range prog.Code {
		if inst.Op == SET_REASON_MODE && inst.Str == "enabled" {
			foundMode = true
		}
		if inst.Op == SET_REASON_BUDGET && inst.Int == 2048 {
			foundBudget = true
		}
	}
	if !foundMode || !foundBudget {
		t.Fatalf("missing typed thinking config mode=%v budget=%v:\n%s", foundMode, foundBudget, prog.Disasm())
	}
	out, err = (&AnthropicEmitter{}).EmitRequest(prog)
	if err != nil {
		t.Fatalf("emit anthropic: %v", err)
	}
	var anthropicOut map[string]any
	json.Unmarshal(out, &anthropicOut)
	thinking := anthropicOut["thinking"].(map[string]any)
	if thinking["type"] != "enabled" || thinking["budget_tokens"].(float64) != 2048 {
		t.Fatalf("bad thinking output: %s", out)
	}

	googleInput := `{
		"model": "gemini-2.5-flash",
		"generation_config": {"thinking_config": {"thinking_budget": 8192}},
		"contents": [{"role": "user", "parts": [{"text": "Hi"}]}]
	}`
	prog, err = (&GoogleGenAIParser{}).ParseRequest([]byte(googleInput))
	if err != nil {
		t.Fatalf("parse google: %v", err)
	}
	foundBudget = false
	for _, inst := range prog.Code {
		if inst.Op == SET_REASON_BUDGET && inst.Int == 8192 {
			foundBudget = true
		}
	}
	if !foundBudget {
		t.Fatalf("missing Google SET_REASON_BUDGET:\n%s", prog.Disasm())
	}
	out, err = (&GoogleGenAIEmitter{}).EmitRequest(prog)
	if err != nil {
		t.Fatalf("emit google: %v", err)
	}
	var googleOut map[string]any
	json.Unmarshal(out, &googleOut)
	genConfig := googleOut["generation_config"].(map[string]any)
	googleThinking := genConfig["thinking_config"].(map[string]any)
	if googleThinking["thinking_budget"].(float64) != 8192 {
		t.Fatalf("bad google thinking output: %s", out)
	}
}

func TestResponseFormatResponsesToChat(t *testing.T) {
	input := `{
		"model": "gpt-4o",
		"text": {"format": {"type": "json_object"}},
		"input": [{"role": "user", "content": "List 3 colors in JSON."}]
	}`

	out, err := ConvertRequest([]byte(input), StyleResponses, StyleChatCompletions)
	if err != nil {
		t.Fatalf("convert responses→chat: %v", err)
	}

	var result map[string]json.RawMessage
	json.Unmarshal(out, &result)

	// Must NOT have text key
	if _, ok := result["text"]; ok {
		t.Error("'text' key must not appear in Chat Completions output")
	}

	// Must have response_format
	fmtRaw, ok := result["response_format"]
	if !ok {
		t.Fatal("expected 'response_format' key in Chat Completions output")
	}
	var fmtObj map[string]string
	json.Unmarshal(fmtRaw, &fmtObj)
	if fmtObj["type"] != "json_object" {
		t.Errorf("response_format.type = %q, want json_object", fmtObj["type"])
	}
}

func TestAnthropicResponseParse(t *testing.T) {
	resp := `{
		"id": "msg_01abc",
		"type": "message",
		"role": "assistant",
		"model": "claude-3-opus-20240229",
		"content": [
			{"type": "text", "text": "Hello! How can I help?"}
		],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 8}
	}`

	parser := &AnthropicParser{}
	prog, err := parser.ParseResponse([]byte(resp))
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}

	t.Logf("Anthropic Response LIL:\n%s", prog.Disasm())

	// Verify structure
	foundID := false
	foundText := false
	foundDone := false
	for _, inst := range prog.Code {
		if inst.Op == RESP_ID && inst.Str == "msg_01abc" {
			foundID = true
		}
		if inst.Op == TXT_CHUNK && inst.Str == "Hello! How can I help?" {
			foundText = true
		}
		if inst.Op == RESP_DONE && inst.Str == "stop" {
			foundDone = true
		}
	}
	if !foundID {
		t.Error("missing RESP_ID")
	}
	if !foundText {
		t.Error("missing text content")
	}
	if !foundDone {
		t.Error("missing RESP_DONE")
	}

	// Round-trip through Anthropic emitter
	emitter := &AnthropicEmitter{}
	out, err := emitter.EmitResponse(prog)
	if err != nil {
		t.Fatalf("emit response: %v", err)
	}

	var result map[string]any
	json.Unmarshal(out, &result)
	if result["id"] != "msg_01abc" {
		t.Errorf("id: got %v", result["id"])
	}
	if result["stop_reason"] != "end_turn" {
		t.Errorf("stop_reason: got %v", result["stop_reason"])
	}
}

func TestAnthropicResponseToolUse(t *testing.T) {
	resp := `{
		"id": "msg_02xyz",
		"type": "message",
		"role": "assistant",
		"model": "claude-3-sonnet",
		"content": [
			{"type": "text", "text": "I'll check the weather."},
			{"type": "tool_use", "id": "toolu_123", "name": "get_weather", "input": {"location": "NYC"}}
		],
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 20, "output_tokens": 15}
	}`

	parser := &AnthropicParser{}
	prog, err := parser.ParseResponse([]byte(resp))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	t.Logf("Anthropic Tool Use Response LIL:\n%s", prog.Disasm())

	foundCall := false
	for _, inst := range prog.Code {
		if inst.Op == CALL_NAME && inst.Str == "get_weather" {
			foundCall = true
		}
	}
	if !foundCall {
		t.Error("missing tool call")
	}
}

func TestGoogleGenAIResponseParse(t *testing.T) {
	resp := `{
		"candidates": [{
			"content": {
				"parts": [{"text": "Hello from Gemini!"}],
				"role": "model"
			},
			"finishReason": "STOP",
			"index": 0
		}],
		"usageMetadata": {
			"promptTokenCount": 5,
			"candidatesTokenCount": 10,
			"totalTokenCount": 15
		},
		"modelVersion": "gemini-1.5-pro"
	}`

	parser := &GoogleGenAIParser{}
	prog, err := parser.ParseResponse([]byte(resp))
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}

	t.Logf("Google Response LIL:\n%s", prog.Disasm())

	foundText := false
	foundDone := false
	for _, inst := range prog.Code {
		if inst.Op == TXT_CHUNK && inst.Str == "Hello from Gemini!" {
			foundText = true
		}
		if inst.Op == RESP_DONE && inst.Str == "stop" {
			foundDone = true
		}
	}
	if !foundText {
		t.Error("missing text content")
	}
	if !foundDone {
		t.Error("missing RESP_DONE")
	}

	// Round-trip through Google emitter
	emitter := &GoogleGenAIEmitter{}
	out, err := emitter.EmitResponse(prog)
	if err != nil {
		t.Fatalf("emit response: %v", err)
	}

	var result map[string]any
	json.Unmarshal(out, &result)
	if result["modelVersion"] != "gemini-1.5-pro" {
		t.Errorf("modelVersion: got %v", result["modelVersion"])
	}
}

func TestMediaTypePreservation(t *testing.T) {
	// Anthropic image with explicit media type
	input := `{
		"model": "claude-3",
		"max_tokens": 1024,
		"messages": [{
			"role": "user",
			"content": [
				{"type": "text", "text": "What is this?"},
				{"type": "image", "source": {"type": "base64", "media_type": "image/webp", "data": "AAAA"}}
			]
		}]
	}`

	parser := &AnthropicParser{}
	prog, err := parser.ParseRequest([]byte(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	t.Logf("Program with media_type:\n%s", prog.Disasm())

	// Verify SET_META with media_type was emitted
	foundMediaType := false
	for _, inst := range prog.Code {
		if inst.Op == SET_META && inst.Key == "media_type" && inst.Str == "image/webp" {
			foundMediaType = true
		}
	}
	if !foundMediaType {
		t.Error("expected SET_META with media_type=image/webp")
	}

	// Emit back to Anthropic — should use the stored media type
	emitter := &AnthropicEmitter{}
	out, err := emitter.EmitRequest(prog)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	var result map[string]any
	json.Unmarshal(out, &result)

	msgs := result["messages"].([]any)
	msg := msgs[0].(map[string]any)
	content := msg["content"].([]any)

	// Find the image block
	for _, block := range content {
		b := block.(map[string]any)
		if b["type"] == "image" {
			source := b["source"].(map[string]any)
			if source["media_type"] != "image/webp" {
				t.Errorf("media_type: got %v, want image/webp", source["media_type"])
			}
		}
	}
}

func TestResponsesInputFileRoundTrip(t *testing.T) {
	input := `{
		"model": "gpt-5",
		"input": [{
			"role": "user",
			"content": [
				{"type": "input_file", "file_data": "JVBERi0xLjQK", "filename": "paper.pdf"},
				{"type": "input_text", "text": "Summarize this."}
			]
		}]
	}`

	parser := &ResponsesParser{}
	prog, err := parser.ParseRequest([]byte(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	foundFile := false
	for _, inst := range prog.Code {
		if inst.Op == FILE_REF {
			foundFile = true
		}
	}
	if !foundFile {
		t.Fatalf("expected FILE_REF\n%s", prog.Disasm())
	}

	out, err := (&ResponsesEmitter{}).EmitRequest(prog)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	var result map[string]any
	json.Unmarshal(out, &result)
	inputs := result["input"].([]any)
	msg := inputs[0].(map[string]any)
	content := msg["content"].([]any)
	file := content[0].(map[string]any)
	if file["type"] != "input_file" || file["file_data"] != "JVBERi0xLjQK" || file["filename"] != "paper.pdf" {
		t.Fatalf("bad file part: %#v", file)
	}
}

func TestAnthropicDocumentRoundTrip(t *testing.T) {
	input := `{
		"model": "claude-sonnet-4-20250514",
		"max_tokens": 1024,
		"messages": [{
			"role": "user",
			"content": [{
				"type": "document",
				"source": {"type": "base64", "media_type": "application/pdf", "data": "JVBERi0xLjQK"},
				"title": "paper.pdf",
				"context": "source document"
			}]
		}]
	}`

	parser := &AnthropicParser{}
	prog, err := parser.ParseRequest([]byte(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	foundFile := false
	for _, inst := range prog.Code {
		if inst.Op == FILE_REF {
			foundFile = true
		}
	}
	if !foundFile {
		t.Fatalf("expected FILE_REF\n%s", prog.Disasm())
	}

	out, err := (&AnthropicEmitter{}).EmitRequest(prog)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	var result map[string]any
	json.Unmarshal(out, &result)
	msgs := result["messages"].([]any)
	content := msgs[0].(map[string]any)["content"].([]any)
	doc := content[0].(map[string]any)
	source := doc["source"].(map[string]any)
	if doc["type"] != "document" || source["media_type"] != "application/pdf" || source["data"] != "JVBERi0xLjQK" {
		t.Fatalf("bad document block: %#v", doc)
	}
	if doc["title"] != "paper.pdf" || doc["context"] != "source document" {
		t.Fatalf("missing document metadata: %#v", doc)
	}
}

func TestGoogleSafetyAndVideoRoundTrip(t *testing.T) {
	input := `{
		"model": "gemini-2.5-flash",
		"contents": [{
			"role": "user",
			"parts": [
				{"inlineData": {"mimeType": "video/mp4", "data": "AAAA-video-base64"}},
				{"fileData": {"mimeType": "application/pdf", "fileUri": "https://example.com/paper.pdf"}},
				{"text": "Summarize both."}
			]
		}],
		"safetySettings": [{
			"category": "HARM_CATEGORY_HATE_SPEECH",
			"threshold": "BLOCK_NONE"
		}]
	}`

	parser := &GoogleGenAIParser{}
	prog, err := parser.ParseRequest([]byte(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	foundVideo, foundFile, foundSafety := false, false, false
	for _, inst := range prog.Code {
		switch inst.Op {
		case VID_REF:
			foundVideo = true
		case FILE_REF:
			foundFile = true
		case SET_SAFETY:
			foundSafety = true
		}
	}
	if !foundVideo || !foundFile || !foundSafety {
		t.Fatalf("missing expected opcodes video=%v file=%v safety=%v\n%s", foundVideo, foundFile, foundSafety, prog.Disasm())
	}

	out, err := (&GoogleGenAIEmitter{}).EmitRequest(prog)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	var result map[string]any
	json.Unmarshal(out, &result)
	if _, ok := result["safety_settings"]; !ok {
		t.Fatalf("missing safety_settings: %s", out)
	}
	contents := result["contents"].([]any)
	parts := contents[0].(map[string]any)["parts"].([]any)
	if parts[0].(map[string]any)["inlineData"] == nil {
		t.Fatalf("missing video inlineData: %#v", parts[0])
	}
	if parts[1].(map[string]any)["fileData"] == nil {
		t.Fatalf("missing fileData: %#v", parts[1])
	}
}

func TestResponsesResponseEmitAndStreamEmit(t *testing.T) {
	resp := `{
		"id": "resp_123",
		"model": "gpt-5",
		"output": [{
			"type": "message",
			"role": "assistant",
			"status": "completed",
			"content": [{"type": "output_text", "text": "Hello"}]
		}],
		"usage": {"input_tokens": 3, "output_tokens": 2, "total_tokens": 5}
	}`

	out, err := ConvertResponse([]byte(resp), StyleResponses, StyleResponses)
	if err != nil {
		t.Fatalf("ConvertResponse Responses->Responses: %v", err)
	}
	var result map[string]any
	json.Unmarshal(out, &result)
	if result["id"] != "resp_123" || result["model"] != "gpt-5" {
		t.Fatalf("bad response metadata: %s", out)
	}
	output := result["output"].([]any)
	content := output[0].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["text"] != "Hello" {
		t.Fatalf("bad response output: %s", out)
	}

	prog := NewProgram()
	prog.Emit(STREAM_START)
	prog.EmitString(RESP_ID, "resp_123")
	prog.EmitString(RESP_MODEL, "gpt-5")
	streamOut, err := (&ResponsesEmitter{}).EmitStreamChunk(prog)
	if err != nil {
		t.Fatalf("EmitStreamChunk: %v", err)
	}
	var event map[string]any
	json.Unmarshal(streamOut, &event)
	if event["type"] != "response.created" {
		t.Fatalf("bad stream event: %s", streamOut)
	}
}

func TestLosslessNativeToolAndPartCoverage(t *testing.T) {
	responsesReq := `{
		"model": "gpt-5",
		"tool_choice": {"type": "file_search"},
		"tools": [{"type": "file_search", "vector_store_ids": ["vs_123"]}],
		"input": [{"role": "developer", "content": [{"type": "input_text", "text": "Be exact."}]}]
	}`
	out, err := ConvertRequest([]byte(responsesReq), StyleResponses, StyleResponses)
	if err != nil {
		t.Fatalf("responses round-trip: %v", err)
	}
	var responsesOut map[string]any
	json.Unmarshal(out, &responsesOut)
	if responsesOut["tool_choice"] == nil {
		t.Fatalf("missing tool_choice: %s", out)
	}
	tools := responsesOut["tools"].([]any)
	if tools[0].(map[string]any)["type"] != "file_search" {
		t.Fatalf("raw Responses tool not preserved: %s", out)
	}
	input := responsesOut["input"].([]any)
	if input[0].(map[string]any)["role"] != "developer" {
		t.Fatalf("developer role not preserved: %s", out)
	}

	googleReq := `{
		"model": "gemini-2.5-flash",
		"tools": [{"codeExecution": {}}],
		"toolConfig": {"functionCallingConfig": {"mode": "AUTO"}},
		"generationConfig": {
			"responseMimeType": "application/json",
			"responseJsonSchema": {"type": "object", "properties": {"ok": {"type": "boolean"}}},
			"candidateCount": 2
		},
		"contents": [{"role": "user", "parts": [
			{"executableCode": {"language": "PYTHON", "code": "print(1)"}}
		]}]
	}`
	out, err = ConvertRequest([]byte(googleReq), StyleGoogleGenAI, StyleGoogleGenAI)
	if err != nil {
		t.Fatalf("google round-trip: %v", err)
	}
	var googleOut map[string]any
	json.Unmarshal(out, &googleOut)
	if googleOut["toolConfig"] == nil {
		t.Fatalf("missing toolConfig: %s", out)
	}
	genConfig := googleOut["generation_config"].(map[string]any)
	if genConfig["responseMimeType"] != "application/json" || genConfig["candidateCount"].(float64) != 2 {
		t.Fatalf("generationConfig not preserved: %s", out)
	}
	gTools := googleOut["tools"].([]any)
	if gTools[0].(map[string]any)["codeExecution"] == nil {
		t.Fatalf("raw Google tool not preserved: %s", out)
	}
	parts := googleOut["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	if parts[0].(map[string]any)["executableCode"] == nil {
		t.Fatalf("raw Google part not preserved: %s", out)
	}

	chatResp := `{
		"id": "chatcmpl_1",
		"model": "gpt-5",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": [{"type": "refusal", "refusal": "I cannot."}]},
			"finish_reason": "stop"
		}]
	}`
	out, err = ConvertResponse([]byte(chatResp), StyleChatCompletions, StyleChatCompletions)
	if err != nil {
		t.Fatalf("chat response round-trip: %v", err)
	}
	var chatOut map[string]any
	json.Unmarshal(out, &chatOut)
	choice := chatOut["choices"].([]any)[0].(map[string]any)
	content := choice["message"].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["type"] != "refusal" {
		t.Fatalf("raw Chat content part not preserved: %s", out)
	}

	responsesToolOutput := `{
		"model": "gpt-5",
		"input": [{
			"type": "function_call_output",
			"call_id": "call_1",
			"output": [{"type": "input_file", "file_data": "abc", "filename": "out.txt"}]
		}]
	}`
	out, err = ConvertRequest([]byte(responsesToolOutput), StyleResponses, StyleResponses)
	if err != nil {
		t.Fatalf("responses tool output round-trip: %v", err)
	}
	json.Unmarshal(out, &responsesOut)
	input = responsesOut["input"].([]any)
	output := input[0].(map[string]any)["output"].([]any)
	if output[0].(map[string]any)["type"] != "input_file" {
		t.Fatalf("Responses function output parts not preserved: %s", out)
	}
}

func TestConverterRegistryCompleteness(t *testing.T) {
	styles := []Style{StyleChatCompletions, StyleResponses, StyleAnthropic, StyleGoogleGenAI}

	for _, style := range styles {
		if _, err := GetParser(style); err != nil {
			t.Errorf("GetParser(%s): %v", style, err)
		}
		if _, err := GetEmitter(style); err != nil {
			t.Errorf("GetEmitter(%s): %v", style, err)
		}
		if _, err := GetResponseParser(style); err != nil {
			t.Errorf("GetResponseParser(%s): %v", style, err)
		}
		if _, err := GetStreamChunkParser(style); err != nil {
			t.Errorf("GetStreamChunkParser(%s): %v", style, err)
		}
	}

	// Response and stream emitters should exist for every supported style.
	for _, style := range styles {
		if _, err := GetResponseEmitter(style); err != nil {
			t.Errorf("GetResponseEmitter(%s): %v", style, err)
		}
		if _, err := GetStreamChunkEmitter(style); err != nil {
			t.Errorf("GetStreamChunkEmitter(%s): %v", style, err)
		}
	}
}
