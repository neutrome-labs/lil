# Runtime Transforms

Runtime transforms process streams of AIL programs. They are separate from
`manip` packages:

- `manip` rewrites one complete `*ail.Program`.
- `transform` consumes and emits channel events at runtime.

This split keeps static request shaping separate from model-calling workflows
such as streaming summarization, fan-out, or chained smaller-model passes.

## Generic Primitives

The root `transform` package defines the shared channel and executor contracts:

```go
type Event struct {
    Program *ail.Program
    Err     error
}

type Stream <-chan Event

type Executor interface {
    Execute(ctx context.Context, req *ail.RequestUnit) Stream
}

type RuntimeTransform interface {
    Apply(ctx context.Context, in Stream) Stream
}
```

Helpers:

| Helper | Purpose |
|---|---|
| `Send` | Context-aware event send |
| `FromPrograms` | Build a stream from one or more programs |
| `FanOut` | Broadcast events to several streams, cloning programs per branch |
| `Merge` | Forward multiple streams into one stream |
| `ParallelMap` | Run event-to-stream work with bounded concurrency |

## Chain Transform

`transform/chain` buffers selected source output, sends it to another model via
a `transform.Executor`, then emits that model's response as stream chunks.

Default behavior:

- Source field: reasoning (`STREAM_THINK_DELTA` / `THINK_CHUNK`)
- Target channel: reasoning (`STREAM_THINK_DELTA`)
- Source selected chunks are stripped
- Buffer flushes every 800 chars and at stream end
- One chained request runs at a time
- Each chained request includes prior source text and prior chained output for
  consistency across segments
- Per-segment terminal events from the chained executor are suppressed; the
  downstream client sees one logical stream with one terminal marker from the
  source stream

Example: replace raw reasoning with captions from a smaller model.

```go
captioner := chain.New(
    chain.WithExecutor(captionModel),
    chain.WithPrompt("Write a short user-safe caption for this reasoning segment."),
    chain.WithModel("caption-model"),
    chain.WithSourceField(chain.SourceReasoning),
    chain.WithTargetChannel(chain.TargetReasoning),
    chain.WithFlushEveryChars(800),
)

out := captioner.Apply(ctx, sourceStream)
for ev := range out {
    if ev.Err != nil {
        return ev.Err
    }
    if ev.Program != nil {
        emitChunk(ev.Program)
    }
}
```

To produce visible captions instead of reasoning captions:

```go
captioner := chain.New(
    chain.WithExecutor(captionModel),
    chain.WithTargetChannel(chain.TargetContent),
)
```

To augment the source stream rather than replace selected source chunks:

```go
captioner := chain.New(
    chain.WithExecutor(captionModel),
    chain.WithIncludeSource(true),
)
```

To translate visible content incrementally while preserving context between
segments:

```go
translator := chain.New(
    chain.WithExecutor(translationModel),
    chain.WithPrompt("Translate the next source segment. Continue the prior translated text naturally."),
    chain.WithSourceField(chain.SourceContent),
    chain.WithTargetChannel(chain.TargetContent),
    chain.WithFlushEveryChars(800),
)
```

History can be disabled with `WithIncludeHistory(false)` or bounded with
`WithMaxHistoryChars(n)`.

## Executor Boundary

The transform package does not know provider APIs. Apps implement
`transform.Executor` by:

1. Emitting `req.Program` to the target provider request format.
2. Calling the provider.
3. Parsing response or stream chunks back into AIL.
4. Returning those chunks on the output stream.

This keeps runtime transforms provider-neutral and composable with existing AIL
parsers and emitters.
