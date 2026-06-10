// Package manip provides composable LIL program transformations and wrappers
// for attaching them to parsers, emitters, and simple conversion flows.
package manip

import (
	"context"
	"fmt"

	"github.com/neutrome-labs/lil"
)

// Manip transforms an LIL program.
type Manip interface {
	Apply(prog *lil.Program) (*lil.Program, error)
}

// ContextManip transforms an LIL program with request-scoped context.
type ContextManip interface {
	ApplyContext(ctx context.Context, prog *lil.Program) (*lil.Program, error)
}

// Func adapts a function into a Manip.
type Func func(prog *lil.Program) (*lil.Program, error)

// Apply calls f.
func (f Func) Apply(prog *lil.Program) (*lil.Program, error) {
	return f(prog)
}

// ContextFunc adapts a context-aware function into a ContextManip.
type ContextFunc func(ctx context.Context, prog *lil.Program) (*lil.Program, error)

// Apply calls f with context.Background.
func (f ContextFunc) Apply(prog *lil.Program) (*lil.Program, error) {
	return f(context.Background(), prog)
}

// ApplyContext calls f.
func (f ContextFunc) ApplyContext(ctx context.Context, prog *lil.Program) (*lil.Program, error) {
	return f(ctx, prog)
}

// Option configures a manip-specific value.
type Option[T any] func(*T)

// ApplyOptions applies options to target in order, ignoring nil options.
func ApplyOptions[T any](target *T, opts ...Option[T]) {
	for _, opt := range opts {
		if opt != nil {
			opt(target)
		}
	}
}

// Chain applies manips in order. A nil program or nil manip is passed through.
func Chain(prog *lil.Program, manips ...Manip) (*lil.Program, error) {
	return ChainContext(context.Background(), prog, manips...)
}

// ChainContext applies manips in order with request-scoped context. Manips that
// implement ContextManip receive ctx; plain Manip values are still supported.
func ChainContext(ctx context.Context, prog *lil.Program, manips ...Manip) (*lil.Program, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	current := prog
	for _, m := range manips {
		if m == nil {
			continue
		}
		var (
			next *lil.Program
			err  error
		)
		if cm, ok := m.(ContextManip); ok {
			next, err = cm.ApplyContext(ctx, current)
		} else {
			next, err = m.Apply(current)
		}
		if err != nil {
			return nil, err
		}
		if next != nil {
			current = next
		}
	}
	return current, nil
}

// Parser wraps an lil.Parser and applies manips after parsing.
type Parser struct {
	Base   lil.Parser
	Manips []Manip
}

// AttachParser wraps parser with manips. If no manips are provided, parser is
// returned unchanged.
func AttachParser(parser lil.Parser, manips ...Manip) lil.Parser {
	if len(manips) == 0 {
		return parser
	}
	return &Parser{Base: parser, Manips: manips}
}

// ParseRequest parses a request and applies configured manips to the result.
func (p *Parser) ParseRequest(body []byte) (*lil.Program, error) {
	if p == nil || p.Base == nil {
		return nil, fmt.Errorf("lil/manip: nil parser")
	}
	prog, err := p.Base.ParseRequest(body)
	if err != nil {
		return nil, err
	}
	return Chain(prog, p.Manips...)
}

// Emitter wraps an lil.Emitter and applies manips before emitting.
type Emitter struct {
	Base   lil.Emitter
	Manips []Manip
}

// AttachEmitter wraps emitter with manips. If no manips are provided, emitter
// is returned unchanged.
func AttachEmitter(emitter lil.Emitter, manips ...Manip) lil.Emitter {
	if len(manips) == 0 {
		return emitter
	}
	return &Emitter{Base: emitter, Manips: manips}
}

// EmitRequest applies configured manips to a request program and emits it.
func (e *Emitter) EmitRequest(prog *lil.Program) ([]byte, error) {
	if e == nil || e.Base == nil {
		return nil, fmt.Errorf("lil/manip: nil emitter")
	}
	next, err := Chain(prog, e.Manips...)
	if err != nil {
		return nil, err
	}
	return e.Base.EmitRequest(next)
}

// ResponseParser wraps an lil.ResponseParser and applies manips after parsing.
type ResponseParser struct {
	Base   lil.ResponseParser
	Manips []Manip
}

// AttachResponseParser wraps parser with response manips.
func AttachResponseParser(parser lil.ResponseParser, manips ...Manip) lil.ResponseParser {
	if len(manips) == 0 {
		return parser
	}
	return &ResponseParser{Base: parser, Manips: manips}
}

// ParseResponse parses a response and applies configured manips to the result.
func (p *ResponseParser) ParseResponse(body []byte) (*lil.Program, error) {
	if p == nil || p.Base == nil {
		return nil, fmt.Errorf("lil/manip: nil response parser")
	}
	prog, err := p.Base.ParseResponse(body)
	if err != nil {
		return nil, err
	}
	return Chain(prog, p.Manips...)
}

// ResponseEmitter wraps an lil.ResponseEmitter and applies manips before
// emitting.
type ResponseEmitter struct {
	Base   lil.ResponseEmitter
	Manips []Manip
}

// AttachResponseEmitter wraps emitter with response manips.
func AttachResponseEmitter(emitter lil.ResponseEmitter, manips ...Manip) lil.ResponseEmitter {
	if len(manips) == 0 {
		return emitter
	}
	return &ResponseEmitter{Base: emitter, Manips: manips}
}

// EmitResponse applies configured manips to a response program and emits it.
func (e *ResponseEmitter) EmitResponse(prog *lil.Program) ([]byte, error) {
	if e == nil || e.Base == nil {
		return nil, fmt.Errorf("lil/manip: nil response emitter")
	}
	next, err := Chain(prog, e.Manips...)
	if err != nil {
		return nil, err
	}
	return e.Base.EmitResponse(next)
}

// StreamChunkParser wraps an lil.StreamChunkParser and applies manips after
// parsing each chunk.
type StreamChunkParser struct {
	Base   lil.StreamChunkParser
	Manips []Manip
}

// AttachStreamChunkParser wraps parser with stream chunk manips.
func AttachStreamChunkParser(parser lil.StreamChunkParser, manips ...Manip) lil.StreamChunkParser {
	if len(manips) == 0 {
		return parser
	}
	return &StreamChunkParser{Base: parser, Manips: manips}
}

// ParseStreamChunk parses a stream chunk and applies configured manips.
func (p *StreamChunkParser) ParseStreamChunk(body []byte) (*lil.Program, error) {
	if p == nil || p.Base == nil {
		return nil, fmt.Errorf("lil/manip: nil stream chunk parser")
	}
	prog, err := p.Base.ParseStreamChunk(body)
	if err != nil {
		return nil, err
	}
	return Chain(prog, p.Manips...)
}

// StreamChunkEmitter wraps an lil.StreamChunkEmitter and applies manips before
// emitting each chunk.
type StreamChunkEmitter struct {
	Base   lil.StreamChunkEmitter
	Manips []Manip
}

// AttachStreamChunkEmitter wraps emitter with stream chunk manips.
func AttachStreamChunkEmitter(emitter lil.StreamChunkEmitter, manips ...Manip) lil.StreamChunkEmitter {
	if len(manips) == 0 {
		return emitter
	}
	return &StreamChunkEmitter{Base: emitter, Manips: manips}
}

// EmitStreamChunk applies configured manips to a stream chunk program and emits it.
func (e *StreamChunkEmitter) EmitStreamChunk(prog *lil.Program) ([]byte, error) {
	if e == nil || e.Base == nil {
		return nil, fmt.Errorf("lil/manip: nil stream chunk emitter")
	}
	next, err := Chain(prog, e.Manips...)
	if err != nil {
		return nil, err
	}
	return e.Base.EmitStreamChunk(next)
}

// RequestConverter is a reusable request converter with an explicit
// manipulation step between parse and emit.
type RequestConverter struct {
	Parser  lil.Parser
	Emitter lil.Emitter
	Manips  []Manip
}

// NewRequestConverter creates a request converter for two provider styles.
func NewRequestConverter(from, to lil.Style, manips ...Manip) (*RequestConverter, error) {
	parser, err := lil.GetParser(from)
	if err != nil {
		return nil, err
	}
	emitter, err := lil.GetEmitter(to)
	if err != nil {
		return nil, err
	}
	return &RequestConverter{Parser: parser, Emitter: emitter, Manips: manips}, nil
}

// Convert parses, manipulates, and emits one request body.
func (c *RequestConverter) Convert(body []byte) ([]byte, error) {
	return c.ConvertContext(context.Background(), body)
}

// ConvertContext parses, manipulates, and emits one request body with
// request-scoped context.
func (c *RequestConverter) ConvertContext(ctx context.Context, body []byte) ([]byte, error) {
	if c == nil || c.Parser == nil || c.Emitter == nil {
		return nil, fmt.Errorf("lil/manip: nil request converter")
	}
	prog, err := c.Parser.ParseRequest(body)
	if err != nil {
		return nil, err
	}
	prog, err = ChainContext(ctx, prog, c.Manips...)
	if err != nil {
		return nil, err
	}
	return c.Emitter.EmitRequest(prog)
}

// ResponseConverter is a reusable response converter with an explicit
// manipulation step between parse and emit.
type ResponseConverter struct {
	Parser  lil.ResponseParser
	Emitter lil.ResponseEmitter
	Manips  []Manip
}

// NewResponseConverter creates a non-streaming response converter for two
// provider styles.
func NewResponseConverter(from, to lil.Style, manips ...Manip) (*ResponseConverter, error) {
	parser, err := lil.GetResponseParser(from)
	if err != nil {
		return nil, err
	}
	emitter, err := lil.GetResponseEmitter(to)
	if err != nil {
		return nil, err
	}
	return &ResponseConverter{Parser: parser, Emitter: emitter, Manips: manips}, nil
}

// Convert parses, manipulates, and emits one response body.
func (c *ResponseConverter) Convert(body []byte) ([]byte, error) {
	return c.ConvertContext(context.Background(), body)
}

// ConvertContext parses, manipulates, and emits one response body with
// request-scoped context.
func (c *ResponseConverter) ConvertContext(ctx context.Context, body []byte) ([]byte, error) {
	if c == nil || c.Parser == nil || c.Emitter == nil {
		return nil, fmt.Errorf("lil/manip: nil response converter")
	}
	prog, err := c.Parser.ParseResponse(body)
	if err != nil {
		return nil, err
	}
	prog, err = ChainContext(ctx, prog, c.Manips...)
	if err != nil {
		return nil, err
	}
	return c.Emitter.EmitResponse(prog)
}

// ConvertRequest parses a request, applies manips, and emits it in another
// style. It mirrors lil.ConvertRequest with an explicit manipulation step.
func ConvertRequest(body []byte, from, to lil.Style, manips ...Manip) ([]byte, error) {
	converter, err := NewRequestConverter(from, to, manips...)
	if err != nil {
		return nil, err
	}
	return converter.Convert(body)
}

// ConvertResponse parses a response, applies manips, and emits it in another
// style. It mirrors lil.ConvertResponse with an explicit manipulation step.
func ConvertResponse(body []byte, from, to lil.Style, manips ...Manip) ([]byte, error) {
	converter, err := NewResponseConverter(from, to, manips...)
	if err != nil {
		return nil, err
	}
	return converter.Convert(body)
}
