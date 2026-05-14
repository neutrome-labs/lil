// Package transform provides runtime streaming primitives for AIL programs.
package transform

import (
	"context"
	"sync"

	"github.com/neutrome-labs/ail"
)

// Event is one item in a runtime transform stream. Exactly one of Program or
// Err is normally set.
type Event struct {
	Program *ail.Program
	Err     error
}

// Stream carries transformed AIL chunks.
type Stream <-chan Event

// Sink accepts transformed AIL chunks.
type Sink chan<- Event

// Executor is the runtime boundary for model calls. Implementations receive a
// materialized request and return response or stream chunks as AIL programs.
type Executor interface {
	Execute(ctx context.Context, req *ail.RequestUnit) Stream
}

// ExecutorFunc adapts a function to Executor.
type ExecutorFunc func(ctx context.Context, req *ail.RequestUnit) Stream

// Execute calls f.
func (f ExecutorFunc) Execute(ctx context.Context, req *ail.RequestUnit) Stream {
	return f(ctx, req)
}

// StreamFunc adapts a function to a runtime transform.
type StreamFunc func(ctx context.Context, in Stream) Stream

// Apply calls f.
func (f StreamFunc) Apply(ctx context.Context, in Stream) Stream {
	return f(ctx, in)
}

// RuntimeTransform consumes one stream and produces another.
type RuntimeTransform interface {
	Apply(ctx context.Context, in Stream) Stream
}

// Send delivers ev unless ctx is canceled.
func Send(ctx context.Context, out Sink, ev Event) bool {
	select {
	case <-ctx.Done():
		return false
	case out <- ev:
		return true
	}
}

// FromPrograms returns a stream containing progs in order.
func FromPrograms(ctx context.Context, progs ...*ail.Program) Stream {
	out := make(chan Event)
	go func() {
		defer close(out)
		for _, prog := range progs {
			if !Send(ctx, out, Event{Program: prog}) {
				return
			}
		}
	}()
	return out
}

// Merge forwards events from streams into one output stream.
func Merge(ctx context.Context, streams ...Stream) Stream {
	out := make(chan Event)
	var wg sync.WaitGroup
	wg.Add(len(streams))
	for _, stream := range streams {
		go func(s Stream) {
			defer wg.Done()
			for ev := range s {
				if !Send(ctx, out, ev) {
					return
				}
			}
		}(stream)
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

// FanOut broadcasts each event to n output streams. Program values are cloned
// for every branch so downstream transforms can mutate safely.
func FanOut(ctx context.Context, in Stream, n int) []Stream {
	if n <= 0 {
		return nil
	}
	outs := make([]chan Event, n)
	streams := make([]Stream, n)
	for i := range outs {
		outs[i] = make(chan Event)
		streams[i] = outs[i]
	}
	go func() {
		defer func() {
			for _, out := range outs {
				close(out)
			}
		}()
		for ev := range in {
			for _, out := range outs {
				next := ev
				if ev.Program != nil {
					next.Program = ev.Program.Clone()
				}
				if !Send(ctx, out, next) {
					return
				}
			}
		}
	}()
	return streams
}

// ParallelMap applies fn concurrently to events while preserving no ordering
// guarantees. maxInFlight <= 0 is treated as 1.
func ParallelMap(ctx context.Context, in Stream, maxInFlight int, fn func(context.Context, Event) Stream) Stream {
	if maxInFlight <= 0 {
		maxInFlight = 1
	}
	out := make(chan Event)
	sem := make(chan struct{}, maxInFlight)
	var wg sync.WaitGroup

	go func() {
		defer func() {
			wg.Wait()
			close(out)
		}()
		for ev := range in {
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
			}
			wg.Add(1)
			go func(ev Event) {
				defer wg.Done()
				defer func() { <-sem }()
				for next := range fn(ctx, ev) {
					if !Send(ctx, out, next) {
						return
					}
				}
			}(ev)
		}
	}()
	return out
}
