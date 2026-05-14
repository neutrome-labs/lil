# Provider Mapping Details

AIL emitters interpret the same instruction stream differently to match each
provider's native API shape.

Runtime stream transforms are documented separately in
[`transforms.md`](transforms.md). Provider mapping here covers parser/emitter
behavior for static AIL programs and stream chunks.

## OpenAI Chat Completions

| AIL Opcode | Chat Completions Equivalent |
|---|---|
| `ROLE_SYS` | `"role": "system"` |
| `ROLE_DEV` | `"role": "developer"` |
| `ROLE_USR` | `"role": "user"` |
| `ROLE_AST` | `"role": "assistant"` |
| `ROLE_TOOL` | `"role": "tool"` + `tool_call_id` |
| `TXT_CHUNK` | `"content": "..."`, as string or content parts |
| `IMG_REF` | `image_url` content part |
| `AUD_REF` | `input_audio` content part |
| `FILE_REF` | `file` / `input_file` content part |
| `PART_JSON` | Native content part for lossless passthrough |
| `DEF_*` | `"tools": [{ "type": "function", "function": {...} }]` |
| `DEF_RAW` | Native non-function tool object |
| `CALL_*` | `tool_calls` in assistant message |
| `SET_MODEL` | `"model": "..."` |
| `SET_TEMP` | `"temperature": ...` |
| `SET_MAX` | `"max_tokens"` or `"max_completion_tokens"` |
| `SET_STREAM` | `"stream": true` + `stream_options: {include_usage: true}` |
| `SET_REASON_EFFORT` | `"reasoning_effort"` |
| `SET_TOOL` | `"tool_choice"` |
| `SET_META` | `"metadata": {...}`, except `media_type` key |
| `EXT_DATA` | Remaining top-level fields passed through |

## OpenAI Responses

| AIL Opcode | Responses API Equivalent |
|---|---|
| `ROLE_SYS` | Top-level `"instructions"` field |
| `ROLE_DEV` | `"role": "developer"` in `input[]` |
| `ROLE_USR` | `"role": "user"` in `input[]` |
| `ROLE_AST` | `"role": "assistant"` in `input[]` |
| `TXT_CHUNK` | `{"type": "input_text", "text": "..."}` |
| `IMG_REF` | `{"type": "input_image", ...}` |
| `FILE_REF` | `{"type": "input_file", ...}` |
| `PART_JSON` | Native input/output item or content part |
| `DEF_*` | `tools[]` with flat structure, with `name` at top level and no `function` wrapper |
| `DEF_RAW` | Built-in/native tool object, such as `file_search`, web, or computer tools |
| `CALL_*` | `function_call` output item |
| `SET_MODEL` | `"model": "..."` |
| `SET_MAX` | `"max_output_tokens": ...` |
| `SET_REASON_EFFORT` | `"reasoning": {"effort": ...}` |

## Anthropic Messages

| AIL Opcode | Anthropic Equivalent |
|---|---|
| `ROLE_SYS` | Top-level `"system"` parameter |
| `ROLE_DEV` | Top-level `"system"` parameter |
| `ROLE_USR` | `"role": "user"` |
| `ROLE_AST` | `"role": "assistant"` |
| `ROLE_TOOL` | `"role": "user"` + `tool_result` content block |
| `TXT_CHUNK` | `{"type": "text", "text": "..."}` |
| `IMG_REF` | `{"type": "image", "source": {"type": "base64", ...}}` |
| `FILE_REF` | `{"type": "document", "source": ...}` |
| `PART_JSON` | Native content block |
| `DEF_SCHEMA` | `"input_schema"`, not `"parameters"` |
| `DEF_RAW` | Server/native/MCP tool object |
| `SET_MAX` | `"max_tokens": ...`, required by Anthropic |
| `SET_STOP` | `"stop_sequences": [...]` |
| `SET_REASON_MODE` | `"thinking.type"` |
| `SET_REASON_BUDGET` | `"thinking.budget_tokens"` |
| `SET_META` | `"metadata": {...}`, except `media_type` key |
| `RESP_DONE` | Stop reason mapped: `stop` <-> `end_turn`, `tool_calls` <-> `tool_use`, `length` <-> `max_tokens` |

## Google GenAI

| AIL Opcode | Google GenAI Equivalent |
|---|---|
| `ROLE_SYS` | `"system_instruction": {"parts": [...]}` |
| `ROLE_DEV` | `"system_instruction": {"parts": [...]}` |
| `ROLE_USR` | `"role": "user"` |
| `ROLE_AST` | `"role": "model"` |
| `ROLE_TOOL` | `"role": "function"` + `functionResponse` part |
| `TXT_CHUNK` | `{"text": "..."}` in parts |
| `IMG_REF` | `{"inlineData": {"mimeType": "...", "data": "..."}}` |
| `AUD_REF` | `{"inlineData": {"mimeType": "audio/...", ...}}` |
| `VID_REF` | `{"inlineData": {"mimeType": "video/...", ...}}` |
| `FILE_REF` | `inlineData` or `fileData`, depending on source metadata |
| `PART_JSON` | Native Gemini `Part` |
| `DEF_*` | `tools[].function_declarations[]` |
| `DEF_RAW` | Native tool object, such as `codeExecution` or search |
| `CALL_*` | `functionCall` part |
| `SET_TEMP` | `generation_config.temperature` |
| `SET_TOPP` | `generation_config.topP` |
| `SET_MAX` | `generation_config.maxOutputTokens` |
| `SET_STOP` | `generation_config.stopSequences` |
| `SET_REASON_BUDGET` | `generation_config.thinking_config.thinking_budget` |
| `SET_FMT` | `generationConfig.responseMimeType/Schema` via passthrough |
| `SET_SAFETY` | `safetySettings` / `safety_settings` |
| `SET_TOOL` | `toolConfig` |
| `RESP_DONE` | Finish reason mapped: `stop` <-> `STOP`, `length` <-> `MAX_TOKENS` |

## Incompatibility Handling

### System Prompt Placement

```text
Input: MSG_START -> ROLE_SYS -> TXT_CHUNK "Be polite" -> MSG_END

OpenAI Emitter:    {"role": "system", "content": "Be polite"} in messages[]
Anthropic Emitter: "system": "Be polite" as top-level field
Google Emitter:    "system_instruction": {"parts": [{"text": "Be polite"}]}
```

### Tool Results

```text
Input: MSG_START -> ROLE_TOOL -> RESULT_START "call_123" -> RESULT_DATA "OK" -> RESULT_END -> MSG_END

OpenAI:    {"role": "tool", "tool_call_id": "call_123", "content": "OK"}
Anthropic: {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "call_123", "content": "OK"}]}
Google:    {"role": "function", "parts": [{"functionResponse": {"name": "...", "response": {...}}}]}
```

### Extension Data Passthrough

```text
Input: EXT_DATA "seed" 42

OpenAI Emitter:    Adds "seed": 42 to request body
Anthropic Emitter: Passes through; provider may ignore unsupported fields
```

### Response Format

`SET_FMT` is a first-class opcode that each emitter maps to the provider's
native format field:

```text
Input: SET_FMT json_object

Chat Completions Emitter: "response_format": {"type":"json_object"}
Responses API Emitter:    "text": {"format": {"type":"json_object"}}
```

Complex response formats, such as JSON Schema configurations, remain valid as
raw JSON:

```asm
SET_FMT {"type":"json_schema","json_schema":{...}}
```
