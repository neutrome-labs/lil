# AIL — AI Intermediate Language

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev)

```
go get github.com/neutrome-labs/ail
```

AIL is a stack-based intermediate representation for AI provider interactions.
It decouples **parsing** (ingesting provider-specific JSON into opcodes) from
**emitting** (writing opcodes back out as provider-specific JSON), enabling
any-to-any conversion between different AI provider APIs.

Supported providers:

| Provider | Style Constant | Request Parse | Request Emit | Response Parse | Response Emit | Stream Parse | Stream Emit |
|---|---|---|---|---|---|---|---|
| OpenAI Chat Completions | `StyleChatCompletions` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| OpenAI Responses | `StyleResponses` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Anthropic Messages | `StyleAnthropic` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Google GenAI | `StyleGoogleGenAI` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

## Quick Start

### Convert a request from one provider format to another

```go
import "github.com/neutrome-labs/ail"

// OpenAI Chat Completions → Anthropic Messages
out, err := ail.ConvertRequest(body, ail.StyleChatCompletions, ail.StyleAnthropic)

// Anthropic Messages → Google GenAI
out, err := ail.ConvertRequest(body, ail.StyleAnthropic, ail.StyleGoogleGenAI)
```

### Convert a non-streaming response

```go
out, err := ail.ConvertResponse(body, ail.StyleAnthropic, ail.StyleChatCompletions)
```

### Convert streaming chunks in real-time

```go
conv, err := ail.NewStreamConverter(ail.StyleAnthropic, ail.StyleChatCompletions)

for _, chunk := range upstreamChunks {
    outputs, err := conv.Push(chunk)
    for _, out := range outputs {
        fmt.Fprintf(w, "data: %s\n\n", out)
        flusher.Flush()
    }
}
// Flush buffered tool calls at end of stream
final, _ := conv.Flush()
for _, out := range final {
    fmt.Fprintf(w, "data: %s\n\n", out)
}
```

### Work with the AIL program directly

```go
parser, _ := ail.GetParser(ail.StyleChatCompletions)
prog, _ := parser.ParseRequest(body)

// Inspect
fmt.Println(prog.GetModel())    // "gpt-4o"
fmt.Println(prog.IsStreaming())  // true

// Modify the program
prog.SetModel("claude-sonnet-4-20250514")

// Debug: print human-readable disassembly
fmt.Println(prog.Disasm())

// Emit to a different provider
emitter, _ := ail.GetEmitter(ail.StyleAnthropic)
out, _ := emitter.EmitRequest(prog)
```

### Attach manipulations to conversion flows

```go
import (
    "context"
    "time"

    "github.com/neutrome-labs/ail"
    "github.com/neutrome-labs/ail/manip"
    "github.com/neutrome-labs/ail/manip/kvtools"
    "github.com/neutrome-labs/ail/manip/slwin"
)

out, err := manip.ConvertRequest(
    body,
    ail.StyleChatCompletions,
    ail.StyleAnthropic,
    slwin.New(slwin.WithKeepStart(1), slwin.WithKeepEnd(10)),
)

converter, err := manip.NewRequestConverter(
    ail.StyleChatCompletions,
    ail.StyleAnthropic,
    slwin.FromParams("15:3"),
)
out, err = converter.Convert(body)

// Router-compatible parameter syntax is supported too:
// "" -> keep 1 from start and 10 from end
// "15" -> keep 1 from start and 15 from end
// "15:3" -> keep 3 from start and 15 from end
window := slwin.FromParams("15:3")
emitter := manip.AttachEmitter(&ail.AnthropicEmitter{}, window)
out, err = emitter.EmitRequest(prog)
```

### Cache old tool results with KVTools

```go
toolCache := kvtools.New(
    kvtools.WithStore(myStore), // any kvtools.Store implementation
    kvtools.WithTTL(30*time.Minute),
)

ctx := kvtools.ContextWithScope(context.Background(), traceID)
converter, err := manip.NewRequestConverter(
    ail.StyleChatCompletions,
    ail.StyleAnthropic,
    toolCache,
)
out, err := converter.ConvertContext(ctx, body)
```

`kvtools` caches older completed tool-result payloads, strips their
`RESULT_DATA` from the prompt, and injects a `get_tool_result` tool definition.
Consumers can serve that tool from any inference loop:

```go
result, handled, err := toolCache.HandleToolCall(ctx, name, argsJSON)
```

Cache backends only need this interface:

```go
type Store interface {
    Get(ctx context.Context, key string) (string, error)
    Set(ctx context.Context, key, value string, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
}
```

### Compose sequential requests

AIL can represent multiple executable requests in one program. The consumer app
materializes and executes each request in order, captures its response, and uses
that captured output to resolve later `SUB_CONTENT` or `SUB_REASON` opcodes.
Parsed responses can also be stored as `RESP_START`...`RESP_END` blocks and
loaded with `CaptureResponseOutputs`.

```asm
REQ_START smart
  REQ_YIELD none
  SET_MODEL smart-model
  MSG_START
    ROLE_USR
    TXT_CHUNK Customer prompt
  MSG_END
REQ_END

REQ_START localize
  REQ_YIELD content
  SET_MODEL localizer-model
  SET_STREAM
  MSG_START
    ROLE_USR
    TXT_CHUNK Localize this:
    SUB_CONTENT smart.content
  MSG_END
REQ_END
```

```go
outputs := map[string]ail.RequestOutput{}
for _, span := range prog.Requests() {
    unit, err := prog.MaterializeRequest(span, outputs)
    if err != nil { return err }

    body, err := emitter.EmitRequest(unit.Program)
    if err != nil { return err }

    responseProgram := callModelAndParse(body)
    outputs[unit.ID] = ail.CaptureRequestOutput(responseProgram)

    switch unit.Yield {
    case ail.YieldContent:
        write(outputs[unit.ID].Content)
    case ail.YieldReasoning:
        write(outputs[unit.ID].Reasoning)
    case ail.YieldBoth:
        write(outputs[unit.ID].Reasoning + outputs[unit.ID].Content)
    }
}
```

The `manip/chain` package appends a follow-up request:

```go
prog, err = chain.New(
    "Suggest next steps.",
    chain.WithModel("suggestion-model"),
).Apply(prog)
```

### Runtime stream transforms

Static manips rewrite one AIL program. Runtime transforms operate on streams of
AIL chunks and can call models while data is flowing. The shared `transform`
package defines the channel contracts:

```go
type Executor interface {
    Execute(ctx context.Context, req *ail.RequestUnit) transform.Stream
}

type RuntimeTransform interface {
    Apply(ctx context.Context, in transform.Stream) transform.Stream
}
```

`transform/chain` buffers selected source output, sends it to a chained
executor, and emits the chained response as replacement stream chunks. This is
useful for replacing raw model reasoning with short captions from a smaller
model:

```go
captioner := transformchain.New(
    transformchain.WithExecutor(smallModel),
    transformchain.WithPrompt("Write a short caption for this reasoning segment."),
    transformchain.WithModel("caption-model"),
    transformchain.WithSourceField(transformchain.SourceReasoning),
    transformchain.WithTargetChannel(transformchain.TargetReasoning),
    transformchain.WithFlushEveryChars(800),
)

out := captioner.Apply(ctx, sourceStream)
for ev := range out {
    if ev.Err != nil { return ev.Err }
    emitChunk(ev.Program)
}
```

By default, selected source chunks are stripped, so raw reasoning is not emitted.
Use `WithIncludeSource(true)` when the chained output should augment rather than
replace the source stream. Shared helpers such as `transform.FanOut`,
`transform.Merge`, and `transform.ParallelMap` are available for future runtime
transforms.

### Pass programs through context

```go
// Store in context (avoids re-serialization in recursive handler chains)
ctx = ail.ContextWithProgram(ctx, prog)

// Retrieve later
prog, ok := ail.ProgramFromContext(ctx)
```

## Example: End-to-End Conversion

```jsonc
// Input: OpenAI Chat Completions Request
{
  "model": "gpt-5-mini",
  "messages": [
    {
      "role": "user",
      "content": "How many r's are in the word 'strawberry'?"
    }
  ]
}
```

```asm
; AIL Representation (prog.Disasm() output)
SET_MODEL gpt-5-mini
MSG_START
  ROLE_USR
  TXT_CHUNK How many r's are in the word 'strawberry'?
MSG_END
```

```jsonc
// Output: OpenAI Responses API Request (via EmitRequest)
{
  "model": "gpt-5-mini",
  "input": [
    {
      "role": "user",
      "content": [
        {
          "type": "input_text",
          "text": "How many r's are in the word 'strawberry'?"
        }
      ]
    }
  ]
}
```

## Design Principles

1. **Zero-Copy Where Possible** — Large payloads (images, audio) are stored in a side buffer and referenced by index. The instruction stream itself contains only opcodes and lightweight arguments.
2. **Stack-Based** — The emitter processes opcodes linearly. No recursive descent needed.
3. **Provider-Agnostic Core** — The opcode set covers the common denominator. Provider-specific parameters are passed through via `SET_META` and `EXT_DATA`.
4. **Binary Wire Format** — Each opcode is a single byte, enabling fast internal transfer and comparison.

## Architecture

### Interfaces

Every provider is implemented as a pair of structs — a **Parser** and an **Emitter**. They satisfy up to three interface pairs each:

```go
// Request conversion
type Parser  interface { ParseRequest(body []byte) (*Program, error) }
type Emitter interface { EmitRequest(prog *Program) ([]byte, error) }

// Non-streaming response conversion
type ResponseParser  interface { ParseResponse(body []byte) (*Program, error) }
type ResponseEmitter interface { EmitResponse(prog *Program) ([]byte, error) }

// Streaming chunk conversion
type StreamChunkParser  interface { ParseStreamChunk(body []byte) (*Program, error) }
type StreamChunkEmitter interface { EmitStreamChunk(prog *Program) ([]byte, error) }
```

Use the factory functions to get the right parser/emitter for a style:

```go
ail.GetParser(style)             // → Parser
ail.GetEmitter(style)            // → Emitter
ail.GetResponseParser(style)     // → ResponseParser
ail.GetResponseEmitter(style)    // → ResponseEmitter
ail.GetStreamChunkParser(style)  // → StreamChunkParser
ail.GetStreamChunkEmitter(style) // → StreamChunkEmitter
```

### Program

`Program` holds an ordered list of `Instruction`s plus a side-buffer for large binary blobs:

```go
type Program struct {
    Code    []Instruction
    Buffers [][]byte
}

type Instruction struct {
    Op   Opcode
    Str  string           // TXT_CHUNK, DEF_NAME, SET_MODEL, CALL_START, etc.
    Num  float64          // SET_TEMP, SET_TOPP
    Int  int32            // SET_MAX
    JSON json.RawMessage  // DEF_SCHEMA, CALL_ARGS, USAGE, EXT_DATA, STREAM_TOOL_DELTA
    Key  string           // SET_META, EXT_DATA (key part)
    Ref  uint32           // IMG_REF, AUD_REF, TXT_REF, FILE_REF, VID_REF
}
```

Programs support building, querying, cloning, appending, and disassembly:

```go
p := ail.NewProgram()
p.EmitString(ail.SET_MODEL, "gpt-4o")
p.EmitFloat(ail.SET_TEMP, 0.7)
p.Emit(ail.MSG_START)
p.Emit(ail.ROLE_USR)
p.EmitString(ail.TXT_CHUNK, "Hello")
p.Emit(ail.MSG_END)
fmt.Println(p.GetModel())     // "gpt-4o"
fmt.Println(p.IsStreaming())   // false
fmt.Println(p.Len())          // 7

clone := p.Clone()             // deep copy
merged := p.Append(other)     // concatenate (re-indexes buffer refs)
```

### StreamConverter

`StreamConverter` handles stateful, real-time streaming translation between providers. It manages:

- **Metadata carry-forward** — `RESP_ID` and `RESP_MODEL` from the first chunk are injected into all subsequent emitted chunks (some formats require this on every event).
- **Event splitting** — One source event may produce multiple output events (e.g., Anthropic requires separate SSE events per content type).
- **Tool call buffering** — Targets that require complete function calls in a single chunk (e.g., Google GenAI) buffer `STREAM_TOOL_DELTA` fragments until flushed.

```go
conv, _ := ail.NewStreamConverter(from, to)

// Push raw bytes (parses internally)
outputs, _ := conv.Push(chunk)

// Or push an already-parsed program (useful after plugin modification)
outputs, _ := conv.PushProgram(prog)

// Flush remaining buffered data at end of stream
final, _ := conv.Flush()
```

## Stream Conversion Edge Cases

The `StreamConverter` handles several structural mismatches:

- **Anthropic targets** require each event type (text delta, tool delta, start, stop) to be a separate SSE event with a different JSON structure — so one source chunk may produce multiple output events.
- **Google GenAI targets** require complete function calls in a single chunk — so tool-call argument deltas are buffered until `Flush()`.
- **Metadata injection** — Some formats (OpenAI) require `id` and `model` on every chunk, while others (Anthropic) send them only once. The converter remembers and injects as needed.

### Program Manipulation (Plugins)

Plugins operate on the `Program` directly rather than provider-specific JSON:

```go
// Inject a system prompt at the beginning
prefix := ail.NewProgram()
prefix.Emit(ail.MSG_START)
prefix.Emit(ail.ROLE_SYS)
prefix.EmitString(ail.TXT_CHUNK, "Always be helpful and safe.")
prefix.Emit(ail.MSG_END)
result := prefix.Append(prog) // buffer refs are re-indexed automatically
```

## Reference Docs

- [Opcode reference](docs/opcodes.md)
- [Binary encoding and assembly notation](docs/binary.md)
- [Provider mapping details](docs/provider-mapping.md)
