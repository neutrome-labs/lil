// Package chain provides runtime chained transforms over AIL streams.
package chain

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/neutrome-labs/ail"
	"github.com/neutrome-labs/ail/transform"
)

const (
	SourceContent   = "content"
	SourceReasoning = "reasoning"

	TargetContent   = "content"
	TargetReasoning = "reasoning"

	DefaultFlushEveryChars = 800
	DefaultMaxInFlight     = 1
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
	FlushEveryChars int
	MaxInFlight     int
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

// WithSourceField chooses which source field is buffered: content or reasoning.
func WithSourceField(field string) Option {
	return func(c *Chain) {
		if field != "" {
			c.SourceField = field
		}
	}
}

// WithTargetChannel chooses where chained output is emitted: content or reasoning.
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

// WithFlushEveryChars flushes buffered substrate once it reaches n chars.
func WithFlushEveryChars(n int) Option {
	return func(c *Chain) {
		if n > 0 {
			c.FlushEveryChars = n
		}
	}
}

// WithMaxInFlight sets the number of concurrent chained requests.
func WithMaxInFlight(n int) Option {
	return func(c *Chain) {
		if n > 0 {
			c.MaxInFlight = n
		}
	}
}

// New creates a runtime chain transform.
func New(opts ...Option) *Chain {
	c := &Chain{
		SourceField:     SourceReasoning,
		TargetChannel:   TargetReasoning,
		FlushEveryChars: DefaultFlushEveryChars,
		MaxInFlight:     DefaultMaxInFlight,
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
			buf     strings.Builder
			seq     int
			wg      sync.WaitGroup
			sem     = make(chan struct{}, cfg.MaxInFlight)
			outMu   sync.Mutex
			sendOut = func(ev transform.Event) bool {
				outMu.Lock()
				defer outMu.Unlock()
				return transform.Send(ctx, out, ev)
			}
		)

		flush := func(force bool) bool {
			text := buf.String()
			if text == "" || (!force && len(text) < cfg.FlushEveryChars) {
				return true
			}
			buf.Reset()
			seq++
			req := cfg.requestFor(seq, text)
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case <-ctx.Done():
					return
				case sem <- struct{}{}:
				}
				defer func() { <-sem }()
				for ev := range cfg.Executor.Execute(ctx, req) {
					mapped := cfg.mapExecutorEvent(ev)
					if mapped.Program == nil && mapped.Err == nil {
						continue
					}
					if !sendOut(mapped) {
						return
					}
				}
			}()
			return true
		}

		for ev := range in {
			if ev.Err != nil {
				if !sendOut(ev) {
					return
				}
				continue
			}
			if ev.Program == nil {
				continue
			}

			text := cfg.extract(ev.Program)
			if text != "" {
				buf.WriteString(text)
			}
			if cfg.IncludeSource {
				if !sendOut(ev) {
					return
				}
			} else if stripped := cfg.strip(ev.Program); stripped != nil && stripped.Len() > 0 {
				if !sendOut(transform.Event{Program: stripped}) {
					return
				}
			}
			if !flush(false) {
				return
			}
			if ev.Program.HasOpcode(ail.STREAM_END) {
				if !flush(true) {
					return
				}
			}
		}
		flush(true)
		wg.Wait()
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
	if cfg.MaxInFlight <= 0 {
		cfg.MaxInFlight = DefaultMaxInFlight
	}
	return &cfg
}

func (c *Chain) requestFor(seq int, substrate string) *ail.RequestUnit {
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

func (c *Chain) mapExecutorEvent(ev transform.Event) transform.Event {
	if ev.Err != nil || ev.Program == nil {
		return ev
	}
	out := ail.NewProgram()
	out.Buffers = ev.Program.Buffers
	captured := ail.CaptureRequestOutput(ev.Program)
	if captured.Content != "" || captured.Reasoning != "" {
		text := captured.Content
		if text == "" {
			text = captured.Reasoning
		}
		emitTargetDelta(out, c.TargetChannel, text)
		return transform.Event{Program: out}
	}
	for _, inst := range ev.Program.Code {
		switch inst.Op {
		case ail.STREAM_DELTA, ail.STREAM_THINK_DELTA:
			emitTargetDelta(out, c.TargetChannel, inst.Str)
		case ail.RESP_ID, ail.RESP_MODEL, ail.RESP_DONE, ail.USAGE, ail.STREAM_START, ail.STREAM_END:
			out.Code = append(out.Code, cloneInstruction(inst))
		}
	}
	if out.Len() == 0 {
		return transform.Event{}
	}
	return transform.Event{Program: out}
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

func cloneInstruction(inst ail.Instruction) ail.Instruction {
	if len(inst.JSON) > 0 {
		j := make([]byte, len(inst.JSON))
		copy(j, inst.JSON)
		inst.JSON = j
	}
	return inst
}

var _ transform.RuntimeTransform = (*Chain)(nil)
