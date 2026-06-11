# LIL — AI Intermediate Language

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev)

```bash
go get github.com/neutrome-labs/lil
```

LIL is a stack-based intermediate representation for AI provider interactions.
It decouples parsing provider-specific JSON into opcodes from emitting those
opcodes back into provider-specific JSON, enabling any-to-any conversion
between supported APIs.

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
import "github.com/neutrome-labs/lil"

out, err := lil.ConvertRequest(
    body,
    lil.StyleChatCompletions,
    lil.StyleAnthropic,
)
```

### Convert a non-streaming response

```go
out, err := lil.ConvertResponse(
    body,
    lil.StyleAnthropic,
    lil.StyleChatCompletions,
)
```

### Convert streaming chunks in real time

Use `ConvertStreamChunk` only for stateless cases. For real provider streams,
use `StreamConverter` so metadata and split tool-call fragments are preserved.

```go
conv, err := lil.NewStreamConverter(
    lil.StyleAnthropic,
    lil.StyleChatCompletions,
)
if err != nil {
    return err
}

for _, chunk := range upstreamChunks {
    outputs, err := conv.Push(chunk)
    if err != nil {
        return err
    }
    for _, out := range outputs {
        fmt.Fprintf(w, "data: %s\n\n", out)
        flusher.Flush()
    }
}

final, err := conv.Flush()
if err != nil {
    return err
}
for _, out := range final {
    fmt.Fprintf(w, "data: %s\n\n", out)
    flusher.Flush()
}
```

### Parse, inspect, and emit a program directly

```go
parser, _ := lil.GetParser(lil.StyleChatCompletions)
prog, _ := parser.ParseRequest(body)

fmt.Println(prog.GetModel())
fmt.Println(prog.IsStreaming())
fmt.Println(prog.Disasm())

prog.SetModel("claude-sonnet-4-20250514")

emitter, _ := lil.GetEmitter(lil.StyleAnthropic)
out, _ := emitter.EmitRequest(prog)
```

## Program Helpers

LIL exposes helpers for traversing and reshaping `*lil.Program` without
manually editing opcode slices.

### Inspect messages and tool calls

```go
msgs := prog.Messages()
for _, msg := range msgs {
    fmt.Println(msg.Role, prog.MessageText(msg))
}

for _, call := range prog.ToolCalls() {
    fmt.Println(call.CallID, call.Name)
}

if lastUser, ok := prog.LastUserMessage(); ok {
    fmt.Println("last user:", prog.MessageText(lastUser))
}
```

### Trim context and adjust prompts

```go
trimmed := prog.TruncateMessages(8)
trimmed = trimmed.PrependSystemPrompt("Answer briefly.")
trimmed = trimmed.AppendUserMessage("Summarize the last response.")
```

These helpers return new programs and do not mutate the original unless the API
explicitly says so.

## Go SDK Helpers

High-level Go authoring helpers now live in
`github.com/neutrome-labs/lilsdk-go`.

### Sliding-window context trimming

```go
import (
    "github.com/neutrome-labs/lil"
    "github.com/neutrome-labs/lilsdk-go/manip"
    "github.com/neutrome-labs/lilsdk-go/manip/slwin"
)

out, err := manip.ConvertRequest(
    body,
    lil.StyleChatCompletions,
    lil.StyleAnthropic,
    slwin.New(
        slwin.WithKeepStart(1),
        slwin.WithKeepEnd(10),
    ),
)
```

### Cache older tool results with KVTools

```go
import (
    "context"
    "time"

    "github.com/neutrome-labs/lil"
    "github.com/neutrome-labs/lilsdk-go/manip"
    "github.com/neutrome-labs/lilsdk-go/manip/kvtools"
)

toolCache := kvtools.New(
    kvtools.WithStore(myStore),
    kvtools.WithTTL(30*time.Minute),
)

ctx := kvtools.ContextWithScope(context.Background(), traceID)
converter, err := manip.NewRequestConverter(
    lil.StyleChatCompletions,
    lil.StyleAnthropic,
    toolCache,
)
out, err := converter.ConvertContext(ctx, body)
```

`lil` itself remains the canonical IR API.

## Assembly and Binary Forms

### Disassemble and reassemble programs

```go
text := prog.Disasm()
roundTripped, err := lil.Asm(text)
```

`Disasm()` produces a human-readable assembly listing, and `Asm()` parses that
listing back into a `Program`.

### Encode to the LIL binary format

```go
var buf bytes.Buffer
if err := prog.Encode(&buf); err != nil {
    return err
}

decoded, err := lil.Decode(&buf)
if err != nil {
    return err
}
```

The binary form preserves the opcode stream and side-buffer payloads for
compact transport or storage.

## Example: End-to-End Conversion

```jsonc
// Input: OpenAI Chat Completions request
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
; LIL representation (prog.Disasm() output)
SET_MODEL gpt-5-mini
MSG_START
  ROLE_USR
  TXT_CHUNK How many r's are in the word 'strawberry'?
MSG_END
```

```jsonc
// Output: OpenAI Responses request
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

1. **Provider-agnostic core**: one opcode model, multiple parsers and emitters.
2. **Linear instruction stream**: emitters can process the program without recursive descent.
3. **Binary side buffers**: large payloads stay out of the main instruction stream.
4. **Composable transforms**: context trimming and tool-result caching live above the core IR.
