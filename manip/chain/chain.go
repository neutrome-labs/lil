// Package chain adds a follow-up request to an AIL program.
package chain

import (
	"fmt"

	"github.com/neutrome-labs/ail"
	"github.com/neutrome-labs/ail/manip"
)

const (
	DefaultSourceID = "main"
	DefaultIDPrefix = "chain"
)

// Chain appends a second request that receives a prior request output as
// substrate plus an instruction prompt.
type Chain struct {
	Prompt        string
	ID            string
	SourceID      string
	SourceField   string
	SourceYield   string
	Yield         string
	TargetChannel string
	Model         string
	Stream        *bool
}

// Option configures Chain.
type Option = manip.Option[Chain]

// WithID sets the generated request id.
func WithID(id string) Option {
	return func(c *Chain) {
		if id != "" {
			c.ID = id
		}
	}
}

// WithSource selects the request whose output is injected.
func WithSource(id string) Option {
	return func(c *Chain) {
		if id != "" {
			c.SourceID = id
		}
	}
}

// WithSourceField selects which captured output field is injected.
func WithSourceField(field string) Option {
	return func(c *Chain) {
		if field != "" {
			c.SourceField = field
		}
	}
}

// WithSourceYield changes the source request's external yield policy.
func WithSourceYield(yield string) Option {
	return func(c *Chain) {
		c.SourceYield = yield
	}
}

// WithYield sets the generated request's external yield policy.
func WithYield(yield string) Option {
	return func(c *Chain) {
		if yield != "" {
			c.Yield = yield
		}
	}
}

// WithTargetChannel chooses how the source output is injected: "content" uses
// SUB_CONTENT; "reasoning" or "reason" uses SUB_REASON.
func WithTargetChannel(channel string) Option {
	return func(c *Chain) {
		if channel != "" {
			c.TargetChannel = channel
		}
	}
}

// WithModel sets SET_MODEL on the generated request.
func WithModel(model string) Option {
	return func(c *Chain) {
		c.Model = model
	}
}

// WithStream controls SET_STREAM on the generated request.
func WithStream(stream bool) Option {
	return func(c *Chain) {
		c.Stream = &stream
	}
}

// New creates a chain manip. The prompt is emitted as a user message in the
// generated request.
func New(prompt string, opts ...Option) *Chain {
	c := &Chain{
		Prompt:        prompt,
		SourceField:   "content",
		Yield:         ail.YieldContent,
		TargetChannel: ail.YieldContent,
	}
	manip.ApplyOptions(c, opts...)
	return c
}

// Apply appends the generated request block.
func (c *Chain) Apply(prog *ail.Program) (*ail.Program, error) {
	if prog == nil {
		return nil, nil
	}
	if c == nil {
		return prog, nil
	}

	out := ensureExplicitRequests(prog)
	requests := out.Requests()
	if len(requests) == 0 {
		return out, nil
	}

	sourceID := c.SourceID
	if sourceID == "" {
		sourceID = requests[len(requests)-1].ID
	}
	if sourceID == "" {
		sourceID = DefaultSourceID
	}
	if c.SourceYield != "" {
		out = setRequestYield(out, sourceID, c.SourceYield)
	}

	id := c.ID
	if id == "" {
		id = nextChainID(out)
	}
	selector := fmt.Sprintf("%s.%s", sourceID, c.SourceField)

	out.EmitString(ail.REQ_START, id)
	out.EmitString(ail.REQ_YIELD, c.Yield)
	if c.Model != "" {
		out.EmitString(ail.SET_MODEL, c.Model)
	}
	if c.Stream != nil && *c.Stream {
		out.Emit(ail.SET_STREAM)
	}
	out.Emit(ail.MSG_START)
	out.Emit(ail.ROLE_USR)
	if c.Prompt != "" {
		out.EmitString(ail.TXT_CHUNK, c.Prompt)
		out.EmitString(ail.TXT_CHUNK, "\n\n")
	}
	switch c.TargetChannel {
	case "reasoning", "reason":
		out.EmitString(ail.SUB_REASON, selector)
	default:
		out.EmitString(ail.SUB_CONTENT, selector)
	}
	out.Emit(ail.MSG_END)
	out.Emit(ail.REQ_END)

	return out, nil
}

func ensureExplicitRequests(prog *ail.Program) *ail.Program {
	requests := prog.Requests()
	if len(requests) > 0 && requests[0].Explicit {
		return prog.Clone()
	}

	out := ail.NewProgram()
	out.Buffers = prog.Buffers
	out.EmitString(ail.REQ_START, DefaultSourceID)
	out.EmitString(ail.REQ_YIELD, ail.YieldContent)
	for _, inst := range prog.Code {
		out.Code = append(out.Code, cloneInst(inst))
	}
	out.Emit(ail.REQ_END)
	return out
}

func setRequestYield(prog *ail.Program, id, yield string) *ail.Program {
	out := prog.Clone()
	for _, span := range out.Requests() {
		if span.ID != id || !span.Explicit {
			continue
		}
		for i := span.Start + 1; i < span.End && i < len(out.Code); i++ {
			if out.Code[i].Op == ail.REQ_YIELD {
				out.Code[i].Str = yield
				return out
			}
		}
		return out.InsertAfter(span.Start, ail.Instruction{Op: ail.REQ_YIELD, Str: yield})
	}
	return out
}

func nextChainID(prog *ail.Program) string {
	used := make(map[string]bool)
	for _, span := range prog.Requests() {
		used[span.ID] = true
	}
	for i := 1; ; i++ {
		id := fmt.Sprintf("%s:%d", DefaultIDPrefix, i)
		if !used[id] {
			return id
		}
	}
}

func cloneInst(inst ail.Instruction) ail.Instruction {
	if len(inst.JSON) > 0 {
		j := make([]byte, len(inst.JSON))
		copy(j, inst.JSON)
		inst.JSON = j
	}
	return inst
}

var _ manip.Manip = (*Chain)(nil)
