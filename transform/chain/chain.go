// Package chain provides runtime chained transforms over AIL streams.
package chain

import (
	"context"
	"fmt"
	"strings"

	"github.com/neutrome-labs/ail"
	"github.com/neutrome-labs/ail/transform"
)

const (
	SourceContent   = "content"
	SourceReasoning = "reasoning"
	SourceBoth      = "both"

	TargetContent   = "content"
	TargetReasoning = "reasoning"
	TargetBoth      = "both"

	DefaultFlushEveryChars = 800
	DefaultMaxHistoryChars = 4000
)

// Chain buffers selected source output, sends it to a chained executor, and
// emits the chained executor's response as transformed stream chunks.
type Chain struct {
	Executor        transform.Executor
	Prompt          string
	Model           string
	SourceField     string
	TargetChannel   string
	IncludeSource   bool
	IncludeHistory  bool
	FlushEveryChars int
	MaxHistoryChars int
}

// Option configures Chain.
type Option func(*Chain)

// WithExecutor sets the chained model executor.
func WithExecutor(exec transform.Executor) Option {
	return func(c *Chain) {
		c.Executor = exec
	}
}

// WithPrompt sets the prompt sent to the chained executor.
func WithPrompt(prompt string) Option {
	return func(c *Chain) {
		c.Prompt = prompt
	}
}

// WithModel sets SET_MODEL on generated chained requests.
func WithModel(model string) Option {
	return func(c *Chain) {
		c.Model = model
	}
}

// WithSourceField chooses which source field is buffered: content, reasoning,
// or both.
func WithSourceField(field string) Option {
	return func(c *Chain) {
		if field != "" {
			c.SourceField = field
		}
	}
}

// WithTargetChannel chooses where chained output is emitted: content,
// reasoning, or both.
func WithTargetChannel(channel string) Option {
	return func(c *Chain) {
		if channel != "" {
			c.TargetChannel = channel
		}
	}
}

// WithIncludeSource controls whether selected source chunks are forwarded.
func WithIncludeSource(include bool) Option {
	return func(c *Chain) {
		c.IncludeSource = include
	}
}

// WithIncludeHistory controls whether each chained request includes previously
// processed source text and chained output for consistency across segments.
func WithIncludeHistory(include bool) Option {
	return func(c *Chain) {
		c.IncludeHistory = include
	}
}

// WithFlushEveryChars flushes buffered substrate once it reaches n chars.
func WithFlushEveryChars(n int) Option {
	return func(c *Chain) {
		if n > 0 {
			c.FlushEveryChars = n
		}
	}
}

// WithMaxHistoryChars sets the maximum source/output history included in each
// chained request. Older text is trimmed from the front.
func WithMaxHistoryChars(n int) Option {
	return func(c *Chain) {
		if n >= 0 {
			c.MaxHistoryChars = n
		}
	}
}

// New creates a runtime chain transform.
func New(opts ...Option) *Chain {
	c := &Chain{
		SourceField:     SourceReasoning,
		TargetChannel:   TargetReasoning,
		IncludeHistory:  true,
		FlushEveryChars: DefaultFlushEveryChars,
		MaxHistoryChars: DefaultMaxHistoryChars,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

// Apply starts the transform.
func (c *Chain) Apply(ctx context.Context, in transform.Stream) transform.Stream {
	if ctx == nil {
		ctx = context.Background()
	}
	out := make(chan transform.Event)

	go func() {
		defer close(out)
		if c == nil || c.Executor == nil {
			for ev := range in {
				if !transform.Send(ctx, out, ev) {
					return
				}
			}
			return
		}

		cfg := c.normalized()
		var (
			buf           strings.Builder
			seq           int
			sourceHistory strings.Builder
			outputHistory strings.Builder
			terminal      *ail.Program
		)

		flush := func(force bool) bool {
			text := buf.String()
			if text == "" || (!force && len(text) < cfg.FlushEveryChars) {
				return true
			}
			buf.Reset()
			seq++
			req := cfg.requestFor(seq, text, sourceHistory.String(), outputHistory.String())
			var segmentOutput strings.Builder
			for ev := range cfg.Executor.Execute(ctx, req) {
				mapped, mappedText := cfg.mapExecutorEvent(ev)
				if mapped.Program == nil && mapped.Err == nil {
					continue
				}
				if mappedText != "" {
					segmentOutput.WriteString(mappedText)
				}
				if !transform.Send(ctx, out, mapped) {
					return false
				}
			}
			appendTrimmed(&sourceHistory, text, cfg.MaxHistoryChars)
			appendTrimmed(&outputHistory, segmentOutput.String(), cfg.MaxHistoryChars)
			return true
		}

		for ev := range in {
			if ev.Err != nil {
				if !transform.Send(ctx, out, ev) {
					return
				}
				continue
			}
			if ev.Program == nil {
				continue
			}

			isTerminal := ev.Program.HasOpcode(ail.RESP_DONE) || ev.Program.HasOpcode(ail.STREAM_END)
			text := cfg.extract(ev.Program)
			if text != "" {
				buf.WriteString(text)
			}
			if cfg.IncludeSource {
				sourceEvent := ev
				if isTerminal {
					sourceEvent.Program = withoutTerminal(ev.Program)
					terminal = terminalProgram(ev.Program)
				}
				if sourceEvent.Program != nil && sourceEvent.Program.Len() > 0 && !transform.Send(ctx, out, sourceEvent) {
					return
				}
			} else if stripped := cfg.strip(ev.Program); stripped != nil && stripped.Len() > 0 {
				if isTerminal {
					terminal = terminalProgram(stripped)
					stripped = withoutTerminal(stripped)
				}
				if stripped != nil && stripped.Len() > 0 && !transform.Send(ctx, out, transform.Event{Program: stripped}) {
					return
				}
			}
			if isTerminal {
				if !flush(true) {
					return
				}
				if terminal != nil && terminal.Len() > 0 && !transform.Send(ctx, out, transform.Event{Program: terminal}) {
					return
				}
				terminal = nil
			} else if !flush(false) {
				return
			}
		}
		flush(true)
	}()

	return out
}

func (c *Chain) normalized() *Chain {
	cfg := *c
	if cfg.SourceField == "" {
		cfg.SourceField = SourceReasoning
	}
	if cfg.TargetChannel == "" {
		cfg.TargetChannel = TargetReasoning
	}
	if cfg.FlushEveryChars <= 0 {
		cfg.FlushEveryChars = DefaultFlushEveryChars
	}
	if cfg.MaxHistoryChars < 0 {
		cfg.MaxHistoryChars = DefaultMaxHistoryChars
	}
	return &cfg
}

func (c *Chain) requestFor(seq int, substrate, sourceHistory, outputHistory string) *ail.RequestUnit {
	prog := ail.NewProgram()
	if c.Model != "" {
		prog.EmitString(ail.SET_MODEL, c.Model)
	}
	prog.Emit(ail.SET_STREAM)
	prog.Emit(ail.MSG_START)
	prog.Emit(ail.ROLE_USR)
	if c.Prompt != "" {
		prog.EmitString(ail.TXT_CHUNK, c.Prompt)
		prog.EmitString(ail.TXT_CHUNK, "\n\n")
	}
	if c.IncludeHistory && (sourceHistory != "" || outputHistory != "") {
		prog.EmitString(ail.TXT_CHUNK, "Previous source text:\n")
		prog.EmitString(ail.TXT_CHUNK, sourceHistory)
		prog.EmitString(ail.TXT_CHUNK, "\n\nPrevious transformed text:\n")
		prog.EmitString(ail.TXT_CHUNK, outputHistory)
		prog.EmitString(ail.TXT_CHUNK, "\n\nNext source segment:\n")
	}
	prog.EmitString(ail.TXT_CHUNK, substrate)
	prog.Emit(ail.MSG_END)
	return &ail.RequestUnit{
		ID:      fmt.Sprintf("chain:%d", seq),
		Yield:   ail.YieldContent,
		Program: prog,
	}
}

func (c *Chain) extract(prog *ail.Program) string {
	var sb strings.Builder
	for _, inst := range prog.Code {
		switch c.SourceField {
		case SourceContent:
			if inst.Op == ail.STREAM_DELTA || inst.Op == ail.TXT_CHUNK {
				sb.WriteString(inst.Str)
			}
		case SourceBoth:
			if inst.Op == ail.STREAM_THINK_DELTA || inst.Op == ail.THINK_CHUNK || inst.Op == ail.STREAM_DELTA || inst.Op == ail.TXT_CHUNK {
				sb.WriteString(inst.Str)
			}
		default:
			if inst.Op == ail.STREAM_THINK_DELTA || inst.Op == ail.THINK_CHUNK {
				sb.WriteString(inst.Str)
			}
		}
	}
	return sb.String()
}

func (c *Chain) strip(prog *ail.Program) *ail.Program {
	out := ail.NewProgram()
	out.Buffers = prog.Buffers
	skipThinking := false
	for _, inst := range prog.Code {
		switch c.SourceField {
		case SourceContent:
			if inst.Op == ail.STREAM_DELTA || inst.Op == ail.TXT_CHUNK {
				continue
			}
		case SourceBoth:
			switch inst.Op {
			case ail.STREAM_DELTA, ail.TXT_CHUNK, ail.STREAM_THINK_DELTA:
				continue
			case ail.THINK_START:
				skipThinking = true
				continue
			case ail.THINK_END:
				skipThinking = false
				continue
			}
			if skipThinking {
				continue
			}
		default:
			switch inst.Op {
			case ail.STREAM_THINK_DELTA:
				continue
			case ail.THINK_START:
				skipThinking = true
				continue
			case ail.THINK_END:
				skipThinking = false
				continue
			}
			if skipThinking {
				continue
			}
		}
		out.Code = append(out.Code, cloneInstruction(inst))
	}
	return out
}

func (c *Chain) mapExecutorEvent(ev transform.Event) (transform.Event, string) {
	if ev.Err != nil || ev.Program == nil {
		return ev, ""
	}
	out := ail.NewProgram()
	out.Buffers = ev.Program.Buffers
	if !hasStreamOutput(ev.Program) {
		captured := ail.CaptureRequestOutput(ev.Program)
		if captured.Content != "" || captured.Reasoning != "" {
			emitTargetOutput(out, c.TargetChannel, captured.Content, captured.Reasoning)
			return transform.Event{Program: out}, captured.Reasoning + captured.Content
		}
	}
	var mappedText strings.Builder
	for _, inst := range ev.Program.Code {
		switch inst.Op {
		case ail.STREAM_DELTA:
			emitTargetOutput(out, c.TargetChannel, inst.Str, "")
			mappedText.WriteString(inst.Str)
		case ail.STREAM_THINK_DELTA:
			emitTargetOutput(out, c.TargetChannel, "", inst.Str)
			mappedText.WriteString(inst.Str)
		}
	}
	if out.Len() == 0 {
		return transform.Event{}, mappedText.String()
	}
	return transform.Event{Program: out}, mappedText.String()
}

func hasStreamOutput(prog *ail.Program) bool {
	if prog == nil {
		return false
	}
	for _, inst := range prog.Code {
		switch inst.Op {
		case ail.STREAM_DELTA, ail.STREAM_THINK_DELTA:
			return true
		}
	}
	return false
}

func emitTargetDelta(prog *ail.Program, target, text string) {
	if text == "" {
		return
	}
	if target == TargetContent {
		prog.EmitString(ail.STREAM_DELTA, text)
		return
	}
	prog.EmitString(ail.STREAM_THINK_DELTA, text)
}

func emitTargetOutput(prog *ail.Program, target, content, reasoning string) {
	switch target {
	case TargetBoth:
		emitTargetDelta(prog, TargetReasoning, reasoning)
		emitTargetDelta(prog, TargetContent, content)
	case TargetContent:
		emitTargetDelta(prog, TargetContent, reasoning)
		emitTargetDelta(prog, TargetContent, content)
	default:
		emitTargetDelta(prog, TargetReasoning, reasoning)
		emitTargetDelta(prog, TargetReasoning, content)
	}
}

func cloneInstruction(inst ail.Instruction) ail.Instruction {
	if len(inst.JSON) > 0 {
		j := make([]byte, len(inst.JSON))
		copy(j, inst.JSON)
		inst.JSON = j
	}
	return inst
}

func terminalProgram(prog *ail.Program) *ail.Program {
	if prog == nil {
		return nil
	}
	out := ail.NewProgram()
	out.Buffers = prog.Buffers
	for _, inst := range prog.Code {
		switch inst.Op {
		case ail.RESP_DONE, ail.USAGE, ail.STREAM_END:
			out.Code = append(out.Code, cloneInstruction(inst))
		}
	}
	return out
}

func withoutTerminal(prog *ail.Program) *ail.Program {
	if prog == nil {
		return nil
	}
	out := ail.NewProgram()
	out.Buffers = prog.Buffers
	for _, inst := range prog.Code {
		switch inst.Op {
		case ail.RESP_DONE, ail.USAGE, ail.STREAM_END:
			continue
		default:
			out.Code = append(out.Code, cloneInstruction(inst))
		}
	}
	return out
}

func appendTrimmed(sb *strings.Builder, text string, maxChars int) {
	if text == "" || maxChars == 0 {
		return
	}
	combined := sb.String() + text
	runes := []rune(combined)
	if maxChars > 0 && len(runes) > maxChars {
		runes = runes[len(runes)-maxChars:]
	}
	sb.Reset()
	sb.WriteString(string(runes))
}

var _ transform.RuntimeTransform = (*Chain)(nil)
