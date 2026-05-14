# AIL Opcode Reference

AIL programs are ordered streams of `Instruction` values. Each instruction has
one opcode and zero or more typed fields:

Runtime channel transforms are described in [`transforms.md`](transforms.md).
This file documents the instruction set those transforms consume and emit.

```go
type Instruction struct {
    Op   Opcode
    Str  string
    Num  float64
    Int  int32
    JSON json.RawMessage
    Key  string
    Ref  uint32
}
```

## Request Sequencing (0x08-0x0F)

| Mnemonic | Byte | Args | Description |
|---|---:|---|---|
| `REQ_START` | `0x08` | String | Begin an executable request block; string is the request id |
| `REQ_YIELD` | `0x09` | String | External output policy: `content`, `reasoning`, `both`, or `none` |
| `REQ_END` | `0x0A` | - | End a request block |
| `SUB_CONTENT` | `0x0B` | String | Inject captured prior output as visible text content |
| `SUB_REASON` | `0x0C` | String | Inject captured prior output as a reasoning/thinking block |
| `RESP_START` | `0x0D` | String | Begin a captured response block for a request id |
| `RESP_END` | `0x0E` | - | End a captured response block |

Request blocks let one AIL program describe a sequential workflow. Provider
request emitters still emit one materialized request at a time. Consumers should
call the sequence helpers to materialize a `REQ_START`...`REQ_END` block,
execute it, parse or reassemble the response, capture its output, then
materialize the next block.

`SUB_CONTENT` and `SUB_REASON` use `request.field` selectors. Supported fields
are `content`, `reasoning`, and `reason` as an alias for `reasoning`.
`RESP_START`...`RESP_END` blocks can store parsed responses alongside request
blocks; `CaptureResponseOutputs` turns those blocks into the output map used by
substrate selectors.

```asm
REQ_START draft
  REQ_YIELD none
  SET_MODEL smart-model
  MSG_START
    ROLE_USR
    TXT_CHUNK Make a plan.
  MSG_END
REQ_END

REQ_START impl
  REQ_YIELD content
  SET_MODEL impl-model
  MSG_START
    ROLE_USR
    TXT_CHUNK Implement using this plan:
    SUB_REASON draft.content
  MSG_END
REQ_END
```

## Structure (0x10-0x1F)

| Mnemonic | Byte | Args | Description |
|---|---:|---|---|
| `MSG_START` | `0x10` | - | Begin a message block |
| `MSG_END` | `0x11` | - | End a message block |
| `ROLE_SYS` | `0x12` | - | Set role to system |
| `ROLE_USR` | `0x13` | - | Set role to user |
| `ROLE_AST` | `0x14` | - | Set role to assistant |
| `ROLE_TOOL` | `0x15` | - | Set role to tool/function result |
| `ROLE_DEV` | `0x16` | - | Set role to developer |

Role opcodes are expected inside a `MSG_START`...`MSG_END` block. A message
should contain one role.

## Content (0x20-0x2F)

| Mnemonic | Byte | Args | Description |
|---|---:|---|---|
| `TXT_CHUNK` | `0x20` | String | Text content segment |
| `IMG_REF` | `0x21` | RefID | Reference to image data in `Program.Buffers` |
| `AUD_REF` | `0x22` | RefID | Reference to audio data in `Program.Buffers` |
| `TXT_REF` | `0x23` | RefID | Reference to large text data in `Program.Buffers` |
| `FILE_REF` | `0x24` | RefID | Reference to arbitrary file/document data |
| `VID_REF` | `0x25` | RefID | Reference to video data in `Program.Buffers` |
| `PART_JSON` | `0x26` | JSON | Provider-native content part/block/item |

`*_REF` instructions point to entries in `Program.Buffers`. Emitters use
metadata such as `SET_META media_type=...` when they need MIME information.

`PART_JSON` is for provider-native content that should survive conversion even
when AIL does not have a first-class opcode for the shape.

## Reasoning / Thinking (0x28-0x2B)

| Mnemonic | Byte | Args | Description |
|---|---:|---|---|
| `THINK_START` | `0x28` | - | Begin a reasoning/thinking block within a message |
| `THINK_CHUNK` | `0x29` | String | Reasoning text content |
| `THINK_END` | `0x2A` | - | End a reasoning/thinking block |
| `THINK_REF` | `0x2B` | RefID | Opaque reasoning blob, such as a Gemini thought signature |

Thinking blocks are distinct from normal message text so emitters can map them
to provider-specific reasoning fields without mixing them into user-visible
content.

## Tool Definition (0x30-0x3F)

| Mnemonic | Byte | Args | Description |
|---|---:|---|---|
| `DEF_START` | `0x30` | - | Begin one tool definition |
| `DEF_NAME` | `0x31` | String | Tool function name |
| `DEF_DESC` | `0x32` | String | Tool function description |
| `DEF_SCHEMA` | `0x33` | JSON | Tool parameter schema |
| `DEF_END` | `0x34` | - | End one tool definition |
| `DEF_RAW` | `0x35` | JSON | Provider-native non-function tool definition |

Each function tool is encoded as a `DEF_START`...`DEF_END` block. `DEF_RAW`
preserves provider-native tools such as built-in search, code execution, file
search, or MCP/server tools.

## Tool Call (0x40-0x4F)

| Mnemonic | Byte | Args | Description |
|---|---:|---|---|
| `CALL_START` | `0x40` | String | Begin tool call; string is the call ID |
| `CALL_NAME` | `0x41` | String | Function name being called |
| `CALL_ARGS` | `0x42` | JSON | Function arguments |
| `CALL_END` | `0x43` | - | End tool call |

Tool calls usually appear inside assistant messages.

## Tool Result (0x48-0x4A)

| Mnemonic | Byte | Args | Description |
|---|---:|---|---|
| `RESULT_START` | `0x48` | String | Begin tool result; string is the original call ID |
| `RESULT_DATA` | `0x49` | String | Tool result content |
| `RESULT_END` | `0x4A` | - | End tool result |

Tool results usually appear inside tool messages. The `RESULT_START` call ID
links a result to its corresponding `CALL_START`.

## Response Metadata (0x50-0x5F)

| Mnemonic | Byte | Args | Description |
|---|---:|---|---|
| `RESP_ID` | `0x50` | String | Response ID |
| `RESP_MODEL` | `0x51` | String | Model that generated the response |
| `RESP_DONE` | `0x52` | String | Finish reason, such as `stop`, `tool_calls`, or `length` |
| `USAGE` | `0x53` | JSON | Token usage statistics |

These opcodes are mainly used in parsed responses and streaming chunks.

## Stream Events (0x60-0x6F)

| Mnemonic | Byte | Args | Description |
|---|---:|---|---|
| `STREAM_START` | `0x60` | - | Begin streaming response |
| `STREAM_DELTA` | `0x61` | String | Text delta chunk |
| `STREAM_TOOL_DELTA` | `0x62` | JSON | Tool call delta |
| `STREAM_END` | `0x63` | - | End streaming response |
| `STREAM_THINK_DELTA` | `0x64` | String | Reasoning/thinking text delta |

`STREAM_TOOL_DELTA` stores provider-normalized tool-call fragments as JSON.
Targets that need complete tool calls can buffer fragments until stream flush.

## Configuration (0xF0-0xFF)

| Mnemonic | Byte | Args | Description |
|---|---:|---|---|
| `SET_MODEL` | `0xF0` | String | Set target model name |
| `SET_TEMP` | `0xF1` | Float | Set temperature |
| `SET_TOPP` | `0xF2` | Float | Set top_p |
| `SET_STOP` | `0xF3` | String | Add one stop sequence |
| `SET_MAX` | `0xF4` | Int | Set max token/output limit |
| `SET_STREAM` | `0xF5` | - | Enable streaming mode |
| `SET_REASON_EFFORT` | `0xF6` | String | Reasoning effort level |
| `SET_FMT` | `0xF7` | JSON | Response format configuration |
| `SET_SAFETY` | `0xF8` | JSON | Content safety configuration |
| `SET_TOOL` | `0xF9` | JSON | Tool choice/tool config |
| `SET_REASON_MODE` | `0xFA` | String | Thinking mode, such as `enabled` or `disabled` |
| `SET_REASON_BUDGET` | `0xFB` | Int | Thinking/reasoning token budget |
| `EXT_DATA` | `0xFE` | Key, JSON | Provider-specific extension data |
| `SET_META` | `0xFF` | Key, Val | Arbitrary metadata key/value pair |

`EXT_DATA` is for provider-specific top-level or structured fields. `SET_META`
is for lightweight metadata used by emitters and transformations.
