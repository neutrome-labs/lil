package ail

import (
	"fmt"
	"strings"
)

const (
	YieldContent   = "content"
	YieldReasoning = "reasoning"
	YieldBoth      = "both"
	YieldNone      = "none"
)

// RequestSpan locates a REQ_START..REQ_END block. Programs without explicit
// request blocks are treated as one implicit request named "main".
type RequestSpan struct {
	Start    int
	End      int
	ID       string
	Yield    string
	Explicit bool
}

// ResponseSpan locates a RESP_START..RESP_END block.
type ResponseSpan struct {
	Start int
	End   int
	ID    string
}

// RequestOutput is the captured result of a completed request. Later request
// blocks can reference it through SUB_CONTENT/SUB_REASON selectors.
type RequestOutput struct {
	Content   string
	Reasoning string
	Program   *Program
}

// RequestUnit is a materialized executable request with sequence-only opcodes
// resolved and stripped.
type RequestUnit struct {
	ID      string
	Yield   string
	Program *Program
}

// EmittedRequest is a materialized request emitted through a provider emitter.
type EmittedRequest struct {
	ID      string
	Yield   string
	Program *Program
	Body    []byte
}

// MarshalJSON serializes only the provider request body. ID, Yield, and Program
// are runner metadata and must not leak into target dialect payloads.
func (r EmittedRequest) MarshalJSON() ([]byte, error) {
	if len(r.Body) == 0 {
		return []byte("null"), nil
	}
	return r.Body, nil
}

// SequenceEmitter adapts a normal single-request provider emitter into a
// request-sequence emitter.
type SequenceEmitter struct {
	Base Emitter
}

// NewSequenceEmitter wraps base with request sequence emission support.
func NewSequenceEmitter(base Emitter) *SequenceEmitter {
	return &SequenceEmitter{Base: base}
}

// EmitRequests materializes and emits request units through Base.
func (e *SequenceEmitter) EmitRequests(prog *Program, outputs map[string]RequestOutput) ([]EmittedRequest, error) {
	if e == nil || e.Base == nil {
		return nil, fmt.Errorf("ail: nil request sequence emitter")
	}
	return EmitRequests(e.Base, prog, outputs)
}

// Requests returns executable request spans in source order. If no REQ_START
// opcodes exist, the whole program is returned as one implicit request.
func (p *Program) Requests() []RequestSpan {
	if p == nil || len(p.Code) == 0 {
		return nil
	}

	var spans []RequestSpan
	for i := 0; i < len(p.Code); i++ {
		if p.Code[i].Op != REQ_START {
			continue
		}
		span := RequestSpan{
			Start:    i,
			End:      len(p.Code) - 1,
			ID:       p.Code[i].Str,
			Yield:    YieldContent,
			Explicit: true,
		}
		if span.ID == "" {
			span.ID = fmt.Sprintf("req:%d", len(spans)+1)
		}
		for j := i + 1; j < len(p.Code); j++ {
			switch p.Code[j].Op {
			case REQ_YIELD:
				span.Yield = p.Code[j].Str
			case REQ_END:
				span.End = j
				spans = append(spans, span)
				i = j
				goto next
			}
		}
		spans = append(spans, span)
	next:
	}

	if len(spans) == 0 {
		return []RequestSpan{{
			Start: 0,
			End:   len(p.Code) - 1,
			ID:    "main",
			Yield: YieldContent,
		}}
	}
	return spans
}

// Responses returns captured response spans in source order.
func (p *Program) Responses() []ResponseSpan {
	if p == nil || len(p.Code) == 0 {
		return nil
	}
	var spans []ResponseSpan
	for i := 0; i < len(p.Code); i++ {
		if p.Code[i].Op != RESP_START {
			continue
		}
		span := ResponseSpan{Start: i, End: len(p.Code) - 1, ID: p.Code[i].Str}
		for j := i + 1; j < len(p.Code); j++ {
			if p.Code[j].Op == RESP_END {
				span.End = j
				i = j
				break
			}
		}
		spans = append(spans, span)
	}
	return spans
}

// ExtractResponse returns the response program inside a RESP_START..RESP_END
// block with the response wrapper stripped.
func (p *Program) ExtractResponse(span ResponseSpan) *Program {
	if p == nil {
		return nil
	}
	start, end := span.Start+1, span.End-1
	if start < 0 {
		start = 0
	}
	if end >= len(p.Code) {
		end = len(p.Code) - 1
	}
	out := NewProgram()
	out.Buffers = p.Buffers
	for i := start; i <= end && i < len(p.Code); i++ {
		out.Code = append(out.Code, cloneInstruction(p.Code[i]))
	}
	return out
}

// CaptureResponseOutputs extracts output text from all captured response blocks.
func (p *Program) CaptureResponseOutputs() map[string]RequestOutput {
	outputs := make(map[string]RequestOutput)
	for _, span := range p.Responses() {
		outputs[span.ID] = CaptureRequestOutput(p.ExtractResponse(span))
	}
	return outputs
}

// MaterializeRequest returns the executable program for span. Request wrappers,
// yield declarations, response capture blocks, and substrate placeholders are
// removed. SUB_CONTENT and SUB_REASON are replaced with captured prior output.
func (p *Program) MaterializeRequest(span RequestSpan, outputs map[string]RequestOutput) (*RequestUnit, error) {
	if p == nil {
		return nil, nil
	}
	start, end := span.Start, span.End
	if span.Explicit {
		start++
		end--
	}
	if start < 0 {
		start = 0
	}
	if end >= len(p.Code) {
		end = len(p.Code) - 1
	}

	out := NewProgram()
	out.Buffers = p.Buffers

	skipRespDepth := 0
	for i := start; i <= end && i < len(p.Code); i++ {
		inst := p.Code[i]
		if skipRespDepth > 0 {
			switch inst.Op {
			case RESP_START:
				skipRespDepth++
			case RESP_END:
				skipRespDepth--
			}
			continue
		}

		switch inst.Op {
		case REQ_START, REQ_END, REQ_YIELD:
			continue
		case RESP_START:
			skipRespDepth = 1
			continue
		case RESP_END:
			continue
		case SUB_CONTENT:
			text, err := resolveSelector(inst.Str, outputs)
			if err != nil {
				return nil, err
			}
			if text != "" {
				out.EmitString(TXT_CHUNK, text)
			}
		case SUB_REASON:
			text, err := resolveSelector(inst.Str, outputs)
			if err != nil {
				return nil, err
			}
			if text != "" {
				out.Emit(THINK_START)
				out.EmitString(THINK_CHUNK, text)
				out.Emit(THINK_END)
			}
		default:
			out.Code = append(out.Code, cloneInstruction(inst))
		}
	}

	return &RequestUnit{ID: span.ID, Yield: normalizeYield(span.Yield), Program: out}, nil
}

// MaterializeRequests materializes every request whose dependencies are present
// in outputs. Consumers executing requests sequentially usually call
// MaterializeRequest one span at a time after capturing each response.
func (p *Program) MaterializeRequests(outputs map[string]RequestOutput) ([]RequestUnit, error) {
	spans := p.Requests()
	units := make([]RequestUnit, 0, len(spans))
	for _, span := range spans {
		unit, err := p.MaterializeRequest(span, outputs)
		if err != nil {
			return nil, err
		}
		if unit != nil {
			units = append(units, *unit)
		}
	}
	return units, nil
}

// EmitRequests materializes and emits request units through a provider emitter.
// It is useful once all referenced prior outputs are already available.
func EmitRequests(emitter Emitter, prog *Program, outputs map[string]RequestOutput) ([]EmittedRequest, error) {
	if emitter == nil {
		return nil, fmt.Errorf("ail: nil request emitter")
	}
	units, err := prog.MaterializeRequests(outputs)
	if err != nil {
		return nil, err
	}
	emitted := make([]EmittedRequest, 0, len(units))
	for _, unit := range units {
		body, err := emitter.EmitRequest(unit.Program)
		if err != nil {
			return emitted, err
		}
		emitted = append(emitted, EmittedRequest{
			ID:      unit.ID,
			Yield:   unit.Yield,
			Program: unit.Program,
			Body:    body,
		})
	}
	return emitted, nil
}

// CaptureRequestOutput extracts content and reasoning text from a response or
// reassembled stream program so it can be referenced by later requests.
func CaptureRequestOutput(prog *Program) RequestOutput {
	out := RequestOutput{Program: prog}
	if prog == nil {
		return out
	}
	src := ReassembleStream(prog)
	var content strings.Builder
	var reasoning strings.Builder

	for _, msg := range src.Messages() {
		if msg.Role == ROLE_AST || msg.Role == 0 {
			content.WriteString(src.MessageText(msg))
		}
	}
	for _, th := range src.Thinkings() {
		reasoning.WriteString(src.ThinkingText(th))
	}

	out.Content = content.String()
	out.Reasoning = reasoning.String()
	return out
}

func normalizeYield(v string) string {
	switch v {
	case YieldContent, YieldReasoning, YieldBoth, YieldNone:
		return v
	case "":
		return YieldContent
	default:
		return v
	}
}

func resolveSelector(selector string, outputs map[string]RequestOutput) (string, error) {
	reqID, field, ok := strings.Cut(selector, ".")
	if !ok || reqID == "" || field == "" {
		return "", fmt.Errorf("ail: invalid substrate selector %q, want request.field", selector)
	}
	out, ok := outputs[reqID]
	if !ok {
		return "", fmt.Errorf("ail: substrate selector %q references missing request %q", selector, reqID)
	}
	switch field {
	case "content":
		return out.Content, nil
	case "reasoning", "reason":
		return out.Reasoning, nil
	default:
		return "", fmt.Errorf("ail: substrate selector %q uses unknown field %q", selector, field)
	}
}

var _ RequestSequenceEmitter = (*SequenceEmitter)(nil)
